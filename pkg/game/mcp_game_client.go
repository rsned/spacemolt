package game

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// MCPGameClient implements the GameClient interface using direct HTTP calls
// to the game server's MCP endpoint (Streamable HTTP transport).
type MCPGameClient struct {
	serverURL string // MCP endpoint (e.g., https://game.spacemolt.com/mcp)
	username  string
	password  string

	// Game session (from login response).
	sessionID string

	// MCP protocol session (from Mcp-Session-Id response header).
	mcpSessionID string

	state *State
	mu    sync.RWMutex

	httpClient *http.Client
	logger     *log.Logger

	handler   MessageHandler
	handlerMu sync.RWMutex

	pollCtx    context.Context
	pollCancel context.CancelFunc
	pollWg     sync.WaitGroup

	connected   bool
	connectedMu sync.RWMutex

	debug bool

	latestListings []MarketListing
	listingsMu     sync.RWMutex

	readyChan chan struct{}
	readyOnce sync.Once

	requestID atomic.Int64
}

// NewMCPGameClient creates a new MCP game client.
func NewMCPGameClient(serverURL, username, password string, logger *log.Logger) *MCPGameClient {
	if logger == nil {
		logger = log.New(io.Discard, "", log.LstdFlags)
	}

	return &MCPGameClient{
		serverURL: serverURL,
		username:  username,
		password:  password,
		state: &State{
			Username: username,
			Password: password,
		},
		httpClient: &http.Client{
			Timeout: 90 * time.Second, // Generous timeout for blocking actions
		},
		logger:    logger,
		readyChan: make(chan struct{}),
	}
}

// SetDebugLogging enables or disables debug logging for raw responses.
func (m *MCPGameClient) SetDebugLogging(enabled bool) {
	m.debug = enabled
}

// SetHandler sets the message handler for lifecycle events.
// Only OnConnected and OnDisconnected are called; OnMessage is not used.
func (m *MCPGameClient) SetHandler(handler MessageHandler) {
	m.handlerMu.Lock()
	defer m.handlerMu.Unlock()
	m.handler = handler
}

// Connect performs the MCP initialize handshake and starts the background poller.
func (m *MCPGameClient) Connect(ctx context.Context) error {
	if err := m.initialize(ctx); err != nil {
		return fmt.Errorf("MCP initialize: %w", err)
	}

	m.pollCtx, m.pollCancel = context.WithCancel(ctx)

	m.setConnected(true)
	m.logger.Printf("[MCP] Connected to %s", m.serverURL)

	return nil
}

// Close stops the background poller and cleans up.
func (m *MCPGameClient) Close() error {
	if m.pollCancel != nil {
		m.pollCancel()
	}
	m.pollWg.Wait()
	m.setConnected(false)

	m.handlerMu.RLock()
	h := m.handler
	m.handlerMu.RUnlock()
	if h != nil {
		h.OnDisconnected(nil)
	}

	m.logger.Printf("[MCP] Disconnected from %s", m.serverURL)
	return nil
}

// IsConnected returns whether the client has an active session.
func (m *MCPGameClient) IsConnected() bool {
	m.connectedMu.RLock()
	defer m.connectedMu.RUnlock()
	return m.connected
}

// Ready returns a channel that is closed when the client is ready (after login).
func (m *MCPGameClient) Ready() <-chan struct{} {
	return m.readyChan
}

// GetState returns a deep copy of the current game state.
func (m *MCPGameClient) GetState() *State {
	m.mu.RLock()
	s := m.state
	m.mu.RUnlock()
	return s.Clone()
}

// Login authenticates with the game server via the MCP login tool.
func (m *MCPGameClient) Login(ctx context.Context) error {
	result, err := m.callTool(ctx, "login", map[string]any{
		"username": m.username,
		"password": m.password,
	})
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// Parse the login response to extract session and state.
	if err := m.parseLoginResult(result); err != nil {
		return fmt.Errorf("parsing login result: %w", err)
	}

	// Start background poller now that we have a session.
	m.startPoller()

	// Signal ready.
	m.readyOnce.Do(func() { close(m.readyChan) })

	// Notify handler.
	m.handlerMu.RLock()
	h := m.handler
	m.handlerMu.RUnlock()
	if h != nil {
		h.OnConnected(m.GetState())
	}

	m.logger.Printf("[MCP] Logged in as %s", m.username)
	return nil
}

