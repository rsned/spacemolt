package game

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/internal/protocol"
)

const (
	// DefaultGameServerURL is the default WebSocket endpoint for the game
	DefaultGameServerURL = "wss://game.spacemolt.com/ws"

	// DefaultMCPServerURL is the default MCP endpoint for the game
	DefaultMCPServerURL = "https://game.spacemolt.com/mcp"

	// DefaultCatalogURL is the static game catalog download (no auth required).
	// It returns the entire catalog (ships, skills, recipes, items, modules,
	// facilities) as a single JSON document with a top-level version field.
	// Cached with an ETag (supports If-None-Match -> 304) and rate-limited to
	// 1/min per IP, so fetch it once per version and grep the local copy.
	DefaultCatalogURL = "https://game.spacemolt.com/api/catalog.json"
)

// Credentials holds agent authentication information loaded from credentials.json
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
}

// SimpleHandler is a basic message handler for agents
// It logs connection lifecycle events and message responses
type SimpleHandler struct {
	Client *Client
	Logger *log.Logger
}

// OnConnected logs successful connection events
func (h *SimpleHandler) OnConnected(state *State) {
	h.Logger.Printf("Connected successfully! Credits: %.2f", state.Credits)
}

// OnMessage logs informational and error messages from server
func (h *SimpleHandler) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.Logger.Printf("OK: %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.Logger.Printf("Error: %s", msg)
		}
	}
}

// OnDisconnected logs disconnection events
func (h *SimpleHandler) OnDisconnected(err error) {
	h.Logger.Printf("Disconnected: %v", err)
}

// LoadCredentials reads and parses credentials.json from an agent directory
// The file must exist at: data/agents/{agentID}/credentials.json
func LoadCredentials(agentDir string) (*Credentials, error) {
	data, err := os.ReadFile(filepath.Join(agentDir, "credentials.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials.json: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials.json: %w", err)
	}

	// Validate required fields
	if creds.Username == "" {
		return nil, fmt.Errorf("username is required in credentials.json")
	}
	if creds.Password == "" {
		return nil, fmt.Errorf("password is required in credentials.json")
	}
	if creds.Empire == "" {
		return nil, fmt.Errorf("empire is required in credentials.json")
	}

	return &creds, nil
}

