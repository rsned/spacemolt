package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

// AgentSession wraps a game.Client and relays all messages to browser clients.
type AgentSession struct {
	username   string
	gameClient *game.Client
	server     *ObserverServer
	ctx        context.Context
	cancel     context.CancelFunc
	connected  bool
	mu         sync.RWMutex
	logger     *log.Logger
	cache      *AgentCache
	lastTick   int64
}

// NewAgentSession creates a session that relays game messages to the browser hub.
func NewAgentSession(username string, client *game.Client, server *ObserverServer, ctx context.Context, cancel context.CancelFunc, logger *log.Logger) *AgentSession {
	return &AgentSession{
		username:   username,
		gameClient: client,
		server:     server,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
		cache:      NewAgentCache(),
	}
}

// OnConnected implements game.MessageHandler.
func (s *AgentSession) OnConnected(state *game.State) {
	s.mu.Lock()
	s.connected = true
	s.mu.Unlock()

	s.cache.Clear()
	s.logger.Printf("*** [%s] connected to game server", s.username)

	statusMsg := ServerMessage{
		Type:      "agent_status",
		Agent:     s.username,
		Connected: boolPtr(true),
	}
	s.broadcastServerMessage(statusMsg)
}

// OnMessage implements game.MessageHandler.
func (s *AgentSession) OnMessage(resp protocol.Response) {
	s.logger.Printf("*** [%s] OnMessage: type='%s'", s.username, resp.Type)

	s.trackTick(resp)
	s.cacheResponse(resp)
	s.updateCache(resp)

	raw, err := json.Marshal(resp)
	if err != nil {
		s.logger.Printf("[%s] failed to marshal response: %v", s.username, err)
		return
	}

	msg := ServerMessage{
		Type:    "game_message",
		Agent:   s.username,
		Message: raw,
	}
	s.broadcastServerMessage(msg)

	s.logger.Printf("*** [%s] broadcast message for type='%s' (%d bytes)", s.username, resp.Type, len(raw))
}

// OnDisconnected implements game.MessageHandler with enhanced error handling.
func (s *AgentSession) OnDisconnected(err error) {
	s.mu.Lock()
	s.connected = false
	s.mu.Unlock()

	// Categorize the disconnection reason for better monitoring
	disconnectReason := categorizeDisconnectError(err)
	s.logger.Printf("*** [%s] disconnected from game server: %v (reason: %s)", s.username, err, disconnectReason)

	statusMsg := ServerMessage{
		Type:      "agent_status",
		Agent:     s.username,
		Connected: boolPtr(false),
		Error:     disconnectReason,
	}
	s.broadcastServerMessage(statusMsg)
}

// IsConnected returns the connection status.
func (s *AgentSession) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// SendCommand sends a command to the game server on behalf of a browser client.
// This is a fire-and-forget operation - responses are handled asynchronously via the message handler.
func (s *AgentSession) SendCommand(ctx context.Context, msg protocol.Message) error {
	s.logger.Printf("*** [%s] SendCommand: type='%s' payload=%v", s.username, msg.Type, msg.Payload)

	// Invalidate cache entries affected by mutation commands.
	if keys, ok := InvalidationMap[msg.Type]; ok {
		s.cache.Invalidate(keys...)
		s.logger.Printf("*** [%s] cache invalidated %v for command '%s'", s.username, keys, msg.Type)
	}

	// Check cache before forwarding to the game server.
	if cached := s.cache.Get(msg.Type, s.lastTick); cached != nil {
		s.logger.Printf("*** [%s] cache hit for '%s', returning cached response", s.username, msg.Type)
		s.broadcastServerMessage(ServerMessage{
			Type:    "game_message",
			Agent:   s.username,
			Message: cached,
		})
		return nil
	}

	// Ensure we're connected before sending
	if !s.IsConnected() {
		s.logger.Printf("*** [%s] not connected, attempting reconnect before command '%s'", s.username, msg.Type)
		if err := s.gameClient.Reconnect(ctx); err != nil {
			s.logger.Printf("*** [%s] reconnect failed: %v", s.username, err)
			return fmt.Errorf("agent %s is not connected and reconnect failed: %w", s.username, err)
		}

		// Wait for connection to be fully ready after reconnect
		s.logger.Printf("*** [%s] waiting for connection to be ready...", s.username)
		if err := s.gameClient.WaitForReady(ctx, 10*time.Second); err != nil {
			s.logger.Printf("*** [%s] connection not ready after reconnect: %v", s.username, err)
			return fmt.Errorf("connection not ready after reconnect: %w", err)
		}

		s.logger.Printf("*** [%s] reconnected successfully, now sending command '%s'", s.username, msg.Type)
	}

	s.logger.Printf("[%s] sending command '%s' to game server", s.username, msg.Type)
	// Session is a generic relay: responses come back via OnMessage and are
	// delivered to the WS client by message type, not correlated to a specific
	// command. Submit / subscribePush expect a one-to-one command/response
	// shape and would steal the response from the OnMessage relay path,
	// so this caller intentionally uses the fire-and-forget Send escape
	// hatch documented on (*game.Client).Send.
	err := s.gameClient.Send(ctx, msg)
	if err != nil {
		s.logger.Printf("*** [%s] ERROR sending command '%s': %v", s.username, msg.Type, err)
		return fmt.Errorf("failed to send command '%s' for agent %s: %w", msg.Type, s.username, err)
	}

	s.logger.Printf("*** [%s] command '%s' sent successfully", s.username, msg.Type)
	return nil
}