// Register creates a new account and logs in.
func (m *MCPGameClient) Register(ctx context.Context, empire, registrationCode string) error {
	result, err := m.callTool(ctx, "register", map[string]any{
		"username":          m.username,
		"empire":            empire,
		"registration_code": registrationCode,
	})
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// The register response includes the password.
	var regResp struct {
		Password string `json:"password"`
	}
	if err := parseToolResultJSON(result, &regResp); err != nil {
		return fmt.Errorf("parsing register result: %w", err)
	}

	if regResp.Password != "" {
		m.password = regResp.Password
		m.mu.Lock()
		m.state.Password = regResp.Password
		m.mu.Unlock()
	}

	// Login after registration.
	return m.Login(ctx)
}

// --- Internal methods ---

// initialize performs the MCP protocol handshake.
func (m *MCPGameClient) initialize(ctx context.Context) error {
	body, err := buildJSONRPCRequest(m.nextID(), "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    ClientName,
			"version": VersionID,
		},
	})
	if err != nil {
		return fmt.Errorf("building initialize request: %w", err)
	}

	resp, mcpSession, err := m.doHTTPRequest(ctx, body)
	if err != nil {
		return fmt.Errorf("initialize HTTP request: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	if mcpSession != "" {
		m.mcpSessionID = mcpSession
	}

	m.logger.Printf("[MCP] Initialized (protocol session: %s)", m.mcpSessionID)

	// Send initialized notification.
	notifBody, err := json.Marshal(mcpJSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})
	if err != nil {
		return fmt.Errorf("building initialized notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.serverURL, bytes.NewReader(notifBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.mcpSessionID != "" {
		req.Header.Set("Mcp-Session-Id", m.mcpSessionID)
	}
	httpResp, err := m.httpClient.Do(req)
	if err != nil {
		// Notification failure is non-fatal.
		m.logger.Printf("[MCP] Warning: initialized notification failed: %v", err)
		return nil
	}
	_, _ = io.Copy(io.Discard, httpResp.Body)
	_ = httpResp.Body.Close()

	return nil
}

// callTool invokes an MCP tool and returns the raw result.
// On session_invalid errors, it automatically re-logs in and retries once.
func (m *MCPGameClient) callTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	result, err := m.callToolOnce(ctx, toolName, args)
	if err == nil {
		return result, nil
	}

	// Check for session_invalid to auto-retry.
	if !isSessionInvalidError(err) {
		return nil, err
	}

	m.logger.Printf("[MCP] Session invalid, re-authenticating...")
	if loginErr := m.relogin(ctx); loginErr != nil {
		return nil, fmt.Errorf("re-login failed: %w (original error: %w)", loginErr, err)
	}

	// Retry the original call once.
	return m.callToolOnce(ctx, toolName, args)
}

