package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// Server is the overmind side of the control channel.
type Server struct {
	sock   string
	ln     net.Listener
	fleet  *Fleet
	logger *log.Logger

	mu        sync.RWMutex
	conns     map[string]*control.Encoder // agentID -> writer
	eventHook func(agentID string, ev control.Event)
	adminHook func(op control.Type, agentID string) control.AdminAck
}

// NewServer removes any stale socket then listens on socketPath.
func NewServer(socketPath string, fleet *Fleet, logger *log.Logger) (*Server, error) {
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("supervisor: remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("supervisor: listen: %w", err)
	}
	return &Server{
		sock: socketPath, ln: ln, fleet: fleet, logger: logger,
		conns: make(map[string]*control.Encoder),
	}, nil
}

// Addr returns the socket path workers should dial.
func (s *Server) Addr() string { return s.sock }

// SetEventHook installs a callback invoked for every worker Event.
func (s *Server) SetEventHook(h func(agentID string, ev control.Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventHook = h
}

// SetAdminHook installs the callback for admin membership envelopes.
func (s *Server) SetAdminHook(h func(op control.Type, agentID string) control.AdminAck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adminHook = h
}

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
	}()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			// ctx cancellation closes the listener; treat that as clean shutdown.
			if ctx.Err() != nil {
				return ctx.Err() //nolint:wrapcheck
			}
			return fmt.Errorf("supervisor: accept: %w", err)
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	dec := control.NewDecoder(conn)
	enc := control.NewEncoder(conn)
	var agentID string
	for {
		env, err := dec.Decode()
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				s.logger.Printf("worker %q read error: %v", agentID, err)
			}
			break
		}
		switch env.Type {
		case control.TypeHello:
			var h control.Hello
			if err := env.Into(&h); err != nil {
				s.logger.Printf("bad hello: %v", err)
				continue
			}
			agentID = h.AgentID
			s.register(agentID, enc)
			s.fleet.ApplyHello(h, h.PID, time.Now())
		case control.TypeStatus:
			var st control.Status
			if err := env.Into(&st); err != nil {
				s.logger.Printf("worker %q bad status: %v", agentID, err)
				continue
			}
			s.fleet.ApplyStatus(env.AgentID, st, time.Now())
		case control.TypeEvent:
			var ev control.Event
			if err := env.Into(&ev); err != nil {
				s.logger.Printf("worker %q bad event: %v", agentID, err)
				continue
			}
			s.mu.RLock()
			hook := s.eventHook
			s.mu.RUnlock()
			if hook != nil {
				hook(env.AgentID, ev)
			}
		case control.TypeAdminRemove, control.TypeAdminReadd:
			var req control.AdminRequest
			if err := env.Into(&req); err != nil {
				s.logger.Printf("bad admin request: %v", err)
				return
			}
			s.mu.RLock()
			hook := s.adminHook
			s.mu.RUnlock()
			ack := control.AdminAck{AgentID: req.AgentID, Status: control.AckUnknownAgent, Detail: "no admin hook installed"}
			if hook != nil {
				ack = hook(env.Type, req.AgentID)
			}
			reply, err := control.NewEnvelope(control.TypeAdminAck, req.AgentID, ack)
			if err == nil {
				err = enc.Encode(reply)
			}
			if err != nil {
				s.logger.Printf("admin ack write failed: %v", err)
			}
			return // one request per admin connection; close without registering
		default:
			s.logger.Printf("worker %q: unhandled inbound type %q", agentID, env.Type)
		}
	}
	if agentID != "" {
		s.unregister(agentID)
	}
}

func (s *Server) register(agentID string, enc *control.Encoder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[agentID] = enc
}

func (s *Server) unregister(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, agentID)
}

// Send routes env to the named worker's connection.
func (s *Server) Send(agentID string, env control.Envelope) error {
	s.mu.RLock()
	enc := s.conns[agentID]
	s.mu.RUnlock()
	if enc == nil {
		return fmt.Errorf("supervisor: worker %q not connected", agentID)
	}
	return enc.Encode(env)
}
