// MCP stdio-to-SSE Bridge
// Bridges between stdio MCP protocol (Claude Code) and SSE MCP protocol (Spacemolt game server)
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	defaultGameServerURL = "https://game.spacemolt.com/mcp"
)

// Credentials holds login information
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
}

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidReq     = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// SSEClient handles communication with the game MCP server via SSE
type SSEClient struct {
	serverURL string
	client    *http.Client
	requestID int
	mu        sync.Mutex
}

// NewSSEClient creates a new SSE MCP client
func NewSSEClient(serverURL string) *SSEClient {
	return &SSEClient{
		serverURL: serverURL,
		client: &http.Client{
			Timeout: 0, // No timeout for SSE
		},
		requestID: 1,
	}
}

// Call makes a JSON-RPC call via SSE
func (c *SSEClient) Call(method string, params map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	reqID := c.requestID
	c.requestID++
	c.mu.Unlock()

	// Build request
	var paramsRaw json.RawMessage
	if params != nil {
		var err error
		paramsRaw, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  paramsRaw,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequest("POST", c.serverURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Send request and read SSE response
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the entire response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse SSE format
	bodyStr := string(body)
	if strings.HasPrefix(bodyStr, "data: ") {
		for _, line := range strings.Split(bodyStr, "\n") {
			line = strings.TrimSpace(line)
			if data, found := strings.CutPrefix(line, "data: "); found {
				var rpcResp JSONRPCResponse
				if err := json.Unmarshal([]byte(data), &rpcResp); err == nil {
					if rpcResp.Error != nil {
						return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
					}
					return rpcResp.Result, nil
				}
			}
		}
		return nil, fmt.Errorf("no valid data event in SSE response")
	}

	// Try as plain JSON response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// Bridge connects stdio MCP to SSE MCP
type Bridge struct {
	gameClient  *SSEClient
	credentials *Credentials
	sessionID   string
	logger      *slog.Logger
	mu          sync.Mutex
	initialized bool
}

// NewBridge creates a new MCP bridge
func NewBridge(gameServerURL string, creds *Credentials, logger *slog.Logger) *Bridge {
	return &Bridge{
		gameClient:  NewSSEClient(gameServerURL),
		credentials: creds,
		logger:      logger,
	}
}

// Initialize the bridge (init game session and login)
func (b *Bridge) Initialize() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.initialized {
		return nil
	}

	// Initialize game MCP session
	b.logger.Info("initializing game MCP session")
	params := map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "mcp-sse-bridge",
			"version": "1.0.0",
		},
	}

	_, err := b.gameClient.Call("initialize", params)
	if err != nil {
		return fmt.Errorf("game server initialize failed: %w", err)
	}

	// Login to get session ID
	b.logger.Info("logging in", "username", b.credentials.Username)
	loginParams := map[string]any{
		"username": b.credentials.Username,
		"password": b.credentials.Password,
	}

	result, err := b.gameClient.Call("login", loginParams)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	var loginResp struct {
		SessionID string `json:"session_id"`
		Username  string `json:"username"`
	}

	if err := json.Unmarshal(result, &loginResp); err != nil {
		return fmt.Errorf("failed to parse login response: %w", err)
	}

	b.sessionID = loginResp.SessionID
	b.initialized = true
	b.logger.Info("login successful", "session_id", b.sessionID)

	return nil
}

// Run starts the stdio MCP server
func (b *Bridge) Run() error {
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	b.logger.Info("MCP stdio-to-SSE bridge starting")

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("reading input: %w", err)
		}

		resp := b.handleRequest(line)
		if resp != nil {
			if err := b.writeResponse(writer, resp); err != nil {
				b.logger.Error("failed to write response", "error", err)
			}
		}
	}
}

