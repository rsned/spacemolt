package game

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
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

	debug          bool
	disablePolling bool // If true, don't poll get_status every 10s

	latestListings []MarketListing
	listingsMu     sync.RWMutex

	latestRawJSON map[string][]byte

	rawJSONMu sync.RWMutex

	readyChan chan struct{}
	readyOnce sync.Once

	requestID atomic.Int64

	// XP observation tracking — fires whenever skill XP changes in state.
	// Mirrors the fields on the WS *Client; see SetXPCallback and checkXPChanges.
	//
	// Because MCP server responses do not include skill_xp in player blocks
	// (only flat skill levels), we refresh XP after every successful mutation
	// by making a follow-up get_skills call — its response carries
	// player_skills[].current_xp which we merge into state.Player.Skills and
	// state.SkillXP. The xpBaselineReady flag is false until the first such
	// refresh completes, so the initial good-XP snapshot seeds the baseline
	// without emitting a spurious 0->N delta.
	xpCallback      XPCallbackFunc
	xpLastSkills    map[string]Skill   // last known skill state
	xpLastXP        map[string]float64 // last known SkillXP
	xpLastAction    string             // most recent action sent
	xpLastTarget    string             // most recent action target
	xpLastQuantity  int                // most recent action quantity (default 1)
	xpBaselineReady bool               // true once first get_skills refresh has seeded baseline
	xpMu            sync.Mutex
}

// SetXPCallback installs an XP observation callback. Passing nil disables
// callbacks. The callback fires after every successful mutation tool call
// once state has been refreshed from get_skills (which provides the XP
// values MCP mutation responses omit).
func (m *MCPGameClient) SetXPCallback(fn XPCallbackFunc) {
	m.xpMu.Lock()
	m.xpCallback = fn
	// Reset baseline — the next refresh seeds snapshot without firing.
	m.xpBaselineReady = false
	m.xpLastSkills = nil
	m.xpLastXP = nil
	m.xpMu.Unlock()
}

// isQueryTool returns true for read-only MCP tools that should not update
// XP attribution or trigger an XP refresh. Mirrors isActionCommand in
// pkg/agent/runner.go — keep them in sync when the server adds tools.
func isQueryTool(name string) bool {
	switch name {
	case "get_status", "get_notifications", "get_ship", "get_skills",
		"get_cargo", "get_system", "get_poi", "get_base", "get_map",
		"get_version", "get_base_cost", "get_listings", "get_trades",
		"get_wrecks", "get_base_wrecks", "get_recipes", "get_notes",
		"get_nearby", "get_missions", "get_active_missions",
		"view_market", "view_orders", "view_storage", "browse_ships",
		"catalog", "catalog_items", "catalog_ships", "catalog_recipes",
		"faction_list", "faction_info", "forum_list", "forum_get_thread",
		"raid_status", "chat_history", "get_chat_history",
		"help", "register", "login", "logout", "wait":
		return true
	}
	return false
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
		logger:        logger,
		readyChan:     make(chan struct{}),
		latestRawJSON: make(map[string][]byte),
	}
}

// SetOnChatMessage is a no-op for the MCP client since it does not receive
// push events over WebSocket.
func (m *MCPGameClient) SetOnChatMessage(fn func(msg serverapi.ChatMessage)) {}

// SetDebugLogging enables or disables debug logging for raw responses.
func (m *MCPGameClient) SetDebugLogging(enabled bool) {
	m.debug = enabled
}

