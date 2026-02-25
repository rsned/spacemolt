package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/pkg/game"
)

// WSBackend implements Backend over a WebSocket connection.
type WSBackend struct {
	url       string
	conn      *websocket.Conn
	mu        sync.Mutex
	connected bool
	metrics   *Metrics

	// Response routing: the listen goroutine sends responses here.
	responseCh chan wsResponse
	stopCh     chan struct{}
}

type wsResponse struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}

// NewWSBackend creates a WebSocket backend.
func NewWSBackend(url string, metrics *Metrics) *WSBackend {
	return &WSBackend{
		url:        url,
		metrics:    metrics,
		responseCh: make(chan wsResponse, 64),
		stopCh:     make(chan struct{}),
	}
}

func (b *WSBackend) Name() string { return "ws" }

func (b *WSBackend) Connect(ctx context.Context) error {
	conn, resp, err := websocket.Dial(ctx, b.url, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"User-Agent": []string{game.UserAgent},
		},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}

	conn.SetReadLimit(10 * 1024 * 1024) // 10MB

	b.mu.Lock()
	b.conn = conn
	b.connected = true
	b.mu.Unlock()

	go b.listen()

	// Wait for welcome message
	if err := b.waitForType(ctx, "welcome", 10*time.Second); err != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		b.mu.Lock()
		b.connected = false
		b.conn = nil
		b.mu.Unlock()
		return fmt.Errorf("waiting for welcome: %w", err)
	}

	return nil
}

func (b *WSBackend) Login(ctx context.Context, username, password string) error {
	msg := map[string]any{
		"type": "login",
		"payload": map[string]any{
			"username": username,
			"password": password,
		},
		"timestamp": time.Now().UnixMilli(),
	}

	if err := b.send(ctx, msg); err != nil {
		return err
	}

	return b.waitForType(ctx, "logged_in", 10*time.Second)
}

func (b *WSBackend) SendCommand(ctx context.Context, command string, payload map[string]any) (map[string]any, error) {
	msg := map[string]any{
		"type":      command,
		"timestamp": time.Now().UnixMilli(),
	}
	if payload != nil {
		msg["payload"] = payload
	}

	if err := b.send(ctx, msg); err != nil {
		return nil, err
	}

	// Wait for ok, error, or a typed response matching the command
	return b.waitForCommandResponse(ctx, command, 15*time.Second)
}

func (b *WSBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case b.stopCh <- struct{}{}:
	default:
	}

	if b.conn != nil {
		b.connected = false
		err := b.conn.Close(websocket.StatusNormalClosure, "benchmark done")
		b.conn = nil
		return err
	}
	return nil
}

func (b *WSBackend) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

func (b *WSBackend) send(ctx context.Context, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return conn.Write(writeCtx, websocket.MessageText, data)
}

func (b *WSBackend) listen() {
	for {
		select {
		case <-b.stopCh:
			return
		default:
		}

		b.mu.Lock()
		conn := b.conn
		b.mu.Unlock()

		if conn == nil {
			return
		}

		_, data, err := conn.Read(context.Background())
		if err != nil {
			b.mu.Lock()
			b.connected = false
			b.mu.Unlock()
			if b.metrics != nil {
				b.metrics.RecordDisconnect()
			}
			return
		}

		// The server may send multiple JSON objects in one message frame
		decoder := json.NewDecoder(bytes.NewReader(data))
		for decoder.More() {
			var resp wsResponse
			if err := decoder.Decode(&resp); err != nil {
				break
			}

			select {
			case b.responseCh <- resp:
			default:
				// Drop if channel is full (non-critical messages like tick, state_update)
			}
		}
	}
}

func (b *WSBackend) waitForType(ctx context.Context, msgType string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case resp := <-b.responseCh:
			if resp.Type == msgType {
				return nil
			}
			if resp.Type == "error" {
				msg, _ := resp.Payload["message"].(string)
				return fmt.Errorf("server error: %s", msg)
			}
			// Ignore other message types (tick, state_update, etc.)
		case <-deadline:
			return fmt.Errorf("timeout waiting for %s", msgType)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (b *WSBackend) waitForCommandResponse(ctx context.Context, _ string, timeout time.Duration) (map[string]any, error) {
	deadline := time.After(timeout)
	for {
		select {
		case resp := <-b.responseCh:
			switch resp.Type {
			case "ok":
				return resp.Payload, nil
			case "error", "action_error":
				msg, _ := resp.Payload["message"].(string)
				code, _ := resp.Payload["code"].(string)
				return resp.Payload, fmt.Errorf("%s: %s", code, msg)
			case "docked", "undocked":
				return resp.Payload, nil
			case "tick", "state_update", "chat_message",
				"combat_update", "nearby_update",
				"poi_arrival", "poi_departure",
				"gameplay_tip", "skill_level_up":
				// Ignore async events, keep waiting
				continue
			default:
				// Treat any other response as a command result
				return resp.Payload, nil
			}
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for command response")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
