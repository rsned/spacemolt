package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MCPBackend implements Backend over MCP Streamable HTTP (JSON-RPC 2.0).
type MCPBackend struct {
	baseURL      string
	client       *http.Client
	mcpSessionID string
	mu           sync.Mutex
	connected    bool
	nextID       atomic.Int64
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
	Error   *jsonRPCError  `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewMCPBackend creates an MCP Streamable HTTP backend.
func NewMCPBackend(baseURL string, sharedTransport *http.Transport) *MCPBackend {
	transport := sharedTransport
	if transport == nil {
		transport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}
	}

	return &MCPBackend{
		baseURL: baseURL,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (b *MCPBackend) Name() string { return "mcp" }

func (b *MCPBackend) Connect(ctx context.Context) error {
	// Send initialize request
	resp, err := b.rpcCall(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "spacemolt-benchmark",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}

	_ = resp // We track the session ID via response headers in rpcCall

	// Send initialized notification (required by MCP spec before tool calls)
	_ = b.sendNotification(ctx, "notifications/initialized")

	b.mu.Lock()
	b.connected = true
	b.mu.Unlock()

	return nil
}

// sendNotification sends a JSON-RPC notification (no id, no response expected).
func (b *MCPBackend) sendNotification(ctx context.Context, method string) error {
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.baseURL+"/mcp", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	b.mu.Lock()
	if b.mcpSessionID != "" {
		req.Header.Set("Mcp-Session-Id", b.mcpSessionID)
	}
	b.mu.Unlock()

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (b *MCPBackend) Login(ctx context.Context, username, password string) error {
	_, err := b.rpcCall(ctx, "tools/call", map[string]any{
		"name": "login",
		"arguments": map[string]any{
			"username": username,
			"password": password,
		},
	})
	return err
}

func (b *MCPBackend) SendCommand(ctx context.Context, command string, payload map[string]any) (map[string]any, error) {
	args := payload
	if args == nil {
		args = map[string]any{}
	}

	// MCP tool calls need the session_id from the MCP session
	b.mu.Lock()
	sid := b.mcpSessionID
	b.mu.Unlock()
	if sid != "" {
		args["session_id"] = sid
	}

	return b.rpcCall(ctx, "tools/call", map[string]any{
		"name":      command,
		"arguments": args,
	})
}

func (b *MCPBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = false
	b.mcpSessionID = ""
	return nil
}

func (b *MCPBackend) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

func (b *MCPBackend) rpcCall(ctx context.Context, method string, params any) (map[string]any, error) {
	id := b.nextID.Add(1)

	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.baseURL+"/mcp", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	b.mu.Lock()
	if b.mcpSessionID != "" {
		req.Header.Set("Mcp-Session-Id", b.mcpSessionID)
	}
	b.mu.Unlock()

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Track session ID from response header
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		b.mu.Lock()
		b.mcpSessionID = sid
		b.mu.Unlock()
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("429 rate limited")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")

	// SSE response: server streams events for long-running tool calls
	if strings.HasPrefix(ct, "text/event-stream") {
		return b.parseSSE(resp.Body, id)
	}

	// JSON response: direct result
	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode rpc response: %w", err)
	}

	return b.handleRPCResponse(rpcResp)
}

// parseSSE reads SSE events and returns the final JSON-RPC response matching our request ID.
func (b *MCPBackend) parseSSE(body io.Reader, requestID int64) (map[string]any, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024) // up to 1MB lines

	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if d, ok := strings.CutPrefix(line, "data: "); ok {
			dataLines = append(dataLines, d)
			continue
		}

		// Empty line = end of event
		if line == "" && len(dataLines) > 0 {
			joined := strings.Join(dataLines, "\n")
			dataLines = nil

			var rpcResp jsonRPCResponse
			if err := json.Unmarshal([]byte(joined), &rpcResp); err != nil {
				continue // skip non-JSON events (notifications, etc.)
			}

			// We want the response matching our request ID
			if rpcResp.ID == requestID {
				return b.handleRPCResponse(rpcResp)
			}
			// Otherwise it's a notification or different response — skip
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("sse read: %w", err)
	}

	return nil, fmt.Errorf("sse stream ended without response for id %d", requestID)
}

func (b *MCPBackend) handleRPCResponse(rpcResp jsonRPCResponse) (map[string]any, error) {
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// Check for MCP tool error in the content array
	if rpcResp.Result != nil {
		if isErr, ok := rpcResp.Result["isError"].(bool); ok && isErr {
			// Extract error text from content array
			if content, ok := rpcResp.Result["content"].([]any); ok && len(content) > 0 {
				if item, ok := content[0].(map[string]any); ok {
					if text, ok := item["text"].(string); ok {
						// Parse "Error: code: message" format
						code, msg := parseToolError(text)
						return map[string]any{"code": code}, fmt.Errorf("%s: %s", code, msg)
					}
				}
			}
			return nil, fmt.Errorf("tool error")
		}
	}

	return rpcResp.Result, nil
}

// parseToolError parses "Error: code: message" from MCP tool error text.
func parseToolError(text string) (code, msg string) {
	s := strings.TrimPrefix(text, "Error: ")
	if idx := strings.Index(s, ": "); idx >= 0 {
		return s[:idx], s[idx+2:]
	}
	return s, s
}