// SetPolling enables or disables automatic get_status polling every 10 seconds.
// By default, polling is enabled to keep state fresh. Disable if you want to
// manually control when to refresh state.
func (m *MCPGameClient) SetPolling(enabled bool) {
	m.disablePolling = !enabled
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

	// Fetch initial tick via get_notifications (lightweight).
	if err := m.pollNotifications(); err != nil {
		m.logger.Printf("[MCP] Warning: initial get_notifications failed: %v", err)
	} else {
		m.logger.Printf("[MCP] Initial tick: %d", m.state.CurrentTick)
	}

	// Start background poller now that we have a session (unless disabled).
	if !m.disablePolling {
		m.startPoller()
	}

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
	// Track the latest action for XP observation attribution. Only update
	// for mutation tools — query tools (get_*, view_*, etc.) would otherwise
	// clobber the attribution pinned by a preceding mutation. This lets
	// internal get_skills refreshes preserve the original command's credit.
	isMutation := !isQueryTool(toolName)
	if isMutation {
		m.xpMu.Lock()
		if m.xpCallback != nil {
			m.xpLastAction = toolName
			m.xpLastTarget = extractTargetFromArgs(args)
			m.xpLastQuantity = extractQuantityFromArgs(args)
		}
		m.xpMu.Unlock()
	}

	result, err := m.callToolOnce(ctx, toolName, args)
	// Always cache the result (including tool-level errors) for interactive display.
	m.cacheLastResult(result)

	// Handle session_invalid auto-retry before the XP refresh hook.
	if err != nil && isSessionInvalidError(err) {
		m.logger.Printf("[MCP] Session invalid, re-authenticating...")
		if loginErr := m.relogin(ctx); loginErr != nil {
			return nil, fmt.Errorf("re-login failed: %w (original error: %w)", loginErr, err)
		}
		result, err = m.callToolOnce(ctx, toolName, args)
		m.cacheLastResult(result)
	}

	// After a successful mutation with an XP callback installed, refresh
	// skill XP via get_skills — MCP mutation responses don't include
	// skill_xp, so without this refresh the XP tracker would only see
	// level-up events, missing all within-level deltas.
	if err == nil && isMutation {
		m.xpMu.Lock()
		hasCallback := m.xpCallback != nil
		m.xpMu.Unlock()
		if hasCallback {
			m.refreshXPFromSkills(ctx)
		}
	}

	return result, err
}

