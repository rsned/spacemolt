package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

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
	cache      *agentCache
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
		cache:      newAgentCache(),
	}
}

// OnConnected implements game.MessageHandler.
func (s *AgentSession) OnConnected(state *game.State) {
	s.mu.Lock()
	s.connected = true
	s.mu.Unlock()

	s.cache.clear()
	s.logger.Printf("[%s] connected to game server", s.username)

	statusMsg := serverMessage{
		Type:      "agent_status",
		Agent:     s.username,
		Connected: boolPtr(true),
	}
	s.broadcastServerMessage(statusMsg)
}

// OnMessage implements game.MessageHandler.
func (s *AgentSession) OnMessage(resp protocol.Response) {
	s.trackTick(resp)
	s.cacheResponse(resp)
	s.updateCache(resp)

	raw, err := json.Marshal(resp)
	if err != nil {
		s.logger.Printf("[%s] failed to marshal response: %v", s.username, err)
		return
	}

	msg := serverMessage{
		Type:    "game_message",
		Agent:   s.username,
		Message: raw,
	}
	s.broadcastServerMessage(msg)
}

// OnDisconnected implements game.MessageHandler.
func (s *AgentSession) OnDisconnected(err error) {
	s.mu.Lock()
	s.connected = false
	s.mu.Unlock()

	s.logger.Printf("[%s] disconnected from game server: %v", s.username, err)

	statusMsg := serverMessage{
		Type:      "agent_status",
		Agent:     s.username,
		Connected: boolPtr(false),
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
// If the agent is disconnected or the send fails, it attempts to reconnect and
// retries the command once after the login sequence completes.
func (s *AgentSession) SendCommand(ctx context.Context, msg protocol.Message) error {
	// Invalidate cache entries affected by mutation commands.
	if keys, ok := invalidationMap[msg.Type]; ok {
		s.cache.invalidate(keys...)
		s.logger.Printf("[%s] cache invalidated %v for command '%s'", s.username, keys, msg.Type)
	}

	// Check cache before forwarding to the game server.
	if cached := s.cache.get(msg.Type, s.lastTick); cached != nil {
		s.logger.Printf("[%s] cache hit for '%s'", s.username, msg.Type)
		s.broadcastServerMessage(serverMessage{
			Type:    "game_message",
			Agent:   s.username,
			Message: cached,
		})
		return nil
	}

	if !s.IsConnected() {
		s.logger.Printf("[%s] not connected, attempting reconnect before command '%s'", s.username, msg.Type)
		if err := s.reconnect(ctx); err != nil {
			return fmt.Errorf("agent %s is not connected and reconnect failed: %v", s.username, err)
		}
		s.logger.Printf("[%s] reconnected successfully, retrying command '%s'", s.username, msg.Type)
		return s.gameClient.Send(ctx, msg)
	}

	err := s.gameClient.Send(ctx, msg)
	if err == nil {
		return nil
	}

	// Send failed — likely a connection drop between the connected check and the write.
	// Attempt one reconnect + retry cycle.
	s.logger.Printf("[%s] send failed for command '%s': %v — attempting reconnect and retry", s.username, msg.Type, err)
	if reconnErr := s.reconnect(ctx); reconnErr != nil {
		return fmt.Errorf("send failed and reconnect also failed: %v (original: %v)", reconnErr, err)
	}
	s.logger.Printf("[%s] reconnected successfully, retrying command '%s'", s.username, msg.Type)
	return s.gameClient.Send(ctx, msg)
}

// reconnect attempts to re-establish the game server connection.
func (s *AgentSession) reconnect(ctx context.Context) error {
	if err := s.gameClient.Reconnect(ctx); err != nil {
		return err
	}
	return nil
}

// Close shuts down the agent session.
func (s *AgentSession) Close() {
	s.cancel()
	_ = s.gameClient.Close()
}

func (s *AgentSession) broadcastServerMessage(msg serverMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		s.logger.Printf("[%s] failed to marshal server message: %v", s.username, err)
		return
	}
	s.server.browserHub.Broadcast(s.username, data)
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

	if _, hasPol := cachePolicies[command]; !hasPol {
		return
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}

	s.mu.RLock()
	tick := s.lastTick
	s.mu.RUnlock()

	s.cache.set(command, raw, tick)
	s.logger.Printf("[%s] cached response for '%s' at tick %d", s.username, command, tick)
}

func boolPtr(b bool) *bool {
	return &b
}