// callToolOnce performs a single tool call without retry.
func (m *MCPGameClient) callToolOnce(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	// Inject session_id for authenticated calls.
	if m.sessionID != "" && toolName != "login" && toolName != "register" {
		if args == nil {
			args = make(map[string]any)
		}
		args["session_id"] = m.sessionID
	}

	body, err := buildJSONRPCRequest(m.nextID(), "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	})
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", toolName, err)
	}

	resp, mcpSession, err := m.doHTTPRequest(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("HTTP request for %s: %w", toolName, err)
	}

	// Update MCP session if server sends a new one.
	if mcpSession != "" {
		m.mcpSessionID = mcpSession
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error for %s (code %d): %s", toolName, resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

// doHTTPRequest sends a JSON-RPC request and parses the response.
// Returns the parsed response and the Mcp-Session-Id header value.
func (m *MCPGameClient) doHTTPRequest(ctx context.Context, body []byte) (*mcpJSONRPCResponse, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.serverURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("creating HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	req.Header.Set("User-Agent", UserAgent)
	if m.mcpSessionID != "" {
		req.Header.Set("Mcp-Session-Id", m.mcpSessionID)
	}

	httpResp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close() //nolint:errcheck

	if httpResp.StatusCode == http.StatusTooManyRequests {
		respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024))
		// Parse retry_after from JSON response if available.
		var rateLimitResp struct {
			RetryAfter int `json:"retry_after"`
		}
		if err := json.Unmarshal(respBody, &rateLimitResp); err == nil && rateLimitResp.RetryAfter > 0 {
			wait := time.Duration(rateLimitResp.RetryAfter) * time.Second
			m.logger.Printf("[MCP] Rate limited, waiting %v...", wait)
			time.Sleep(wait)
		} else {
			m.logger.Printf("[MCP] Rate limited, waiting 30s...")
			time.Sleep(SleepReconnect)
		}
		return nil, "", fmt.Errorf("HTTP 429: %s", string(respBody))
	}

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024))
		return nil, "", fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, string(respBody))
	}

	mcpSession := httpResp.Header.Get("Mcp-Session-Id")
	contentType := httpResp.Header.Get("Content-Type")

	// Read body so we can log it on error.
	bodyData, err := io.ReadAll(io.LimitReader(httpResp.Body, 10*1024*1024))
	if err != nil {
		return nil, mcpSession, fmt.Errorf("reading response body: %w", err)
	}

	resp, err := detectAndParseResponse(contentType, bytes.NewReader(bodyData))
	if err != nil {
		m.logger.Printf("[MCP] Parse error. Content-Type: %s, Body: %s", contentType, truncate(string(bodyData), 500))
		return nil, mcpSession, fmt.Errorf("parsing response: %w", err)
	}

	return resp, mcpSession, nil
}

// relogin performs a fresh login to get a new game session.
// This does not call callTool to avoid infinite recursion.
func (m *MCPGameClient) relogin(ctx context.Context) error {
	args := map[string]any{
		"username": m.username,
		"password": m.password,
	}

	body, err := buildJSONRPCRequest(m.nextID(), "tools/call", map[string]any{
		"name":      "login",
		"arguments": args,
	})
	if err != nil {
		return err
	}

	resp, mcpSession, err := m.doHTTPRequest(ctx, body)
	if err != nil {
		return err
	}
	if mcpSession != "" {
		m.mcpSessionID = mcpSession
	}
	if resp.Error != nil {
		return fmt.Errorf("login error: %s", resp.Error.Message)
	}

	return m.parseLoginResult(resp.Result)
}

// parseLoginResult extracts session and state from a login tool result.
func (m *MCPGameClient) parseLoginResult(result json.RawMessage) error {
	text, err := parseToolResultText(result)
	if err != nil {
		return err
	}

	// The login tool returns JSON with session info and player state.
	var loginResp struct {
		SessionID string `json:"session_id"`
		// Player state fields that may be present.
		Player json.RawMessage `json:"player,omitempty"`
		Ship   json.RawMessage `json:"ship,omitempty"`
		System json.RawMessage `json:"system,omitempty"`
		POI    json.RawMessage `json:"poi,omitempty"`
	}

	if err := json.Unmarshal([]byte(text), &loginResp); err != nil {
		// If the text isn't structured JSON, try extracting session_id from it.
		m.logger.Printf("[MCP] Login result not structured JSON, raw: %s", truncate(text, 200))
		return fmt.Errorf("parsing login response: %w", err)
	}

	if loginResp.SessionID != "" {
		m.sessionID = loginResp.SessionID
		m.logger.Printf("[MCP] Game session: %s", truncate(m.sessionID, 16))
	}

	// Parse state from login response if available.
	m.mu.Lock()
	defer m.mu.Unlock()

	if loginResp.Player != nil {
		var player Player
		if err := json.Unmarshal(loginResp.Player, &player); err == nil {
			m.state.Player = player
			m.state.Credits = player.Credits
			m.state.CurrentSystem = player.CurrentSystem
			m.state.CurrentPOI = player.CurrentPOI
			m.state.Doc = player.DockedAtBase != ""
		}
	}

	if loginResp.Ship != nil {
		var ship Ship
		if err := json.Unmarshal(loginResp.Ship, &ship); err == nil {
			m.state.Ship = ship
			m.state.Fuel = ship.Fuel
			m.state.MaxFuel = ship.MaxFuel
			m.state.Hull = ship.Hull
			m.state.MaxHull = ship.MaxHull
		}
	}

	if loginResp.System != nil {
		var system SystemData
		if err := json.Unmarshal(loginResp.System, &system); err == nil {
			m.state.System = system
		}
	}

	return nil
}