// refreshXPFromSkills fetches the player_skills list via an internal
// get_skills call and merges the current_xp values into state. On the
// first call (baseline not ready) it only seeds xpLastSkills/xpLastXP
// without firing the callback. On subsequent calls it updates the
// skill state then runs checkXPChanges with the original attribution
// from xpLastAction (the mutation that just completed).
//
// Uses callToolOnce directly so the attribution-update hook in callTool
// does not overwrite xpLastAction. Silent on errors — XP tracking is
// best-effort and should never break the caller's command flow.
func (m *MCPGameClient) refreshXPFromSkills(ctx context.Context) {
	result, err := m.callToolOnce(ctx, "get_skills", nil)
	if err != nil || len(result) == 0 {
		return
	}
	text, terr := parseToolResultText(result)
	if terr != nil {
		return
	}

	// Parse the skills map out of the response. The MCP get_skills response
	// shape (per server_docs/openapi.json GetSkillsResponse) is:
	//   {"skills": {"mining": {"level": 11, "xp": 1234, "next_level_xp": 2000, ...}, ...}}
	// i.e., a map keyed by skill id with per-skill objects. The game server
	// wraps responses in several possible envelopes (direct, {"result": ...},
	// SSE content blocks) — try direct first, then unwrap.
	type skillRow struct {
		Level int     `json:"level"`
		XP    float64 `json:"xp"`
	}
	type skillsResp struct {
		Skills map[string]skillRow `json:"skills"`
	}
	var resp skillsResp
	if err := json.Unmarshal([]byte(text), &resp); err != nil || len(resp.Skills) == 0 {
		// Try SSE content unwrap.
		var sseWrap struct {
			Result struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if jerr := json.Unmarshal([]byte(text), &sseWrap); jerr == nil {
			for _, c := range sseWrap.Result.Content {
				if c.Type == "text" && c.Text != "" {
					if jerr2 := json.Unmarshal([]byte(c.Text), &resp); jerr2 == nil && len(resp.Skills) > 0 {
						break
					}
				}
			}
		}
	}
	if len(resp.Skills) == 0 {
		return
	}

	// Merge into state under m.mu.
	m.mu.Lock()
	newXP := make(map[string]float64, len(resp.Skills))
	if m.state.Player.Skills == nil {
		m.state.Player.Skills = make(map[string]Skill, len(resp.Skills))
	}
	for skillID, row := range resp.Skills {
		newXP[skillID] = row.XP
		m.state.Player.Skills[skillID] = Skill(row)
	}
	m.state.SkillXP = newXP

	// Seed baseline on first call; fire checkXPChanges thereafter.
	m.xpMu.Lock()
	ready := m.xpBaselineReady
	m.xpBaselineReady = true
	m.xpMu.Unlock()

	if !ready {
		m.xpMu.Lock()
		m.xpLastSkills = copySkillMap(m.state.Player.Skills)
		m.xpLastXP = copyStringFloatMap(m.state.SkillXP)
		m.xpMu.Unlock()
		m.mu.Unlock()
		return
	}

	m.checkXPChanges() // requires m.mu held
	m.mu.Unlock()
}

// cacheLastResult extracts text from an MCP tool result and stores it as _last raw JSON.
// This caches both successful responses and tool-level errors (isError: true).
func (m *MCPGameClient) cacheLastResult(result json.RawMessage) {
	if len(result) == 0 {
		return
	}
	// parseToolResultText returns an error for isError responses,
	// but we still want to cache the text content for display.
	text, err := parseToolResultText(result)
	if err != nil {
		// Extract text content even from error responses.
		text = extractToolResultText(result)
	}
	if text == "" {
		return
	}
	m.rawJSONMu.Lock()
	m.latestRawJSON["_last"] = []byte(text)
	m.rawJSONMu.Unlock()
}

// extractToolResultText extracts all text content blocks from an MCP tool result,
// regardless of isError status.
func extractToolResultText(result json.RawMessage) string {
	var toolResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return ""
	}
	var texts []string
	for _, c := range toolResult.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "")
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

	// DEBUG: Log the request being sent
	if m.debug {
		m.logger.Printf("=== MCP Client Send Debug ===")
		m.logger.Printf("Tool: '%s'", toolName)
		m.logger.Printf("Request Body: %s", string(body))
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

	// Check for tool-level isError in the result content.
	// This catches game errors (e.g. "faction name too long") that are
	// returned as successful JSON-RPC responses with isError=true.
	if _, err := parseToolResultText(resp.Result); err != nil {
		return resp.Result, err
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

	// DEBUG: Log the response received
	if m.debug {
		m.logger.Printf("=== MCP Client Receive Debug ===")
		m.logger.Printf("Content-Type: %s", contentType)
		m.logger.Printf("Response Body: %s", truncate(string(bodyData), 200))
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
		SessionID    string `json:"session_id"`
		CurrentTick  *int64 `json:"current_tick,omitempty"` // Try to get tick from login
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

	// Check if login response contains current_tick
	if loginResp.CurrentTick != nil {
		m.state.CurrentTick = *loginResp.CurrentTick
		m.logger.Printf("[MCP] Login provided current_tick: %d", *loginResp.CurrentTick)
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

	// Ensure System.ID is set from player data if not already set.
	// This is critical for MCP which doesn't get a welcome message.
	if m.state.System.ID == "" && m.state.Player.CurrentSystem != "" {
		m.state.System.ID = m.state.Player.CurrentSystem
		m.logger.Printf("[MCP] Set System.ID = '%s' from player.CurrentSystem (login)", m.state.Player.CurrentSystem)
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

// pollLoop periodically calls get_notifications to keep tick and state fresh.
// This is much lighter than get_status — it only returns tick, timestamp,
// and any pending notifications.
func (m *MCPGameClient) pollLoop() {
	ticker := time.NewTicker(SleepTick)
	defer ticker.Stop()

	for {
		select {
		case <-m.pollCtx.Done():
			return
		case <-ticker.C:
			if err := m.pollNotifications(); err != nil {
				m.logger.Printf("[MCP] Poll error: %v", err)
			}
		}
	}
}

// pollNotifications fetches current tick and notifications.
func (m *MCPGameClient) pollNotifications() error {
	ctx, cancel := context.WithTimeout(m.pollCtx, 30*time.Second)
	defer cancel()

	result, err := m.callTool(ctx, "get_notifications", nil)
	if err != nil {
		return err
	}

	return m.updateStateFromResult(result)
}

// updateStateFromResult parses an MCP tool result's text content and
// updates the internal state. The text is expected to be JSON matching
// the server's response payload structure.
// cacheResultAs caches the raw tool result under the given key and then updates state.
func (m *MCPGameClient) cacheResultAs(result json.RawMessage, key string) error {
	if text, err := parseToolResultText(result); err == nil {
		m.rawJSONMu.Lock()
		m.latestRawJSON[key] = []byte(text)
		m.rawJSONMu.Unlock()
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) updateStateFromResult(result json.RawMessage) error {
	text, err := parseToolResultText(result)
	if err != nil {
		return err
	}

	// Cache last response for interactive tools like play_as
	m.rawJSONMu.Lock()
	m.latestRawJSON["_last"] = []byte(text)
	m.rawJSONMu.Unlock()

	// Try to parse as a structured response with player/ship/system fields.
	var payload struct {
		Action      string          `json:"action,omitempty"`
		Player      json.RawMessage `json:"player,omitempty"`
		Ship        json.RawMessage `json:"ship,omitempty"`
		System      json.RawMessage `json:"system,omitempty"`
		POI         json.RawMessage `json:"poi,omitempty"`
		Modules     json.RawMessage `json:"modules,omitempty"`
		Nearby      json.RawMessage `json:"nearby,omitempty"`
		CurrentTick *int64          `json:"current_tick,omitempty"`
		Timestamp   *int64          `json:"timestamp,omitempty"`
		Listings    json.RawMessage `json:"listings,omitempty"`
		Docked      *bool           `json:"docked,omitempty"`
		CurrentPOI  string          `json:"current_poi,omitempty"`
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

	// If still not parsed, try SSE format: {"result": {"content": [{"type": "text", "text": "..."}]}}
	// The actual game data is in result.content[0].text
	if payload.Player == nil && payload.Ship == nil && payload.System == nil && payload.Message == "" {
		var sseWrapper struct {
			Result struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(text), &sseWrapper); err == nil && len(sseWrapper.Result.Content) > 0 {
			// Find the first text content block
			for _, c := range sseWrapper.Result.Content {
				if c.Type == "text" && c.Text != "" {
					if m.debug {
						m.logger.Printf("[MCP DEBUG] unwrapping SSE content text: %s", truncate(c.Text, 500))
					}
					// Re-parse the text content as the payload
					if err := json.Unmarshal([]byte(c.Text), &payload); err != nil {
						if m.debug {
							m.logger.Printf("[MCP DEBUG] failed to parse SSE content text: %v", err)
						}
					} else {
						// Successfully parsed, break out of loop
						break
					}
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
			// Preserve XP values refreshed via refreshXPFromSkills. MCP
			// mutation responses only carry skill levels (XP=0), so a
			// naive overwrite would wipe the XP data the tracker needs.
			// Merge level updates from the response into existing skills,
			// keeping current XP when the incoming value is 0.
			if player.Skills != nil && m.state.Player.Skills != nil {
				for k, newSkill := range player.Skills {
					if existing, ok := m.state.Player.Skills[k]; ok {
						if newSkill.XP == 0 && existing.XP > 0 {
							newSkill.XP = existing.XP
						}
						player.Skills[k] = newSkill
					}
				}
			}
			// Same rationale for state.SkillXP: only overwrite if the
			// response actually carried skill_xp data.
			preservedSkillXP := m.state.SkillXP
			m.state.Player = player
			m.state.Credits = player.Credits
			m.state.CurrentSystem = player.CurrentSystem
			m.state.CurrentPOI = player.CurrentPOI
			m.state.Doc = player.DockedAtBase != ""
			if len(player.SkillXP) > 0 {
				m.state.SkillXP = player.SkillXP
			} else {
				m.state.SkillXP = preservedSkillXP
			}

			// Ensure System.ID is set when player data provides a system.
			// This handles the case where parseSystemData hasn't run yet
			// (e.g., MCP transport which doesn't get a welcome message).
			if m.state.System.ID == "" && player.CurrentSystem != "" {
				m.state.System.ID = player.CurrentSystem
				m.logger.Printf("[MCP] Set System.ID = '%s' from player.CurrentSystem (was empty)", player.CurrentSystem)
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

	if payload.POI != nil {
		// Parse POI data (from get_poi response) and update the system's POI list.
		// This populates resources and other detailed fields.
		var extPOI serverapi.POI
		if err := json.Unmarshal(payload.POI, &extPOI); err == nil && extPOI.ID != "" {
			poi := POIFromAPI(extPOI)

			// Find and update the existing POI in the system, or append if new
			found := false
			for i, existing := range m.state.System.POIs {
				if existing.ID == poi.ID {
					m.state.System.POIs[i] = poi
					found = true
					if m.debug {
						m.logger.Printf("[MCP DEBUG] Updated POI %s with %d resources", poi.ID, len(poi.Resources))
					}
					break
				}
			}
			if !found {
				m.state.System.POIs = append(m.state.System.POIs, poi)
				if m.debug {
					m.logger.Printf("[MCP DEBUG] Added new POI %s with %d resources", poi.ID, len(poi.Resources))
				}
			}
		}
	}
		if payload.Modules != nil {
			// Parse module definitions from get_ship response
			var modules []serverapi.ShipModule
			if err := json.Unmarshal(payload.Modules, &modules); err == nil {
				if m.state.ModuleDefinitions == nil {
					m.state.ModuleDefinitions = make(map[string]ModuleDefinition)
				}
				for _, extMod := range modules {
					if extMod.ID != "" {
						m.state.ModuleDefinitions[extMod.ID] = ModuleDefinitionFromShipModule(extMod)
						if m.debug {
							m.logger.Printf("[MCP DEBUG] Parsed module %s: %s (type_id: %s)", extMod.ID, extMod.Name, extMod.TypeID)
						}
					}
				}
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
		if m.debug {
			m.logger.Printf("[MCP] Tick updated: %d", *payload.CurrentTick)
		}
	}

	if payload.Timestamp != nil {
		m.state.ServerTimestamp = *payload.Timestamp
	}

	if payload.Docked != nil {
		m.state.Doc = *payload.Docked
	}

	if payload.CurrentPOI != "" {
		m.state.CurrentPOI = payload.CurrentPOI
	}

	// Update state from action responses (dock, undock, travel, arrived).
	switch payload.Action {
	case "dock":
		m.state.Doc = true
		m.state.Traveling = false
	case "undock":
		m.state.Doc = false
	case "arrived":
		m.state.Traveling = false
	case "travel":
		m.state.Traveling = true
	}

	// Run API change detection on the parsed response (same as WebSocket transport).
	if payload.Action != "" {
		var rawMap map[string]any
		if json.Unmarshal([]byte(text), &rawMap) == nil {
			CheckOKResponseFields(rawMap)
		}
	}

	// Fire XP observation callback if skills changed. Only runs when a
	// callback is installed AND the response updated player data (since
	// XP lives on Player.Skills / SkillXP). m.mu is still held.
	if payload.Player != nil {
		m.checkXPChanges()
	}

	return nil
}

// checkXPChanges compares the current skill state against the snapshot
// taken before the last tool call, and fires the XP callback with any
// deltas. Must be called with m.mu held. This mirrors the WebSocket
// client's implementation in client.go.
func (m *MCPGameClient) checkXPChanges() {
	m.xpMu.Lock()
	cb := m.xpCallback
	if cb == nil {
		m.xpMu.Unlock()
		return
	}

	currentSkills := copySkillMap(m.state.Player.Skills)
	currentXP := copyStringFloatMap(m.state.SkillXP)
	gameTick := m.state.CurrentTick

	beforeSkills := m.xpLastSkills
	beforeXP := m.xpLastXP
	action := m.xpLastAction
	target := m.xpLastTarget
	quantity := m.xpLastQuantity

	// Update last known state
	m.xpLastSkills = currentSkills
	m.xpLastXP = currentXP
	m.xpMu.Unlock()

	// Skip first call (no previous state to compare)
	if beforeSkills == nil && beforeXP == nil {
		return
	}

	// Check if anything actually changed (quick check)
	changed := false
	for k, v := range currentXP {
		if beforeXP[k] != v {
			changed = true
			break
		}
	}
	if !changed {
		for k, v := range currentSkills {
			if b, ok := beforeSkills[k]; !ok || b.Level != v.Level || b.XP != v.XP {
				changed = true
				break
			}
		}
	}
	if !changed {
		return
	}

	cb(action, target, quantity, beforeSkills, currentSkills, beforeXP, currentXP, gameTick)
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
		"session expired",
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
