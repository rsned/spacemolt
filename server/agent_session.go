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
	}
}

// OnConnected implements game.MessageHandler.
func (s *AgentSession) OnConnected(state *game.State) {
	s.mu.Lock()
	s.connected = true
	s.mu.Unlock()

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
func (s *AgentSession) SendCommand(ctx context.Context, msg protocol.Message) error {
	if !s.IsConnected() {
		return fmt.Errorf("agent %s is not connected to the game server", s.username)
	}
	return s.gameClient.Send(ctx, msg)
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

func boolPtr(b bool) *bool {
	return &b
}
