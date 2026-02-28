// MCP stdio-to-WebSocket Bridge
// Bridges between stdio MCP protocol (Claude Code) and WebSocket game API (Spacemolt)
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
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

// Bridge connects stdio MCP to WebSocket game API
type Bridge struct {
	gameClient *game.Client
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *log.Logger
	mu         sync.Mutex
	initialized bool
}

// NewBridge creates a new MCP bridge
func NewBridge(gameURL, username, password string, logger *log.Logger) *Bridge {
	ctx, cancel := context.WithCancel(context.Background())

	// Create game client
	client := game.NewClient(gameURL, username, password, logger)

	return &Bridge{
		gameClient: client,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
	}
}

// Initialize the bridge (connect and login)
func (b *Bridge) Initialize() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.initialized {
		return nil
	}

	b.logger.Printf("Connecting to game server...")
	if err := b.gameClient.Connect(b.ctx); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Wait for first message (ready signal)
	b.logger.Printf("Waiting for connection...")
	select {
	case <-b.gameClient.Ready():
		b.logger.Printf("Connection ready")
	case <-time.After(15 * time.Second):
		return fmt.Errorf("connection timeout waiting for ready signal")
	case <-b.ctx.Done():
		return b.ctx.Err()
	}

	// Small delay to allow connection to stabilize
	time.Sleep(100 * time.Millisecond)

	b.logger.Printf("Logging in...")
	if err := b.gameClient.Login(b.ctx); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	b.initialized = true
	b.logger.Printf("Login successful")

	return nil
}

// Run starts the stdio MCP server
func (b *Bridge) Run() error {
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	b.logger.Printf("MCP stdio-to-WebSocket bridge starting")

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
				b.logger.Printf("ERROR: failed to write response: %v", err)
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

	b.logger.Printf("Request: method=%s id=%v", req.Method, req.ID)

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
		b.logger.Printf("ERROR: failed to initialize: %v", err)
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

// handleToolsList returns available game commands
func (b *Bridge) handleToolsList(req JSONRPCRequest) *JSONRPCResponse {
	// Use auto-generated tools from OpenAPI spec
	tools := GeneratedTools()

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

	b.logger.Printf("Tool call: %s", params.Name)

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

	// Execute tool by sending generic command to game server
	// The tool name maps directly to the game command type
	var err error
	var responseData []byte

	// Send the command with arguments as payload
	err = b.gameClient.Send(b.ctx, protocol.Message{
		Type:      params.Name,
		Payload:   params.Arguments,
		Timestamp: time.Now().UnixMilli(),
	})

	if err != nil {
		return b.errorResponse(req.ID, fmt.Sprintf("Failed to execute command: %v", err))
	}

	// Wait briefly for response (game server should respond quickly)
	time.Sleep(500 * time.Millisecond)

	// Get the latest response from game client
	// For most commands, the response will be in the last message received
	state := b.gameClient.GetState()
	responseData, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		return b.errorResponse(req.ID, fmt.Sprintf("Failed to marshal response: %v", err))
	}

	// Format as MCP tool result
	toolResult := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(responseData),
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

func (b *Bridge) errorResponse(id any, msg string) *JSONRPCResponse {
	b.logger.Printf("ERROR: %s", msg)

	// Return error as tool result (not JSON-RPC error)
	errorContent := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": fmt.Sprintf("Error: %s", msg),
			},
		},
		"isError": true,
	}
	errorJSON, _ := json.Marshal(errorContent)
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  errorJSON,
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
	gameURL := flag.String("url", "wss://game.spacemolt.com/ws", "Game WebSocket URL")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	debug := flag.Bool("debug", false, "Enable game client debug logging")
	flag.Parse()

	// Setup logging (to stderr)
	logger := log.New(os.Stderr, "", log.LstdFlags)
	if !*verbose {
		logger.SetOutput(io.Discard)
	}

	// Load credentials
	if *credsPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: mcp-ws-bridge -creds <path-to-credentials.json>")
		os.Exit(1)
	}

	credsData, err := os.ReadFile(*credsPath)
	if err != nil {
		logger.Printf("ERROR: failed to read credentials: %v", err)
		os.Exit(1)
	}

	var creds Credentials
	if err := json.Unmarshal(credsData, &creds); err != nil {
		logger.Printf("ERROR: failed to parse credentials: %v", err)
		os.Exit(1)
	}

	// Validate URL
	if !strings.HasPrefix(*gameURL, "ws://") && !strings.HasPrefix(*gameURL, "wss://") {
		logger.Printf("ERROR: game URL must start with ws:// or wss://")
		os.Exit(1)
	}

	// Create and run bridge
	bridge := NewBridge(*gameURL, creds.Username, creds.Password, logger)
	bridge.gameClient.SetDebugLogging(*debug)
	defer bridge.cancel()

	if err := bridge.Run(); err != nil {
		logger.Printf("ERROR: bridge error: %v", err)
		os.Exit(1)
	}
}