// Close shuts down the agent session.
func (s *AgentSession) Close() {
	s.cancel()
	_ = s.gameClient.Close()
}

func (s *AgentSession) broadcastServerMessage(msg ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		s.logger.Printf("*** [%s] failed to marshal server message: %v", s.username, err)
		return
	}
	s.server.BrowserHub.Broadcast(s.username, data)
}

func (s *AgentSession) updateCache(resp protocol.Response) {
	cache := s.server.cache
	switch resp.Type {
	case "map":
		if raw, err := json.Marshal(resp.Payload); err == nil {
			cache.SetGalaxyMap(raw)
		}
	case "ships":
		if raw, err := json.Marshal(resp.Payload); err == nil {
			cache.SetShipClasses(raw)
		}
	case "recipes":
		if raw, err := json.Marshal(resp.Payload); err == nil {
			cache.SetRecipes(raw)
		}
	case "system":
		if sysID, ok := resp.Payload["id"].(string); ok {
			if raw, err := json.Marshal(resp.Payload); err == nil {
				cache.SetSystem(sysID, raw)
			}
		}
	}
}

// trackTick extracts the game tick from server responses and updates lastTick.
func (s *AgentSession) trackTick(resp protocol.Response) {
	var tickVal float64
	var found bool

	switch resp.Type {
	case protocol.TypeStateUpdate, protocol.TypeTick:
		if v, ok := resp.Payload["tick"].(float64); ok {
			tickVal = v
			found = true
		}
	case protocol.TypeWelcome:
		if v, ok := resp.Payload["tick"].(float64); ok {
			tickVal = v
			found = true
		}
	}

	if found {
		s.mu.Lock()
		s.lastTick = int64(tickVal)
		s.mu.Unlock()
	}
}

// cacheResponse stores "ok" responses for cacheable commands.
func (s *AgentSession) cacheResponse(resp protocol.Response) {
	if resp.Type != protocol.TypeOK {
		return
	}

	command, ok := resp.Payload["command"].(string)
	if !ok {
		return
	}

	if _, hasPol := CachePolicies[command]; !hasPol {
		return
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}

	s.mu.RLock()
	tick := s.lastTick
	s.mu.RUnlock()

	s.cache.Set(command, raw, tick)
	s.logger.Printf("*** [%s] cached response for '%s' at tick %d", s.username, command, tick)
}

func boolPtr(b bool) *bool {
	return &b
}

// categorizeDisconnectError categorizes disconnection errors for better monitoring and debugging.
func categorizeDisconnectError(err error) string {
	if err == nil {
		return "unknown"
	}

	errMsg := strings.ToLower(err.Error())

	// Connection closed by server
	if strings.Contains(errMsg, "normal closure") || strings.Contains(errMsg, "status 1000") {
		return "normal_closure"
	}
	if strings.Contains(errMsg, "going away") || strings.Contains(errMsg, "status 1001") {
		return "client_going_away"
	}

	// Network issues
	if strings.Contains(errMsg, "connection reset") || strings.Contains(errMsg, "broken pipe") {
		return "connection_reset"
	}
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(errMsg, "no route") || strings.Contains(errMsg, "unreachable") {
		return "network_unreachable"
	}

	// WebSocket specific
	if strings.Contains(errMsg, "close frame") {
		return "server_initiated_close"
	}
	if strings.Contains(errMsg, "websocket") {
		return "websocket_error"
	}

	// Connection timeout monitoring
	if strings.Contains(errMsg, "connection timeout") || strings.Contains(errMsg, "no messages") {
		return "connection_timeout"
	}

	// Default
	return "unknown_error"
}