// InitializeAgent creates and initializes a game client for an autonomous agent
// It handles the complete setup flow: load credentials, create client, set up handlers,
// connect to server, wait for ready, and authenticate.
//
// Parameters:
//   - agentID: The agent identifier (used for logger prefix and credential path)
//   - logger: A logger instance for agent-specific logging
//   - ctx: Context for lifecycle management
//   - debug: When true, enables verbose WebSocket debug logging; when false, debug output is suppressed
//
// Returns:
//   - *Client: Fully initialized and authenticated game client
//   - *Credentials: The loaded credentials (for reference if needed)
//   - error: Any error during initialization
//
// Example usage:
//
//	client, creds, err := game.InitializeAgent("miner-1", logger, ctx, false)
//	if err != nil {
//	    log.Fatalf("Failed to initialize agent: %v", err)
//	}
//	defer client.Close()
//
//	// Now use the client for autonomous operations...
func InitializeAgent(agentID string, logger *log.Logger, ctx context.Context, debug bool) (*Client, *Credentials, error) {
	// Step 1: Load credentials from agent directory
	agentDir := filepath.Join("data", "agents", agentID)
	creds, err := LoadCredentials(agentDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load credentials for %s: %w", agentID, err)
	}

	logger.Printf("Agent: %s | Empire: %s", creds.Username, creds.Empire)

	// Step 2: Create game client with dedicated game logger
	gameLogger := log.New(os.Stdout, fmt.Sprintf("[%s-GAME] ", agentID), log.LstdFlags)
	client := NewClient(DefaultGameServerURL, creds.Username, creds.Password, gameLogger)
	client.SetDebugLogging(debug)

	// Step 3: Set up message handler with automatic reconnection
	handler := &SimpleHandler{
		Client: client,
		Logger: logger,
	}
	reconnectingHandler := NewReconnectingHandler(client, handler, ctx, logger)
	// Coordinate authentication across every agent on this host/IP. The server
	// budgets auth per IP, so a mass disconnect (a game-server restart) and a
	// mass start (a fleet redeploy) both stampede the same allowance. One gate
	// instance serves both paths: the handler uses it for reconnects, the
	// client for its internal retries, and the initial dial below.
	gate := NewReconnectGate(DefaultReconnectGatePath(), reconnectGateCooldown)
	reconnectingHandler.SetReconnectGate(gate)
	client.SetReconnectGate(gate)
	client.SetHandler(reconnectingHandler)

	// Steps 4-6: Connect, wait for ready, and authenticate -- as one gated
	// attempt, so a fresh start is spaced against every other client on the
	// host instead of racing them into a per-IP block.
	if err := dialWithGate(ctx, gate,
		func(ctx context.Context) error {
			logger.Printf("Connecting to game server...")
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("failed to connect to game server: %w", err)
			}
			logger.Printf("Waiting for connection ready...")
			select {
			case <-client.Ready():
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		func(ctx context.Context) error {
			logger.Printf("Logging in...")
			if err := client.Login(ctx); err != nil {
				return fmt.Errorf("failed to login: %w", err)
			}
			return nil
		},
	); err != nil {
		return nil, nil, err
	}

	// Step 7: Get initial state to confirm successful login
	state := client.GetState()
	if state.Player.ID == "" {
		return nil, nil, fmt.Errorf("login appeared to succeed but player data is missing")
	}

	logger.Printf("Ready! Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	return client, creds, nil
}

// InitializeMCPAgent creates and initializes an MCP game client for an autonomous agent.
// It uses direct HTTP to the MCP endpoint instead of WebSocket.
//
// Parameters:
//   - agentID: The agent identifier (used for logger prefix and credential path)
//   - logger: A logger instance for agent-specific logging
//   - ctx: Context for lifecycle management
//
// Returns:
//   - GameClient: Fully initialized and authenticated MCP game client
//   - *Credentials: The loaded credentials (for reference if needed)
//   - error: Any error during initialization
func InitializeMCPAgent(agentID string, logger *log.Logger, ctx context.Context, debug bool, disablePolling bool) (GameClient, *Credentials, error) {
	// Step 1: Load credentials from agent directory
	agentDir := filepath.Join("data", "agents", agentID)
	creds, err := LoadCredentials(agentDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load credentials for %s: %w", agentID, err)
	}

	logger.Printf("Agent: %s | Empire: %s | Transport: MCP", creds.Username, creds.Empire)

	// Step 2: Create MCP game client
	gameLogger := log.New(os.Stdout, fmt.Sprintf("[%s-MCP] ", agentID), log.LstdFlags)
	client := NewMCPGameClient(DefaultMCPServerURL, creds.Username, creds.Password, gameLogger)
	if debug {
		client.SetDebugLogging(true)
	}
	if disablePolling {
		client.SetPolling(false)
	}

	// Steps 3-4: Connect (MCP initialize handshake) and authenticate, as one
	// gated attempt. The MCP transport reaches the same accounts over the same
	// outbound IP, so it draws on the same per-IP auth budget as the WS path
	// and must share the host-wide gate.
	gate := NewReconnectGate(DefaultReconnectGatePath(), reconnectGateCooldown)
	if err := dialWithGate(ctx, gate,
		func(ctx context.Context) error {
			logger.Printf("Connecting to MCP server...")
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("failed to connect to MCP server: %w", err)
			}
			return nil
		},
		func(ctx context.Context) error {
			logger.Printf("Logging in via MCP...")
			if err := client.Login(ctx); err != nil {
				_ = client.Close()
				return fmt.Errorf("failed to login via MCP: %w", err)
			}
			return nil
		},
	); err != nil {
		return nil, nil, err
	}

	// Step 5: Fetch initial state (login response may not include full player data)
	logger.Printf("Fetching initial state...")
	if err := client.GetStatus(ctx); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("failed to get initial status: %w", err)
	}

	// Step 6: Verify state
	state := client.GetState()
	if state.Player.ID == "" {
		_ = client.Close()
		return nil, nil, fmt.Errorf("login appeared to succeed but player data is missing")
	}

	logger.Printf("Ready! Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	return client, creds, nil
}
