package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/internal/protocol"
)

// BrowserHub manages all connected browser WebSocket clients.
type BrowserHub struct {
	clients map[*BrowserClient]bool
	mu      sync.RWMutex
	logger  *log.Logger
}

// NewBrowserHub creates a new hub for browser clients.
func NewBrowserHub(logger *log.Logger) *BrowserHub {
	return &BrowserHub{
		clients: make(map[*BrowserClient]bool),
		logger:  logger,
	}
}

// Register adds a browser client to the hub.
func (h *BrowserHub) Register(client *BrowserClient) {
	h.mu.Lock()
	h.clients[client] = true
	h.mu.Unlock()
}

// Unregister removes a browser client from the hub.
func (h *BrowserHub) Unregister(client *BrowserClient) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
}

// Broadcast sends a message to all browser clients watching the given agent.
func (h *BrowserHub) Broadcast(agentName string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		client.mu.RLock()
		watching := client.agentName == agentName
		client.mu.RUnlock()

		if watching {
			select {
			case client.sendCh <- data:
			default:
				// Drop message if client is too slow.
				h.logger.Printf("dropping message for slow browser client")
			}
		}
	}
}

// BroadcastAll sends a message to all connected browser clients regardless of subscription.
func (h *BrowserHub) BroadcastAll(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.sendCh <- data:
		default:
		}
	}
}

// BrowserClient represents a single browser WebSocket connection.
type BrowserClient struct {
	conn      *websocket.Conn
	sendCh    chan []byte
	agentName string
	server    *ObserverServer
	mu        sync.RWMutex
	logger    *log.Logger
}

// NewBrowserClient creates a browser client and starts its read/write loops.
func NewBrowserClient(conn *websocket.Conn, server *ObserverServer, logger *log.Logger) *BrowserClient {
	return &BrowserClient{
		conn:   conn,
		sendCh: make(chan []byte, 256),
		server: server,
		logger: logger,
	}
}

// Run starts the read and write loops for this browser client.
func (bc *BrowserClient) Run(ctx context.Context) {
	hub := bc.server.browserHub
	hub.Register(bc)
	defer hub.Unregister(bc)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go bc.writeLoop(ctx, cancel)
	bc.readLoop(ctx)
}

func (bc *BrowserClient) writeLoop(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-bc.sendCh:
			if !ok {
				return
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
			err := bc.conn.Write(writeCtx, websocket.MessageText, msg)
			writeCancel()
			if err != nil {
				bc.logger.Printf("browser write error: %v", err)
				return
			}
		}
	}
}

func (bc *BrowserClient) readLoop(ctx context.Context) {
	for {
		_, data, err := bc.conn.Read(ctx)
		if err != nil {
			return
		}

		var msg browserMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			bc.logger.Printf("invalid browser message: %v", err)
			continue
		}

		bc.handleMessage(ctx, msg)
	}
}

func (bc *BrowserClient) handleMessage(ctx context.Context, msg browserMessage) {
	switch msg.Type {
	case "subscribe":
		bc.mu.Lock()
		bc.agentName = msg.Agent
		bc.mu.Unlock()
		bc.logger.Printf("browser subscribed to agent %q", msg.Agent)

		// Send current state snapshot if agent is connected.
		if session := bc.server.getAgent(msg.Agent); session != nil {
			state := session.gameClient.GetState()
			if state != nil {
				raw, err := json.Marshal(state)
				if err == nil {
					sm := serverMessage{
						Type:    "game_message",
						Agent:   msg.Agent,
						Message: raw,
					}
					if data, err := json.Marshal(sm); err == nil {
						select {
						case bc.sendCh <- data:
						default:
						}
					}
				}
			}
		}

	case "command":
		session := bc.server.getAgent(msg.Agent)
		if session == nil {
			bc.sendError("agent not found: " + msg.Agent)
			return
		}
		payload := msg.Payload
		if payload == nil {
			payload = make(map[string]any)
		}
		err := session.SendCommand(ctx, protocol.Message{
			Type:    msg.Command,
			Payload: payload,
		})
		if err != nil {
			bc.sendError("command failed: " + err.Error())
		}

	case "list_agents":
		agents := bc.server.ListAgents()
		sm := serverMessage{
			Type:   "agent_list",
			Agents: agents,
		}
		if data, err := json.Marshal(sm); err == nil {
			select {
			case bc.sendCh <- data:
			default:
			}
		}

	case "get_cache":
		data := bc.server.cache.GetCacheData(msg.Key)
		sm := serverMessage{
			Type:    "cache_data",
			Key:     msg.Key,
			Message: data,
		}
		if out, err := json.Marshal(sm); err == nil {
			select {
			case bc.sendCh <- out:
			default:
			}
		}

	default:
		bc.sendError("unknown message type: " + msg.Type)
	}
}

func (bc *BrowserClient) sendError(errMsg string) {
	sm := serverMessage{
		Type:  "error",
		Error: errMsg,
	}
	if data, err := json.Marshal(sm); err == nil {
		select {
		case bc.sendCh <- data:
		default:
		}
	}
}

// browserMessage is a message sent from the browser to the server.
type browserMessage struct {
	Type    string         `json:"type"`
	Agent   string         `json:"agent,omitempty"`
	Command string         `json:"command,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
	Key     string         `json:"key,omitempty"`
}

// serverMessage is a message sent from the server to the browser.
type serverMessage struct {
	Type      string           `json:"type"`
	Agent     string           `json:"agent,omitempty"`
	Message   json.RawMessage  `json:"message,omitempty"`
	Agents    []AgentInfo      `json:"agents,omitempty"`
	Connected *bool            `json:"connected,omitempty"`
	Error     string           `json:"error,omitempty"`
	Key       string           `json:"key,omitempty"`
}
