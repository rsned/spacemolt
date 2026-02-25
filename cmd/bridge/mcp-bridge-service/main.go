// MCP Bridge Service - Multi-Agent Connection Pool
// Provides a single MCP server that manages connections for multiple agents
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
	"path/filepath"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

const protocolVersion = "2024-11-05"

// ServiceConfig holds the bridge service configuration
type ServiceConfig struct {
	AgentsDir         string `json:"agents_dir"`
	GameServerURL     string `json:"game_server_url"`
	IdleTimeout       string `json:"idle_timeout"`
	MaxConnections    int    `json:"max_connections"`
	KeepAliveInterval string `json:"keep_alive_interval"`
	ReconnectAttempts int    `json:"reconnect_attempts"`
}

// ParsedConfig holds the parsed configuration with duration fields
type ParsedConfig struct {
	AgentsDir         string
	GameServerURL     string
	IdleTimeout       time.Duration
	MaxConnections    int
	KeepAliveInterval time.Duration
	ReconnectAttempts int
}

// Credentials holds agent login information
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
}

// ConnectionState represents the state of an agent connection
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateReconnecting
	StateError
)

func (s ConnectionState) String() string {
	return [...]string{"Disconnected", "Connecting", "Connected", "Reconnecting", "Error"}[s]
}

// AgentConnection represents a single agent's connection to the game
type AgentConnection struct {
	agentID     string
	client      *game.Client
	credentials Credentials
	lastUsed    time.Time
	state       ConnectionState
	reconnectCh chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	logger      *log.Logger
}

// ConnectionPool manages multiple agent connections
type ConnectionPool struct {
	mu          sync.RWMutex
	connections map[string]*AgentConnection
	config      *ParsedConfig
	logger      *log.Logger
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config *ParsedConfig, logger *log.Logger) *ConnectionPool {
	return &ConnectionPool{
		connections: make(map[string]*AgentConnection),
		config:      config,
		logger:      logger,
	}
}

// GetConnection retrieves or creates a connection for an agent
func (p *ConnectionPool) GetConnection(agentID string) (*AgentConnection, error) {
	// Try read lock first for existing connections
	p.mu.RLock()
	conn, exists := p.connections[agentID]
	p.mu.RUnlock()

	if exists {
		conn.mu.RLock()
		state := conn.state
		conn.mu.RUnlock()

		switch state {
		case StateConnected:
			conn.mu.Lock()
			conn.lastUsed = time.Now()
			conn.mu.Unlock()
			return conn, nil
		case StateConnecting, StateReconnecting:
			// Wait for connection to complete
			time.Sleep(2 * time.Second)
			return p.GetConnection(agentID) // Retry
		}
	}

	// Need to create new connection
	return p.createConnection(agentID)
}

// loadCredentials loads agent credentials from file
func (p *ConnectionPool) loadCredentials(agentID string) (Credentials, error) {
	credPath := filepath.Join(p.config.AgentsDir, agentID, "credentials.json")
	data, err := os.ReadFile(credPath)
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to read credentials for %s: %w", agentID, err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("failed to parse credentials for %s: %w", agentID, err)
	}

	return creds, nil
}

// createConnection creates and initializes a new agent connection
func (p *ConnectionPool) createConnection(agentID string) (*AgentConnection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if conn, exists := p.connections[agentID]; exists {
		return conn, nil
	}

	// Check connection limit
	if len(p.connections) >= p.config.MaxConnections {
		return nil, fmt.Errorf("connection pool full (%d/%d)", len(p.connections), p.config.MaxConnections)
	}

	// Load credentials
	creds, err := p.loadCredentials(agentID)
	if err != nil {
		return nil, err
	}

	// Create connection
	ctx, cancel := context.WithCancel(context.Background())
	logger := log.New(os.Stderr, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	conn := &AgentConnection{
		agentID:     agentID,
		credentials: creds,
		ctx:         ctx,
		cancel:      cancel,
		reconnectCh: make(chan struct{}, 1),
		logger:      logger,
		state:       StateDisconnected,
	}

	// Create game client
	gameLogger := log.New(os.Stderr, fmt.Sprintf("[%s:game] ", agentID), log.LstdFlags)
	conn.client = game.NewClient(p.config.GameServerURL, creds.Username, creds.Password, gameLogger)

	// Connect and login
	if err := p.connectAgent(conn); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect %s: %w", agentID, err)
	}

	// Store in pool
	p.connections[agentID] = conn

	// Start maintenance goroutine
	go p.maintainConnection(conn)

	p.logger.Printf("Created connection for agent %s (%s)", agentID, creds.Username)

	return conn, nil
}

// connectAgent establishes connection and logs in
func (p *ConnectionPool) connectAgent(conn *AgentConnection) error {
	conn.mu.Lock()
	conn.state = StateConnecting
	conn.mu.Unlock()

	if err := conn.client.Connect(conn.ctx); err != nil {
		conn.mu.Lock()
		conn.state = StateError
		conn.mu.Unlock()
		return err
	}

	<-conn.client.Ready()
	time.Sleep(1 * time.Second)

	if err := conn.client.Login(conn.ctx); err != nil {
		conn.mu.Lock()
		conn.state = StateError
		conn.mu.Unlock()
		return err
	}

	// Wait a moment for state to be fully populated after login
	time.Sleep(2 * time.Second)

	conn.mu.Lock()
	conn.state = StateConnected
	conn.lastUsed = time.Now()
	conn.mu.Unlock()

	conn.logger.Printf("Connected successfully as %s", conn.credentials.Username)

	return nil
}

