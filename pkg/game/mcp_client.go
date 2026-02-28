package game

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"
)

// MCPClient represents a client for communicating with MCP servers over stdio
type MCPClient struct {
	serverPath string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	logger     *log.Logger
}

// NewMCPClient creates a new MCP client for the specified server executable
func NewMCPClient(serverPath string, logger *log.Logger) *MCPClient {
	if logger == nil {
		logger = log.New(io.Discard, "", log.LstdFlags)
	}

	return &MCPClient{
		serverPath: serverPath,
		logger:     logger,
	}
}

// Start starts the MCP server process and establishes stdio communication
func (m *MCPClient) Start(ctx context.Context) error {
	// Start the crafting-server process
	m.cmd = exec.CommandContext(ctx, m.serverPath, "--db", "data/crafting/crafting.db")

	var err error
	m.stdin, err = m.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	m.stdout, err = m.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	m.stderr, err = m.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Capture stderr in background for diagnostics
	go func() {
		data, _ := io.ReadAll(m.stderr)
		if len(data) > 0 {
			m.logger.Printf("Crafting server stderr: %s", string(data))
		}
	}()

	// Start the process
	if err := m.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start crafting server: %w", err)
	}

	m.logger.Printf("Crafting MCP server started (PID: %d)", m.cmd.Process.Pid)

	// Give the server a moment to initialize, then verify it's still alive
	time.Sleep(500 * time.Millisecond)

	if !m.isAlive() {
		_ = m.cmd.Wait()
		return fmt.Errorf("crafting server exited immediately after start (check stderr above)")
	}

	// Send MCP initialize handshake
	if err := m.initialize(ctx); err != nil {
		_ = m.Stop()
		return fmt.Errorf("MCP initialize handshake failed: %w", err)
	}

	return nil
}

// initialize sends the MCP protocol initialize handshake
func (m *MCPClient) initialize(ctx context.Context) error {
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "spacemolt",
				"version": "1.0",
			},
		},
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal initialize: %w", err)
	}

	requestJSON = append(requestJSON, '\n')
	if _, err := m.stdin.Write(requestJSON); err != nil {
		return fmt.Errorf("failed to write initialize: %w", err)
	}

	resp, err := m.readResponse(ctx, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to read initialize response: %w", err)
	}

	m.logger.Printf("MCP initialized: %s", string(resp))

	// Send initialized notification — some servers respond with an error
	// (non-spec-compliant), so read and discard any response.
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	notifJSON, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("failed to marshal initialized notification: %w", err)
	}
	notifJSON = append(notifJSON, '\n')
	if _, err = m.stdin.Write(notifJSON); err != nil {
		return fmt.Errorf("failed to write initialized notification: %w", err)
	}

	// Drain any spurious response (1s timeout — it's optional)
	_, _ = m.readResponse(ctx, time.Second)

	return nil
}

// isAlive checks if the server process is still running
func (m *MCPClient) isAlive() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	// ProcessState is only set after Wait() returns; nil means still running
	return m.cmd.ProcessState == nil
}

// Stop gracefully shuts down the MCP server
func (m *MCPClient) Stop() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}

	// Close stdin to signal EOF to the server
	if m.stdin != nil {
		_ = m.stdin.Close()
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		done <- m.cmd.Wait()
	}()

	select {
	case err := <-done:
		m.logger.Printf("Crafting server stopped: %v", err)
		return err
	case <-time.After(5 * time.Second):
		// Timeout - kill the process
		if err := m.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill crafting server: %w", err)
		}
		return fmt.Errorf("crafting server did not stop gracefully, killed it")
	}
}

// CallTool invokes a tool on the MCP server
func (m *MCPClient) CallTool(ctx context.Context, toolName string, params map[string]any) (map[string]any, error) {
	// Build JSON-RPC request
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": params,
		},
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	requestJSON = append(requestJSON, '\n')
	if _, err := m.stdin.Write(requestJSON); err != nil {
		return nil, fmt.Errorf("failed to write to server stdin: %w", err)
	}

	m.logger.Printf("Sent MCP request: %s", toolName)

	// Read response
	response, err := m.readResponse(ctx, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var resp struct {
		JSONRPC string                 `json:"jsonrpc"`
		ID      any                      `json:"id,omitempty"`
		Result  map[string]any `json:"result,omitempty"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

// readResponse reads a complete response from stdout
func (m *MCPClient) readResponse(ctx context.Context, timeout time.Duration) ([]byte, error) {
	// Set read deadline
	deadline := time.Now().Add(timeout)

	response := bytes.NewBuffer([]byte{})
	reader := bufio.NewReader(m.stdout)

	// Read until we get a complete JSON object
	depth := 0
	inString := false
	escapeNext := false

	for {
		// Check timeout
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for response")
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("read error: %w", err)
		}

		char := rune(b)

		// Simple JSON parsing to find object boundaries
		if escapeNext {
			escapeNext = false
			response.WriteByte(b)
			continue
		}

		switch char {
		case '{':
			if !inString {
				depth++
			}
			response.WriteByte(b)
		case '}':
			if !inString {
				depth--
			}
			response.WriteByte(b)
		case '"':
			response.WriteByte(b)
			if !escapeNext {
				inString = !inString
			}
		case '\\':
			response.WriteByte(b)
			escapeNext = true
		case '\n':
			// Check if we have a complete JSON object
			if depth == 0 && response.Len() > 0 {
				// Check if it's an empty line or we have content
				if response.Len() > 1 {
					return response.Bytes(), nil
				}
			}
			// Otherwise, continue reading
		default:
			response.WriteByte(b)
		}
	}

	return response.Bytes(), nil
}

// Close closes all connections and stops the server
func (m *MCPClient) Close() error {
	var errs []error

	if m.stdin != nil {
		if err := m.stdin.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if m.stdout != nil {
		if err := m.stdout.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if m.stderr != nil {
		if err := m.stderr.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if err := m.Stop(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}

// MCPManager manages the lifecycle of MCP client connections
type MCPManager struct {
	clients map[string]*MCPClient
	mu      sync.RWMutex
	logger  *log.Logger
}

// NewMCPManager creates a new MCP manager
func NewMCPManager(logger *log.Logger) *MCPManager {
	if logger == nil {
		logger = log.New(io.Discard, "", log.LstdFlags)
	}

	return &MCPManager{
		clients: make(map[string]*MCPClient),
		logger:  logger,
	}
}

// GetClient gets or creates an MCP client for the specified server
func (mgr *MCPManager) GetClient(ctx context.Context, serverPath string) (*MCPClient, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// Check if client already exists and is still alive
	if client, exists := mgr.clients[serverPath]; exists {
		if client.isAlive() {
			return client, nil
		}
		mgr.logger.Printf("MCP client for %s is dead, restarting...", serverPath)
		_ = client.Close()
		delete(mgr.clients, serverPath)
	}

	// Create new client
	client := NewMCPClient(serverPath, mgr.logger)

	// Start the server process
	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start MCP client: %w", err)
	}

	mgr.clients[serverPath] = client
	mgr.logger.Printf("MCP client created for: %s", serverPath)

	return client, nil
}

// CloseAll closes all MCP clients
func (mgr *MCPManager) CloseAll() error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	var errs []error

	for serverPath, client := range mgr.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", serverPath, err))
		}
	}

	mgr.clients = make(map[string]*MCPClient)

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}