// startPoller starts the background state polling goroutine.
func (m *MCPGameClient) startPoller() {
	m.pollWg.Add(1)
	go func() {
		defer m.pollWg.Done()
		m.pollLoop()
	}()
}

// pollLoop periodically calls get_status to keep state fresh.
func (m *MCPGameClient) pollLoop() {
	ticker := time.NewTicker(SleepTick)
	defer ticker.Stop()

	for {
		select {
		case <-m.pollCtx.Done():
			return
		case <-ticker.C:
			if err := m.pollStatus(); err != nil {
				m.logger.Printf("[MCP] Poll error: %v", err)
			}
		}
	}
}

// pollStatus fetches current status and updates internal state.
func (m *MCPGameClient) pollStatus() error {
	ctx, cancel := context.WithTimeout(m.pollCtx, 30*time.Second)
	defer cancel()

	result, err := m.callTool(ctx, "get_status", nil)
	if err != nil {
		return err
	}

	return m.updateStateFromResult(result)
}

// updateStateFromResult parses an MCP tool result's text content and
// updates the internal state. The text is expected to be JSON matching
// the server's response payload structure.
func (m *MCPGameClient) updateStateFromResult(result json.RawMessage) error {
	text, err := parseToolResultText(result)
	if err != nil {
		return err
	}

	// Try to parse as a structured response with player/ship/system fields.
	var payload struct {
		Player      json.RawMessage `json:"player,omitempty"`
		Ship        json.RawMessage `json:"ship,omitempty"`
		System      json.RawMessage `json:"system,omitempty"`
		POI         json.RawMessage `json:"poi,omitempty"`
		Modules     json.RawMessage `json:"modules,omitempty"`
		Nearby      json.RawMessage `json:"nearby,omitempty"`
		CurrentTick *int64          `json:"current_tick,omitempty"`
		Listings    json.RawMessage `json:"listings,omitempty"`
		Docked      *bool           `json:"docked,omitempty"`
		Message     string          `json:"message,omitempty"`
	}

	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		// Non-JSON text responses (e.g., "Mining complete") are fine.
		if m.debug {
			m.logger.Printf("[MCP DEBUG] updateStateFromResult: not JSON, skipping: %v", err)
		}
		return nil
	}

	// If the response is wrapped in {"result": {...}, "session": {...}} (HTTP API format),
	// unwrap the "result" field and re-parse.
	if payload.Player == nil && payload.Ship == nil && payload.System == nil && payload.Message == "" {
		var wrapper struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal([]byte(text), &wrapper); err == nil && len(wrapper.Result) > 0 {
			if m.debug {
				m.logger.Printf("[MCP DEBUG] unwrapping result envelope: %s", truncate(string(wrapper.Result), 500))
			}
			if err := json.Unmarshal(wrapper.Result, &payload); err != nil {
				if m.debug {
					m.logger.Printf("[MCP DEBUG] failed to parse unwrapped result: %v", err)
				}
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if payload.Player != nil {
		player, parseErr := parseMCPPlayer(payload.Player)
		if parseErr != nil {
			m.logger.Printf("[MCP] Player parse failed: %v", parseErr)
		} else {
			m.state.Player = player
			m.state.Credits = player.Credits
			m.state.CurrentSystem = player.CurrentSystem
			m.state.CurrentPOI = player.CurrentPOI
			m.state.Doc = player.DockedAtBase != ""
			if player.Skills != nil {
				m.state.SkillXP = player.SkillXP
			}
		}
	}

	if payload.Ship != nil {
		var ship Ship
		if err := json.Unmarshal(payload.Ship, &ship); err == nil {
			m.state.Ship = ship
			m.state.Fuel = ship.Fuel
			m.state.MaxFuel = ship.MaxFuel
			m.state.Hull = ship.Hull
			m.state.MaxHull = ship.MaxHull
			m.state.MaxCargo = int(ship.CargoCapacity)
		}
	}

	if payload.System != nil {
		var system SystemData
		if err := json.Unmarshal(payload.System, &system); err == nil {
			m.state.System = system
		}
	}

	if payload.Nearby != nil {
		var nearby []NearbyPlayer
		if err := json.Unmarshal(payload.Nearby, &nearby); err == nil {
			m.state.Nearby = nearby
		}
	}

	if payload.CurrentTick != nil {
		m.state.CurrentTick = *payload.CurrentTick
	}

	if payload.Docked != nil {
		m.state.Doc = *payload.Docked
	}

	return nil
}

// parseMCPPlayer parses a player JSON blob, handling both the standard
// skills format (map[string]Skill with {level, xp}) and the MCP format
// (map[string]int with just level numbers).
func parseMCPPlayer(data json.RawMessage) (Player, error) {
	var player Player
	if err := json.Unmarshal(data, &player); err == nil {
		return player, nil
	}

	// Skills format mismatch — parse everything except skills, then fix skills.
	// First, unmarshal into a generic map to extract and rewrite the skills field.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Player{}, fmt.Errorf("player JSON parse: %w", err)
	}

	skillsRaw, hasSkills := raw["skills"]
	if hasSkills {
		// Try as map[string]int (MCP format: {"mining": 10, ...}).
		var skillLevels map[string]int
		if err := json.Unmarshal(skillsRaw, &skillLevels); err == nil {
			// Convert to map[string]Skill format.
			converted := make(map[string]Skill, len(skillLevels))
			for name, level := range skillLevels {
				converted[name] = Skill{Level: level}
			}
			// Re-serialize in the format Player expects.
			fixedSkills, _ := json.Marshal(converted)
			raw["skills"] = fixedSkills
		}
	}

	fixedData, err := json.Marshal(raw)
	if err != nil {
		return Player{}, fmt.Errorf("re-marshaling player: %w", err)
	}

	if err := json.Unmarshal(fixedData, &player); err != nil {
		return Player{}, fmt.Errorf("player parse after skills fix: %w", err)
	}
	return player, nil
}

// setConnected updates the connected state.
func (m *MCPGameClient) setConnected(c bool) {
	m.connectedMu.Lock()
	defer m.connectedMu.Unlock()
	m.connected = c
}

// nextID returns the next JSON-RPC request ID.
func (m *MCPGameClient) nextID() int64 {
	return m.requestID.Add(1)
}

// isSessionInvalidError checks if an error indicates an expired/invalid session.
func isSessionInvalidError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, pattern := range []string{
		"session_invalid",
		"session_expired",
		"invalid session",
		"not authenticated",
		"unauthorized",
	} {
		if containsInsensitive(msg, pattern) {
			return true
		}
	}
	return false
}

// containsInsensitive checks if s contains substr (case-insensitive).
func containsInsensitive(s, substr string) bool {
	sLower := make([]byte, len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		sLower[i] = c
	}
	subLower := make([]byte, len(substr))
	for i := range len(substr) {
		c := substr[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		subLower[i] = c
	}
	return bytes.Contains(sLower, subLower)
}

// Ensure MCPGameClient implements GameClient.
var _ GameClient = (*MCPGameClient)(nil)
