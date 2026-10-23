package game

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/clerk"
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

// clerkBaseURL overrides the Clerk API origin for tests; empty in production.
var clerkBaseURL string

// resolveTokenMinter decides how this process authenticates and logs the
// decision once: "auth=clerk player_id=<id>" or "auth=password reason=<r>".
// It returns nil (password logins) when no Clerk key is configured, when the
// player list cannot be read, or when the key's account does not own
// creds.Username. With creds.PlayerID set it never lists players.
func resolveTokenMinter(creds *Credentials, logger *log.Logger) TokenMinter {
	key, err := clerk.LoadKey()
	if err != nil {
		if !errors.Is(err, clerk.ErrNoKey) {
			// LoadKey's errors never include the key itself.
			logger.Printf("clerk key unreadable: %v", err)
		}
		logger.Printf("auth=password reason=no_key")
		return nil
	}

	var opts []clerk.Option
	if clerkBaseURL != "" {
		opts = append(opts, clerk.WithBaseURL(clerkBaseURL))
	}
	src := clerk.NewSource(key, opts...)

	id := creds.PlayerID
	if id == "" {
		// The Source's HTTP client bounds this call (30s).
		players, err := src.ListPlayers(context.Background())
		if err != nil {
			logger.Printf("clerk list players: %v", err)
			logger.Printf("auth=password reason=list_failed")
			return nil
		}
		for _, p := range players {
			if p.Username == creds.Username {
				id = p.ID
				break
			}
		}
		if id == "" {
			logger.Printf("auth=password reason=not_owned")
			return nil
		}
		logger.Printf("player_id missing from credentials.json; run clerk-sync")
	}

	logger.Printf("auth=clerk player_id=%s", id)
	return func(ctx context.Context) (string, error) {
		t, err := src.MintWSToken(ctx, id)
		return t.Value, err
	}
}

// Credentials holds agent authentication information loaded from credentials.json
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
	// PlayerID is the Clerk-owned player id, written by cmd/tools/clerk-sync.
	// It lets a worker mint a ws-token without listing every player first.
	// Optional: empty means not yet synced.
	PlayerID string `json:"player_id,omitempty"`
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
	gate := NewReconnectGate(DefaultReconnectGatePath(), DefaultGateConfig())
	reconnectingHandler.SetReconnectGate(gate)
	client.SetReconnectGate(gate)
	client.SetHandler(reconnectingHandler)

	// Clerk ws-token auth when this host has a key and owns the player; else
	// passwords. The client keeps the minter so Reconnect mints per try too.
	minter := resolveTokenMinter(creds, logger)
	client.SetTokenMinter(minter)

	// Steps 4-6: Connect, wait for ready, and authenticate -- in up to
	// dialAttempts gated tries, so a fresh start is spaced against every other
	// client on the host instead of racing them into a per-IP block. The gate
	// is the only retry mechanism; Connect itself dials once.
	//
	// Hold the handler's reconnect guard while dialing: tearing down a failed
	// try's socket fires OnDisconnected, which must not start a reconnect
	// burst racing this loop for the same gate slots.
	reconnectingHandler.reconnecting.Store(true)
	tries := 0
	dialErr := dialWithGate(ctx, gate, dialAttempts, authAttempt{
		mint: minter,
		connect: func(ctx context.Context) error {
			if tries++; tries > 1 {
				client.resetForDial()
			}
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
		login: func(ctx context.Context, token string) error {
			logger.Printf("Logging in...")
			var err error
			if token != "" {
				err = client.LoginWithToken(ctx, token)
			} else {
				err = client.Login(ctx)
			}
			if err != nil {
				return fmt.Errorf("failed to login: %w", err)
			}
			return nil
		},
		logger:       logger,
		fallbackOnce: &client.mintFallbackOnce,
	})
	if dialErr != nil {
		// The caller never sees this client; close it while the guard is held
		// so its listener cannot start a reconnect after we return.
		_ = client.Disconnect()
	}
	reconnectingHandler.reconnecting.Store(false)
	if dialErr != nil {
		return nil, nil, dialErr
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
	gate := NewReconnectGate(DefaultReconnectGatePath(), DefaultGateConfig())
	// Password-only, single attempt: the MCP transport has no token login.
	if err := dialWithGate(ctx, gate, 1, authAttempt{
		connect: func(ctx context.Context) error {
			logger.Printf("Connecting to MCP server...")
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("failed to connect to MCP server: %w", err)
			}
			return nil
		},
		login: func(ctx context.Context, _ string) error {
			logger.Printf("Logging in via MCP...")
			if err := client.Login(ctx); err != nil {
				_ = client.Close()
				return fmt.Errorf("failed to login via MCP: %w", err)
			}
			return nil
		},
	}); err != nil {
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
