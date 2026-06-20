package supervisor

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// SpawnFunc starts a worker process for spec, told to dial socket.
type SpawnFunc func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error)

// DefaultSpawn returns a SpawnFunc that launches workerBin with flags.
func DefaultSpawn(workerBin string) SpawnFunc {
	return func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx,
			workerBin,
			"--agent", spec.AgentID,
			"--role", spec.Role,
			"--station", spec.Station,
			"--socket", socket,
		)
		cmd.Stdout = log.Writer()
		cmd.Stderr = log.Writer()
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("supervisor: start worker %q: %w", spec.AgentID, err)
		}
		return cmd, nil
	}
}

// Supervisor spawns and keeps workers alive.
type Supervisor struct {
	server      *Server
	fleet       *Fleet
	specs       []WorkerSpec
	spawn       SpawnFunc
	logger      *log.Logger
	// SilenceTimeout MUST exceed worst-case worker cold-start, which includes
	// per-IP /login rate-limit throttling when many workers boot together; until
	// the Plan B structural fix lands, a too-small value causes duplicate worker
	// processes.
	SilenceTimeout time.Duration
	MaxRestarts    int
	restarts       map[string]int
}

// NewSupervisor wires a supervisor. server may be nil in tests.
func NewSupervisor(server *Server, fleet *Fleet, specs []WorkerSpec, spawn SpawnFunc, logger *log.Logger) *Supervisor {
	return &Supervisor{
		server: server, fleet: fleet, specs: specs, spawn: spawn, logger: logger,
		SilenceTimeout: 5 * game.SleepLong,
		MaxRestarts:    100,
		restarts:       make(map[string]int),
	}
}

func (s *Supervisor) socket() string {
	if s.server == nil {
		return ""
	}
	return s.server.Addr()
}

// Run spawns each spec, then periodically restarts silent/dead workers.
func (s *Supervisor) Run(ctx context.Context) error {
	for _, spec := range s.specs {
		s.launch(ctx, spec)
	}
	ticker := time.NewTicker(game.SleepMedium)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.reapAndRestart(ctx)
		}
	}
}

func (s *Supervisor) launch(ctx context.Context, spec WorkerSpec) {
	cmd, err := s.spawn(ctx, spec, s.socket())
	if err != nil {
		s.logger.Printf("spawn %q failed: %v", spec.AgentID, err)
		return
	}
	if cmd != nil {
		// Reap the child when it exits (or is killed on ctx cancel) so it
		// does not linger as a zombie across restart cycles.
		go func() { _ = cmd.Wait() }()
	}
}

func (s *Supervisor) reapAndRestart(ctx context.Context) {
	now := time.Now()
	healthy := make(map[string]WorkerInfo)
	for _, w := range s.fleet.Snapshot() {
		healthy[w.AgentID] = w
	}
	for _, spec := range s.specs {
		w, seen := healthy[spec.AgentID]
		if !seen || NeedsRestart(w, now, s.SilenceTimeout) {
			if s.restarts[spec.AgentID] >= s.MaxRestarts {
				continue
			}
			s.restarts[spec.AgentID]++
			s.logger.Printf("restarting worker %q (seen=%v)", spec.AgentID, seen)
			s.fleet.MarkRestart(spec.AgentID)
			s.launch(ctx, spec)
		} else {
			// Worker is connected and healthy — clear its crash-loop counter so
			// MaxRestarts bounds restarts-per-incident, not lifetime restarts.
			delete(s.restarts, spec.AgentID)
		}
	}
}