// handleRequest processes a single stdio MCP request
func (b *Bridge) handleRequest(data []byte) *JSONRPCResponse {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCError{
				Code:    ErrCodeParse,
				Message: "Parse error",
				Data:    err.Error(),
			},
		}
	}

	b.logger.Debug("received request", "method", req.Method, "id", req.ID)

	// Handle MCP protocol methods
	switch req.Method {
	case "initialize":
		return b.handleInitialize(req)
	case "tools/list":
		return b.handleToolsList(req)
	case "tools/call":
		return b.handleToolsCall(req)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    ErrCodeMethodNotFound,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

// handleInitialize handles the initialize request
func (b *Bridge) handleInitialize(req JSONRPCRequest) *JSONRPCResponse {
	// Initialize connection to game server
	if err := b.Initialize(); err != nil {
		b.logger.Error("failed to initialize", "error", err)
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    ErrCodeInternal,
				Message: fmt.Sprintf("Failed to initialize game connection: %v", err),
			},
		}
	}

	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]any{
			"name":    "spacemolt-game",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}

	resultJSON, _ := json.Marshal(result)
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleToolsList returns available game tools
func (b *Bridge) handleToolsList(req JSONRPCRequest) *JSONRPCResponse {
	// Return a curated list of useful game commands
	tools := []map[string]any{
		{
			"name":        "get_status",
			"description": "Get current player and ship status",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_ship",
			"description": "Get detailed ship information",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_cargo",
			"description": "Get cargo contents",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_system",
			"description": "Get current system details",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_map",
			"description": "Get all discovered star systems",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_skills",
			"description": "Get skills and progress",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_recipes",
			"description": "Get available crafting recipes",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_listings",
			"description": "Get market listings at current base",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "help",
			"description": "Get help about game commands",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "Command to get help for",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Category to get help for",
					},
				},
			},
		},
	}

	result := map[string]any{
		"tools": tools,
	}

	resultJSON, _ := json.Marshal(result)
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleToolsCall forwards tool calls to the game server
func (b *Bridge) handleToolsCall(req JSONRPCRequest) *JSONRPCResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    ErrCodeInvalidParams,
				Message: "Invalid params",
				Data:    err.Error(),
			},
		}
	}

	b.logger.Debug("calling tool", "name", params.Name)

	// Ensure initialized
	if !b.initialized {
		if err := b.Initialize(); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    ErrCodeInternal,
					Message: "Not initialized",
					Data:    err.Error(),
				},
			}
		}
	}

	// Add session_id to arguments
	if params.Arguments == nil {
		params.Arguments = make(map[string]any)
	}
	params.Arguments["session_id"] = b.sessionID

	// Forward to game server
	result, err := b.gameClient.Call(params.Name, params.Arguments)
	if err != nil {
		// Return error as tool result (not JSON-RPC error)
		errorContent := map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": fmt.Sprintf("Error: %v", err),
				},
			},
			"isError": true,
		}
		errorJSON, _ := json.Marshal(errorContent)
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  errorJSON,
		}
	}

	// Format as MCP tool result
	toolResult := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(result),
			},
		},
	}

	resultJSON, _ := json.Marshal(toolResult)
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// writeResponse writes a JSON-RPC response to stdout
func (b *Bridge) writeResponse(w io.Writer, resp *JSONRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshaling response: %w", err)
	}

	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func main() {
	// Parse flags
	credsPath := flag.String("creds", "", "Path to credentials JSON file")
	gameServerURL := flag.String("server", defaultGameServerURL, "Game server URL")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	// Setup logging
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Load credentials
	if *credsPath == "" {
		logger.Error("credentials path required")
		fmt.Fprintln(os.Stderr, "Usage: mcp-sse-bridge -creds <path-to-credentials.json>")
		os.Exit(1)
	}

	credsData, err := os.ReadFile(*credsPath)
	if err != nil {
		logger.Error("failed to read credentials", "error", err)
		os.Exit(1)
	}

	var creds Credentials
	if err := json.Unmarshal(credsData, &creds); err != nil {
		logger.Error("failed to parse credentials", "error", err)
		os.Exit(1)
	}

	// Create and run bridge
	bridge := NewBridge(*gameServerURL, &creds, logger)
	if err := bridge.Run(); err != nil {
		logger.Error("bridge error", "error", err)
		os.Exit(1)
	}
}