// maintainConnection handles keep-alive and reconnection
func (p *ConnectionPool) maintainConnection(conn *AgentConnection) {
	ticker := time.NewTicker(p.config.KeepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-conn.ctx.Done():
			conn.logger.Printf("Connection context cancelled, stopping maintenance")
			return

		case <-ticker.C:
			// Keep-alive: just update last used time if recently used
			conn.mu.RLock()
			lastUsed := conn.lastUsed
			state := conn.state
			conn.mu.RUnlock()

			if time.Since(lastUsed) < p.config.KeepAliveInterval*2 {
				if state != StateConnected {
					// Try to reconnect if not connected
					select {
					case conn.reconnectCh <- struct{}{}:
					default:
					}
				}
			}

		case <-conn.reconnectCh:
			p.reconnectAgent(conn)
		}
	}
}

// reconnectAgent attempts to reconnect a disconnected agent
func (p *ConnectionPool) reconnectAgent(conn *AgentConnection) {
	conn.mu.Lock()
	if conn.state == StateConnecting || conn.state == StateReconnecting {
		conn.mu.Unlock()
		return // Already reconnecting
	}
	conn.state = StateReconnecting
	conn.mu.Unlock()

	conn.logger.Printf("Starting reconnection...")

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for attempt := 1; attempt <= p.config.ReconnectAttempts; attempt++ {
		conn.logger.Printf("Reconnection attempt %d/%d", attempt, p.config.ReconnectAttempts)

		if err := p.connectAgent(conn); err == nil {
			conn.logger.Printf("Reconnected successfully")
			return
		}

		if attempt < p.config.ReconnectAttempts {
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}

	conn.mu.Lock()
	conn.state = StateError
	conn.mu.Unlock()

	conn.logger.Printf("Failed to reconnect after %d attempts", p.config.ReconnectAttempts)
}

// CleanupIdle removes idle connections
func (p *ConnectionPool) CleanupIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for agentID, conn := range p.connections {
		conn.mu.RLock()
		idle := now.Sub(conn.lastUsed)
		conn.mu.RUnlock()

		if idle > p.config.IdleTimeout {
			p.logger.Printf("Closing idle connection for %s (idle: %v)", agentID, idle)
			conn.cancel()
			delete(p.connections, agentID)
		}
	}
}

// StartIdleCleanup starts periodic idle connection cleanup
func (p *ConnectionPool) StartIdleCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			p.CleanupIdle()
		}
	}()
}

// GetStatus returns status of all connections
func (p *ConnectionPool) GetStatus() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := make(map[string]string)
	for agentID, conn := range p.connections {
		conn.mu.RLock()
		status[agentID] = conn.state.String()
		conn.mu.RUnlock()
	}
	return status
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

// BridgeService implements the MCP stdio protocol with connection pooling
type BridgeService struct {
	pool   *ConnectionPool
	logger *log.Logger
}

// Run starts the stdio MCP server
func (b *BridgeService) Run() error {
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	b.logger.Printf("MCP Bridge Service starting (multi-agent mode)")

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

// handleRequest processes a single JSON-RPC request
func (b *BridgeService) handleRequest(data []byte) *JSONRPCResponse {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCError{
				Code:    -32700,
				Message: "Parse error",
				Data:    err.Error(),
			},
		}
	}

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
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

// handleInitialize handles the initialize request
func (b *BridgeService) handleInitialize(req JSONRPCRequest) *JSONRPCResponse {
	result := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "spacemolt-bridge-service",
			"version": "1.0.0",
		},
	}

	resultJSON, _ := json.Marshal(result)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// writeResponse writes a JSON-RPC response to stdout
func (b *BridgeService) writeResponse(w io.Writer, resp *JSONRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	_, err = w.Write(append(data, '\n'))
	return err
}

func main() {
	configPath := flag.String("config", "bridge-service.json", "Path to service configuration")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	// Load configuration
	configData, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	var rawConfig ServiceConfig
	if err := json.Unmarshal(configData, &rawConfig); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	// Parse into ParsedConfig with duration fields
	config := ParsedConfig{
		AgentsDir:         rawConfig.AgentsDir,
		GameServerURL:     rawConfig.GameServerURL,
		MaxConnections:    rawConfig.MaxConnections,
		ReconnectAttempts: rawConfig.ReconnectAttempts,
	}

	// Parse durations
	if rawConfig.IdleTimeout != "" {
		if d, err := time.ParseDuration(rawConfig.IdleTimeout); err == nil {
			config.IdleTimeout = d
		} else {
			log.Fatalf("Invalid idle_timeout: %v", err)
		}
	}
	if rawConfig.KeepAliveInterval != "" {
		if d, err := time.ParseDuration(rawConfig.KeepAliveInterval); err == nil {
			config.KeepAliveInterval = d
		} else {
			log.Fatalf("Invalid keep_alive_interval: %v", err)
		}
	}

	// Set defaults
	if config.GameServerURL == "" {
		config.GameServerURL = "wss://game.spacemolt.com/ws"
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 15 * time.Minute
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 100
	}
	if config.KeepAliveInterval == 0 {
		config.KeepAliveInterval = 30 * time.Second
	}
	if config.ReconnectAttempts == 0 {
		config.ReconnectAttempts = 5
	}

	// Create logger
	logger := log.New(os.Stderr, "[bridge] ", log.LstdFlags)
	if !*verbose {
		logger.SetOutput(io.Discard)
	}

	// Create connection pool
	pool := NewConnectionPool(&config, logger)

	// Start idle cleanup
	pool.StartIdleCleanup(1 * time.Minute)

	// Create and run service
	service := &BridgeService{
		pool:   pool,
		logger: logger,
	}

	if err := service.Run(); err != nil {
		log.Fatalf("Service error: %v", err)
	}
}
