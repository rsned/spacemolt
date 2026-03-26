package game

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Client represents a WebSocket client for the Spacemolt game
type Client struct {
	conn        *websocket.Conn
	url         string
	username    string
	password    string // Permanent password from registration
	state       *State
	mu          sync.RWMutex
	handler     MessageHandler
	stopCh      chan struct{}
	connected   bool
	debugLogger *log.Logger

	// Ready synchronization - closed when first message is received
	readyChan chan struct{}
	readyOnce sync.Once

	// Market listings data (from get_listings response)
	latestListings []MarketListing
	listingsMu     sync.RWMutex

	// Ship listings data (from get_ships response)
	latestShips map[string]any
	shipsMu     sync.RWMutex

	// Raw JSON payloads from server responses (for saving complete server data)
	latestRawJSON map[string][]byte
	rawJSONMu     sync.RWMutex

	// Crafting configuration
	CraftingConfig *CraftingConfig

	// Last error response (for diagnostics)
	lastError   map[string]any
	lastErrorMu sync.RWMutex

	// Response waiting for synchronous operations
	waiterMu sync.Mutex
	waiters  map[string]chan protocol.Response

	// Command queue for sequential execution
	CmdQueue *CommandQueue

	// Connection health monitoring
	lastMessageTime time.Time
	lastMessageMu   sync.RWMutex
	pingInterval    time.Duration
	pongTimeout     time.Duration
	stopPing        chan struct{}

	// Goroutine lifecycle management
	goroutineCtx    context.Context
	goroutineCancel context.CancelFunc
	goroutineWg     sync.WaitGroup

	// Map data cache - get_map data is static and changes less than once per hour
	mapFetchedAt time.Time
	mapFetchedMu sync.RWMutex

	// IP rate limit tracking
	ipBlockedUntil time.Time
	ipBlockedMu    sync.RWMutex

	// Diagnostic tracking
	connectionID      string    // Unique ID for this connection instance
	connectTime       time.Time // When this connection was established
	messagesSent      int64     // Counter for messages sent
	messagesReceived  int64     // Counter for messages received
	lastSendTime      time.Time // Time of last send
	lastReceiveTime   time.Time // Time of last receive
	diagnosticMu      sync.RWMutex
	goroutineID       int64 // Counter for tracking goroutine instances

	sendOverride func(ctx context.Context, msg protocol.Message) error // Test hook

	// Storage update callback — fired when a view_storage response is received
	onStorageUpdate func(resp StorageUpdateEvent)
	onStorageMu     sync.RWMutex
}

// MessageHandler handles incoming game messages
type MessageHandler interface {
	OnConnected(state *State)
	OnMessage(resp protocol.Response)
	OnDisconnected(err error)
}

// ReconnectingHandler wraps a MessageHandler and adds automatic reconnection
type ReconnectingHandler struct {
	client       *Client
	handler      MessageHandler
	ctx          context.Context
	logger       *log.Logger
	reconnecting atomic.Bool   // Prevents multiple concurrent reconnections
	wg           sync.WaitGroup // Track reconnection goroutine lifecycle
}

// NewReconnectingHandler creates a handler that automatically reconnects on disconnect
func NewReconnectingHandler(client *Client, handler MessageHandler, ctx context.Context, logger *log.Logger) *ReconnectingHandler {
	return &ReconnectingHandler{
		client:  client,
		handler: handler,
		ctx:     ctx,
		logger:  logger,
	}
}

func (r *ReconnectingHandler) OnConnected(state *State) {
	if r.handler != nil {
		r.handler.OnConnected(state)
	}
}

func (r *ReconnectingHandler) OnMessage(resp protocol.Response) {
	if r.handler != nil {
		r.handler.OnMessage(resp)
	}
}

func (r *ReconnectingHandler) OnDisconnected(err error) {
	// Notify wrapped handler first
	if r.handler != nil {
		r.handler.OnDisconnected(err)
	}

	// Only start reconnection if not already reconnecting
	if r.reconnecting.CompareAndSwap(false, true) {
		r.wg.Add(1)
		go r.attemptReconnection()
	}
}

func (r *ReconnectingHandler) attemptReconnection() {
	defer r.wg.Done()
	defer r.reconnecting.Store(false)

	maxAttempts := 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if r.ctx.Err() != nil {
			r.logger.Printf("Context cancelled, stopping reconnection attempts")
			return
		}

		backoff := time.Duration(1<<uint(attempt)) * time.Second
		r.logger.Printf("Reconnection attempt %d/%d after %v", attempt, maxAttempts, backoff)
		time.Sleep(backoff)

		if err := r.client.Reconnect(r.ctx); err != nil {
			r.logger.Printf("Reconnection attempt %d failed: %v", attempt, err)
			continue
		}

		r.logger.Printf("✓ Reconnected successfully")
		return
	}

	r.logger.Printf("Failed to reconnect after %d attempts", maxAttempts)
}

// WaitForShutdown waits for any active reconnection goroutine to complete.
// This should be called during graceful shutdown to ensure all goroutines exit cleanly.
func (r *ReconnectingHandler) WaitForShutdown(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// NewClient creates a new game client
func NewClient(url, username, password string, debugLogger *log.Logger) *Client {
	if debugLogger == nil {
		debugLogger = log.New(log.Writer(), "[GAME] ", log.LstdFlags)
	}

	goroutineCtx, goroutineCancel := context.WithCancel(context.Background())

	client := &Client{
		url:            url,
		username:       username,
		password:       password,
		state: &State{
			Doc:         true,
			MaxCargo:    10,
			CurrentTick: 0,
			System: SystemData{
				POIs:        []POI{},
				Connections: []ConnectionInfo{},
			},
			Nearby:   []NearbyPlayer{},
			InCombat: false,
		},
		stopCh:          make(chan struct{}),
		readyChan:       make(chan struct{}),
		waiters:         make(map[string]chan protocol.Response),
		debugLogger:     debugLogger,
		latestListings:  make([]MarketListing, 0),
		latestShips:     make(map[string]any),
		latestRawJSON:   make(map[string][]byte),
		lastError:       make(map[string]any),
		pingInterval:    SleepWSHealthCheck,
		pongTimeout:     SleepWSPongTimeout,
		stopPing:        make(chan struct{}),
		CmdQueue:        NewCommandQueue(nil), // Will be set after creation
		goroutineCtx:    goroutineCtx,
		goroutineCancel: goroutineCancel,
	}
	client.CmdQueue.client = client // Set the client reference
	return client
}

// SetOnStorageUpdate registers a callback that fires when a view_storage response
// is received. This allows automatic storage snapshot recording without modifying
// individual agent code.
func (c *Client) SetOnStorageUpdate(fn func(resp StorageUpdateEvent)) {
	c.onStorageMu.Lock()
	defer c.onStorageMu.Unlock()
	c.onStorageUpdate = fn
}

// SetDebugLogging controls whether the game client logs WebSocket messages.
// When disabled, the debug logger output is discarded.
func (c *Client) SetDebugLogging(enabled bool) {
	if !enabled {
		c.debugLogger.SetOutput(io.Discard)
	}
}

// Connect establishes a WebSocket connection to the game server
// Implements retry logic with exponential backoff for rate limiting (429 errors)
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	maxRetries := 5
	baseDelay := 2 * time.Second

	var ws *websocket.Conn
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var resp *http.Response
		ws, resp, err = websocket.Dial(ctx, c.url, &websocket.DialOptions{
			HTTPHeader: http.Header{
				"User-Agent": []string{UserAgent},
			},
		})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			// Success!
			break
		}

		// Check if this is a 429 (rate limit) error
		errMsg := err.Error()
		isRateLimited := false
		if len(errMsg) > 0 {
			// Check for "429" in error message
			for i := 0; i < len(errMsg)-2; i++ {
				if errMsg[i:i+3] == "429" {
					isRateLimited = true
					break
				}
			}
		}

		// If this is the last attempt, or not a rate limit error, fail
		if attempt >= maxRetries || !isRateLimited {
			return fmt.Errorf("failed to connect: %w", err)
		}

		// Calculate exponential backoff delay
		delay := baseDelay * time.Duration(1<<uint(attempt))
		c.debugLogger.Printf("Rate limited (429), retrying in %v (attempt %d/%d)", delay, attempt+1, maxRetries)

		// Wait before retrying (check for context cancellation)
		select {
		case <-time.After(delay):
			// Continue to next retry
		case <-ctx.Done():
			return fmt.Errorf("connection cancelled: %w", ctx.Err())
		}
	}

	// Set a large read limit (10MB) to handle large state updates
	// Default is 32KB which is too small for system info with many POIs
	ws.SetReadLimit(10 * 1024 * 1024) // 10MB

	// Already holding the lock from line 173, no need to lock again
	c.conn = ws
	c.connected = true

	c.state.Username = c.username
	c.state.Password = c.password

	// Initialize diagnostics for this connection
	c.diagnosticMu.Lock()
	c.connectionID = generateConnectionID()
	c.connectTime = time.Now()
	c.messagesSent = 0
	c.messagesReceived = 0
	c.lastSendTime = time.Time{}
	c.lastReceiveTime = time.Time{}
	c.lastMessageTime = time.Now() // Initialize to now to avoid false timeout
	c.diagnosticMu.Unlock()

	goroutineID := atomic.AddInt64(&c.goroutineID, 1)
	c.debugLogger.Printf("Connected to %s (read limit: 10MB) | Connection ID: %s | Goroutine: %d", c.url, c.connectionID, goroutineID)

	// Start message listener with managed lifecycle
	c.goroutineWg.Add(1)
	go func() {
		defer c.goroutineWg.Done()
		c.listen(c.goroutineCtx)
	}()

	// Start connection health monitoring with managed lifecycle
	c.goroutineWg.Add(1)
	go func() {
		defer c.goroutineWg.Done()
		c.monitorConnectionHealth(c.goroutineCtx)
	}()

	return nil
}

// Disconnect closes the WebSocket connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Log connection metrics before disconnecting
	c.logConnectionMetrics("client_disconnect")

	// Signal all goroutines to stop
	c.goroutineCancel()

	// Stop health monitoring
	select {
	case c.stopPing <- struct{}{}:
	default:
	}

	if c.conn != nil {
		c.connected = false
		// Use AbortiveClose to force immediate closure without waiting for WebSocket close handshake
		// This unblocks the Read() call in listen() immediately
		conn := c.conn
		c.conn = nil

		// First try graceful close
		_ = conn.Close(websocket.StatusNormalClosure, "client disconnect")
		c.debugLogger.Printf("Disconnected from server")

		// If graceful close doesn't unblock the read within 1 second, force close
		done := make(chan struct{})
		go func() {
			c.goroutineWg.Wait()
			close(done)
		}()

		select {
		case <-done:
			c.debugLogger.Printf("All goroutines exited cleanly")
		case <-time.After(1 * time.Second):
			// Goroutines are stuck, force close underlying connection
			c.debugLogger.Printf("Goroutines slow to exit, connection closed")
		}
	}

	// Wait for all goroutines to exit (with longer timeout for safety)
	done := make(chan struct{})
	go func() {
		c.goroutineWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Already logged above
	case <-time.After(10 * time.Second):
		c.debugLogger.Printf("Warning: Extended timeout waiting for goroutines to exit")
	}

	return nil
}

// Reconnect disconnects and reconnects to the server
func (c *Client) Reconnect(ctx context.Context) error {
	c.debugLogger.Printf("Attempting to reconnect...")

	// Close existing connection if any
	_ = c.Disconnect()

	// Reset goroutine context for the new connection
	c.mu.Lock()
	c.goroutineCancel()
	c.goroutineCtx, c.goroutineCancel = context.WithCancel(context.Background())
	c.mu.Unlock()

	// Wait a moment before reconnecting with exponential backoff
	select {
	case <-time.After(2 * time.Second):
		// Continue
	case <-ctx.Done():
		return ctx.Err()
	}

	// Reconnect with retries
	maxRetries := 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			c.debugLogger.Printf("Reconnect attempt %d/%d, waiting %v", attempt+1, maxRetries, backoff)
			select {
			case <-time.After(backoff):
				// Continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if err := c.Connect(ctx); err != nil {
			lastErr = err
			c.debugLogger.Printf("Reconnect attempt %d failed: %v", attempt+1, err)
			continue
		}

		// Wait for connection to be ready
		if err := c.WaitForReady(ctx, 10*time.Second); err != nil {
			lastErr = err
			c.debugLogger.Printf("Connection not ready after attempt %d: %v", attempt+1, err)
			_ = c.Disconnect()
			continue
		}

		// Re-authenticate
		if err := c.Login(ctx); err != nil {
			lastErr = err
			c.debugLogger.Printf("Login failed after attempt %d: %v", attempt+1, err)
			_ = c.Disconnect()
			continue
		}

		c.debugLogger.Printf("Reconnected and logged in successfully")
		return nil
	}

	return fmt.Errorf("reconnect failed after %d attempts: %w", maxRetries, lastErr)
}

// SetHandler sets the message handler
func (c *Client) SetHandler(handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}

// Send sends a message to the game server
func (c *Client) Send(ctx context.Context, msg protocol.Message) error {
	if c.sendOverride != nil {
		return c.sendOverride(ctx, msg)
	}

	// Check IP rate limit before sending
	c.ipBlockedMu.RLock()
	blockedUntil := c.ipBlockedUntil
	c.ipBlockedMu.RUnlock()

	if time.Now().Before(blockedUntil) {
		waitDuration := time.Until(blockedUntil)
		c.debugLogger.Printf("IP rate limited, waiting %v before sending (type: %s)", waitDuration, msg.Type)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
			c.debugLogger.Printf("IP rate limit wait complete, resuming send (type: %s)", msg.Type)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// DEBUG: Log the message being sent
	c.debugLogger.Printf("=== Game Client Send Debug ===")
	c.debugLogger.Printf("Message Type: '%s'", msg.Type)
	if len(msg.Payload) > 0 {
		payloadJSON, _ := json.Marshal(msg.Payload)
		c.debugLogger.Printf("Message Payload: %s", string(payloadJSON))
	}
	//c.debugLogger.Printf("Full JSON being sent to WebSocket: %s", string(data))

	// Track message for diagnostics
	c.trackMessageSent()

	// Set a write timeout to prevent hanging
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := c.conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		c.debugLogger.Printf("ERROR sending to WebSocket: %v", err)
		return fmt.Errorf("failed to send message: %w", err)
	}

	//c.debugLogger.Printf("WebSocket write successful")
	return nil
}

// Login authenticates with the server using stored credentials
// This is a synchronous operation that waits for the server response
func (c *Client) Login(ctx context.Context) error {
	if c.password == "" {
		return fmt.Errorf("no password available")
	}

	msg := protocol.Message{
		Type: "login",
		Payload: map[string]any{
			"username": c.username,
			"password": c.password,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	if err := c.Send(ctx, msg); err != nil {
		return fmt.Errorf("failed to send login: %w", err)
	}

	// Wait for login response (success or error)
	_, err := c.waitForAuthResponse(ctx, protocol.TypeLoggedIn, 10*time.Second)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	return nil
}

// Register creates a new account
// This is a synchronous operation that waits for the server response
func (c *Client) Register(ctx context.Context, empire, registrationCode string) error {
	payload := map[string]any{
		"username": c.username,
		"empire":   empire,
	}

	// Add registration code if provided
	if registrationCode != "" {
		payload["registration_code"] = registrationCode
	}

	msg := protocol.Message{
		Type:      "register",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}

	if err := c.Send(ctx, msg); err != nil {
		return fmt.Errorf("failed to send register: %w", err)
	}

	// Wait for register response (success or error)
	_, err := c.waitForAuthResponse(ctx, protocol.TypeRegistered, 10*time.Second)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Token is updated by handleResponse() when the response is processed
	return nil
}

// Claim links the current player to a website account using a registration code
// This is a synchronous operation that waits for the server response
func (c *Client) Claim(ctx context.Context, registrationCode string) error {
	msg := protocol.Message{
		Type: "claim",
		Payload: map[string]any{
			"registration_code": registrationCode,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	if err := c.Send(ctx, msg); err != nil {
		return fmt.Errorf("failed to send claim: %w", err)
	}

	// Wait for claim response (success or error)
	if err := c.waitForActionResponse(ctx, 10*time.Second); err != nil {
		return fmt.Errorf("claim failed: %w", err)
	}

	return nil
}

// Undock undocks from the current station
func (c *Client) Undock(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "undock",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Dock docks at a station in the current system
func (c *Client) Dock(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "dock",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Travel travels to a POI within the current system
// Travel travels to a POI within the current system.
// It blocks until the ship arrives at the destination or an error occurs.
// The returned TravelResult contains the final POI the ship ended up at.
func (c *Client) Travel(ctx context.Context, targetPOI string) (*TravelResult, error) {
	if err := c.Send(ctx, protocol.Message{
		Type:      "travel",
		Payload:   map[string]any{"target_poi": targetPOI},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return nil, err
	}

	// Wait for initial server acknowledgment (OK or error).
	resp, err := c.waitForInitialResponse(ctx, SleepTick)
	if err != nil {
		return nil, err
	}

	// Handle "already_there" — returned as an error response with code.
	if resp.Type == protocol.TypeError {
		if code, _ := resp.Payload["code"].(string); code == "already_there" {
			state := c.GetState()
			return &TravelResult{POI: state.CurrentPOI}, nil
		}
	}

	// Update CurrentTick from response if present (prevents stale tick values)
	if tick, ok := resp.Payload["tick"].(float64); ok {
		c.mu.Lock()
		c.state.CurrentTick = int64(tick)
		c.mu.Unlock()
	}

	// Compute timeout from arrival_tick if available, else use generous default.
	timeout := 90 * time.Second
	if arrivalTick, ok := resp.Payload["arrival_tick"].(float64); ok {
		currentTick := c.GetState().CurrentTick
		ticksRemaining := int64(arrivalTick) - currentTick
		if ticksRemaining < 1 {
			ticksRemaining = 1
		}
		// Each tick ~10s, plus 30s buffer for safety.
		timeout = time.Duration(ticksRemaining)*SleepTick + 30*time.Second
	}

	c.debugLogger.Printf("Travel to %s: waiting up to %v for arrival", targetPOI, timeout)

	// Block until state.Traveling becomes false (arrival or interruption).
	if err := c.waitForStateChange(ctx, func(s *State) bool {
		return !s.Traveling
	}, timeout); err != nil {
		return &TravelResult{Canceled: true}, fmt.Errorf("travel to %s: %w", targetPOI, err)
	}

	state := c.GetState()
	return &TravelResult{
		POI:      state.CurrentPOI,
		Canceled: false,
	}, nil
}

// Jump jumps to another system
// Jump jumps to another system.
// It blocks until the ship arrives in the new system or an error occurs.
// The returned JumpResult contains the destination system info.
func (c *Client) Jump(ctx context.Context, targetSystem string) (*JumpResult, error) {
	if err := c.Send(ctx, protocol.Message{
		Type:      "jump",
		Payload:   map[string]any{"target_system": targetSystem},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return nil, err
	}

	// Wait for initial server acknowledgment.
	resp, err := c.waitForInitialResponse(ctx, SleepTick)
	if err != nil {
		return nil, err
	}

	// Handle benign errors.
	if resp.Type == protocol.TypeError {
		if code, _ := resp.Payload["code"].(string); code == "already_there" {
			state := c.GetState()
			return &JumpResult{SystemID: state.System.ID, SystemName: state.System.Name}, nil
		}
	}

	// Update CurrentTick from response if present (prevents stale tick values)
	if tick, ok := resp.Payload["tick"].(float64); ok {
		c.mu.Lock()
		c.state.CurrentTick = int64(tick)
		c.mu.Unlock()
	}

	// Compute timeout from arrival_tick if available.
	timeout := 90 * time.Second
	if arrivalTick, ok := resp.Payload["arrival_tick"].(float64); ok {
		currentTick := c.GetState().CurrentTick
		ticksRemaining := int64(arrivalTick) - currentTick
		if ticksRemaining < 1 {
			ticksRemaining = 1
		}
		timeout = time.Duration(ticksRemaining)*SleepTick + 30*time.Second
	}

	c.debugLogger.Printf("Jump to %s: waiting up to %v for arrival", targetSystem, timeout)

	// Block until state.Traveling becomes false (jump completed).
	if err := c.waitForStateChange(ctx, func(s *State) bool {
		return !s.Traveling
	}, timeout); err != nil {
		return &JumpResult{Canceled: true}, fmt.Errorf("jump to %s: %w", targetSystem, err)
	}

	state := c.GetState()
	return &JumpResult{
		SystemID:   state.System.ID,
		SystemName: state.System.Name,
		POI:        state.CurrentPOI,
	}, nil
}

// Mine mines resources at the current location
func (c *Client) Mine(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "mine",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Attack attacks a target player or NPC
func (c *Client) Attack(ctx context.Context, targetID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "attack",
		Payload:   map[string]any{"target_id": targetID, "weapon_idx": 0},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Scan scans the current area
func (c *Client) Scan(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "scan",
		Payload:   map[string]any{"target_id": "area"},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// SurveySystem scans for hidden POIs in the current system
// Requires a survey scanner module installed
func (c *Client) SurveySystem(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "survey_system",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FindRoute finds a route to a target system using the server's pathfinding.
// Returns the route steps (excluding the current system) or an error.
func (c *Client) FindRoute(ctx context.Context, targetSystem string) ([]RouteStep, error) {
	if err := c.Send(ctx, protocol.Message{
		Type:      "find_route",
		Payload:   map[string]any{"target_system": targetSystem},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return nil, err
	}

	resp, err := c.waitForAuthResponse(ctx, protocol.TypeOK, SleepTick)
	if err != nil {
		return nil, fmt.Errorf("find_route failed: %w", err)
	}

	// Check if a route was found
	if found, ok := resp.Payload["found"].(bool); ok && !found {
		msg, _ := resp.Payload["message"].(string)
		return nil, fmt.Errorf("no route found: %s", msg)
	}

	// Parse the route array from the response
	var steps []RouteStep
	if unmarshalPayloadKey(resp.Payload, "route", &steps) {
		return steps, nil
	}

	return nil, fmt.Errorf("find_route: could not parse route from response")
}

// GetSystem requests information about the current system.
// Blocks until the server responds.
func (c *Client) GetSystem(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "get_system",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// GetMap requests all systems with coordinates and connections.
// Map data is cached for MapCacheTTL since it changes infrequently.
// Pass force=true to bypass the cache and always fetch fresh data.
func (c *Client) GetMap(ctx context.Context, force ...bool) error {
	if len(force) == 0 || !force[0] {
		c.mapFetchedMu.RLock()
		fresh := !c.mapFetchedAt.IsZero() && time.Since(c.mapFetchedAt) < MapCacheTTL
		c.mapFetchedMu.RUnlock()
		if fresh {
			c.debugLogger.Printf("GetMap: using cached data (age %v)", time.Since(c.mapFetchedAt).Round(time.Second))
			return nil
		}
	}

	err := c.Send(ctx, protocol.Message{
		Type:      "get_map",
		Timestamp: time.Now().UnixMilli(),
	})
	if err == nil {
		c.mapFetchedMu.Lock()
		c.mapFetchedAt = time.Now()
		c.mapFetchedMu.Unlock()
	}
	return err
}

// GetPOI requests information about the current POI.
// Blocks until the server responds.
func (c *Client) GetPOI(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "get_poi",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// GetStatus requests player status.
// Blocks until the server responds.
func (c *Client) GetStatus(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "get_status",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// GetNotifications retrieves pending notifications and the current tick/timestamp.
// Over WebSocket, notifications and tick updates are pushed automatically by the
// server, so this is a no-op. The MCP client implementation calls the tool.
func (c *Client) GetNotifications(_ context.Context) error {
	// WebSocket connections receive notifications via push — no polling needed.
	return nil
}

// GetListings requests market listings for the current station.
// Blocks until the server responds.
func (c *Client) GetListings(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "view_market",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// GetShips requests ship listings from the current station.
// Blocks until the server responds.
func (c *Client) GetShips(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "get_ships",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// GetShipListings returns the most recently fetched ship listings
func (c *Client) GetShipListings() map[string]any {
	c.shipsMu.RLock()
	defer c.shipsMu.RUnlock()

	// Return a copy of the ships data
	result := make(map[string]any)
	for k, v := range c.latestShips {
		result[k] = v
	}
	return result
}

// Sell sells items from cargo at the current station
func (c *Client) Sell(ctx context.Context, itemID string, quantity float64) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "sell",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// CreateBulkSellOrder creates multiple sell orders in a single API call (up to 50 items).
// This is more efficient than calling Sell repeatedly for each item.
// The orders parameter should be prepared using PrepareBulkSellOrder().
//
// Example:
//
//	orders, _ := game.PrepareBulkSellOrder(state.Ship.Cargo, []string{}, priceMap)
//	if len(orders) > 0 {
//	    err := client.CreateBulkSellOrder(ctx, orders)
//	}
func (c *Client) CreateBulkSellOrder(ctx context.Context, orders []BulkSellOrder) error {
	if len(orders) == 0 {
		return nil // Nothing to sell
	}

	if len(orders) > 50 {
		return fmt.Errorf("bulk sell order limited to 50 items, got %d", len(orders))
	}

	if err := c.Send(ctx, protocol.Message{
		Type:      "create_sell_order",
		Payload:   map[string]any{"orders": orders},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// SellAllBulk sells all cargo items using the bulk create_sell_order API.
// This is much faster than SellAll as it makes only one API call instead of N calls.
// It fetches market listings to price items competitively, then creates sell orders.
// Only sells ores and resources, not equipment.
//
// Parameters:
//   - ctx: Context for cancellation
//   - reservedItems: Optional list of item IDs to keep (not sell)
//
// Returns error if not docked or if API call fails.
func (c *Client) SellAllBulk(ctx context.Context, reservedItems []string) error {
	state := c.GetState()
	if !state.Doc {
		return fmt.Errorf("must be docked to sell")
	}

	if len(state.Ship.Cargo) == 0 {
		return nil // Nothing to sell
	}

	// Get market listings for pricing
	if err := c.GetListings(ctx); err != nil {
		c.debugLogger.Printf("Warning: Failed to get market listings: %v (using default prices)", err)
	}
	time.Sleep(1 * time.Second) // Wait for listings response

	listings := c.GetMarketListings()
	priceMap := GetMarketPricesForCargo(state.Ship.Cargo, listings)

	// Prepare bulk sell orders
	orders, skippedCount := PrepareBulkSellOrder(state.Ship.Cargo, reservedItems, priceMap)

	if len(orders) == 0 {
		if skippedCount > 0 {
			c.debugLogger.Printf("No items to sell (%d reserved/equipment items skipped)", skippedCount)
		}
		return nil
	}

	c.debugLogger.Printf("Creating bulk sell order for %d items (%d skipped)", len(orders), skippedCount)

	// Create bulk sell order
	return c.CreateBulkSellOrder(ctx, orders)
}

// SellAll sells all cargo items at the current station
func (c *Client) SellAll(ctx context.Context) error {
	state := c.GetState()
	if !state.Doc {
		return fmt.Errorf("must be docked to sell")
	}

	if len(state.Ship.Cargo) == 0 {
		return nil // Nothing to sell
	}

	// Sell each item in cargo (but only ores/resources, not equipment)
	for _, item := range state.Ship.Cargo {
		if item.Quantity > 0 {
			// Only sell ore and resources, not equipment/modules/weapons
			// This prevents selling items we just bought as upgrades
			if c.isOreOrResource(item.ItemID) {
				if err := c.Sell(ctx, item.ItemID, item.Quantity); err != nil {
					c.debugLogger.Printf("Failed to sell %s: %v", item.ItemID, err)
					// Continue selling other items even if one fails
				}
				time.Sleep(10 * time.Second) // Wait between sells to respect game tick rate
			} else {
				c.debugLogger.Printf("Skipping sale of equipment: %s (keeping for installation)", item.ItemID)
			}
		}
	}

	return nil
}

// isOreOrResource returns true if the item is ore or a resource (should be sold)
func (c *Client) isOreOrResource(itemID string) bool {
	// Ores and resources to sell
	oreAndResourcePrefixes := []string{
		"ore_",     // All ores (ore_iron, ore_copper, etc.)
		"gas_",     // Gases
		"crystal_", // Crystals
		"salvage_", // Salvage materials
		"scrap_",   // Scrap materials
	}

	for _, prefix := range oreAndResourcePrefixes {
		if len(itemID) >= len(prefix) && itemID[:len(prefix)] == prefix {
			return true
		}
	}

	// Don't sell these - they're equipment
	// mining_laser_*, weapon_*, shield_*, cargo_*, engine_*, module_*, etc.
	return false
}

// DepositItems deposits items from the ship's cargo to station storage.
// This moves the specified quantity of items from cargo to the station's storage.
//
// Parameters:
//   - ctx: Context for cancellation
//   - itemID: The ID of the item to deposit
//   - quantity: The quantity to deposit (must be >= 0)
//
// Returns an error if:
//   - Not docked at a station
//   - Item not found in cargo
//   - Quantity exceeds available amount
//   - Station doesn't have storage service
//
// Example:
//
//	// Deposit all iron ore
//	err := client.DepositItems(ctx, "iron_ore", 100.0)
func (c *Client) DepositItems(ctx context.Context, itemID string, quantity float64) error {
	state := c.GetState()
	if !state.Doc {
		return fmt.Errorf("must be docked at station to deposit items")
	}

	if quantity < 0 {
		return fmt.Errorf("quantity must be >= 0, got %f", quantity)
	}

	// Check if item exists in cargo with sufficient quantity
	var availableQty float64
	for _, item := range state.Ship.Cargo {
		if item.ItemID == itemID {
			availableQty = item.Quantity
			break
		}
	}

	if availableQty == 0 {
		return fmt.Errorf("item %s not found in cargo", itemID)
	}

	if quantity > availableQty {
		return fmt.Errorf("requested quantity %f exceeds available %f for item %s", quantity, availableQty, itemID)
	}

	if err := c.Send(ctx, protocol.Message{
		Type:      "deposit_items",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}

	return c.waitForActionResponse(ctx, SleepTick)
}

// DepositAllItems deposits all items from the ship's cargo to station storage.
// This moves everything in cargo to the station's storage.
//
// Returns an error if:
//   - Not docked at a station
//   - Station doesn't have storage service
//   - Any individual deposit fails
//
// Example:
//
//	err := client.DepositAllItems(ctx)
func (c *Client) DepositAllItems(ctx context.Context) error {
	state := c.GetState()
	if !state.Doc {
		return fmt.Errorf("must be docked at station to deposit items")
	}

	if len(state.Ship.Cargo) == 0 {
		return nil // Nothing to deposit
	}

	// Deposit each item in cargo
	depositErrors := 0
	for _, item := range state.Ship.Cargo {
		if item.Quantity <= 0 {
			continue
		}

		// Check context before each deposit
		if err := ctx.Err(); err != nil {
			return err
		}

		// Wait before each deposit to avoid action_pending errors
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(SleepQuick):
		}

		if err := c.DepositItems(ctx, item.ItemID, item.Quantity); err != nil {
			c.debugLogger.Printf("Failed to deposit %s: %v", item.ItemID, err)
			depositErrors++
			// If action is pending, wait longer before next item
			if strings.Contains(err.Error(), "action_pending") || strings.Contains(err.Error(), "already pending") {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(SleepShort):
				}
			}
			// Continue depositing other items even if one fails
		} else {
			c.debugLogger.Printf("Deposited %s x%.0f", item.ItemID, item.Quantity)
			// Brief delay between deposits
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(SleepShort):
			}
		}
	}

	if depositErrors > 0 {
		return fmt.Errorf("failed to deposit %d out of %d items", depositErrors, len(state.Ship.Cargo))
	}

	return nil
}

// Refuel refills the ship's fuel tank at the current station
func (c *Client) Refuel(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "refuel",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Repair repairs the ship's hull. At station uses credits; in space uses repair kits.
// v0.240: optional params for item_id, quantity, and target (remote repair).
func (c *Client) Repair(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "repair",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// RepairWith repairs using specific options (repair kits, remote target, etc.).
func (c *Client) RepairWith(ctx context.Context, payload map[string]any) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "repair",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Fleet manages player fleet operations (create, invite, accept, decline, leave, kick, disband, status).
// v0.240: new command.
func (c *Client) Fleet(ctx context.Context, action string, playerID string) error {
	payload := map[string]any{"action": action}
	if playerID != "" {
		payload["player_id"] = playerID
	}
	if err := c.Send(ctx, protocol.Message{
		Type:      "fleet",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// DistressSignal broadcasts a distress signal to nearby players.
// v0.240: accepts optional distress_type ("fuel", "repair", "combat").
func (c *Client) DistressSignal(ctx context.Context, distressType string) error {
	payload := map[string]any{}
	if distressType != "" {
		payload["distress_type"] = distressType
	}
	if err := c.Send(ctx, protocol.Message{
		Type:      "distress_signal",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Buy purchases items or modules at the current station
func (c *Client) Buy(ctx context.Context, itemID string, quantity float64) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "buy",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Install installs a module from cargo onto the ship
func (c *Client) Install(ctx context.Context, itemID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "install",
		Payload:   map[string]any{"item_id": itemID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// GetState returns the current game state
func (c *Client) GetState() *State {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a deep copy to prevent data races.
	// The Clone() method creates an independent copy of the state,
	// ensuring concurrent access doesn't cause race conditions.
	return c.state.Clone()
}

// listen handles incoming WebSocket messages
func (c *Client) listen(ctx context.Context) {
	goroutineID := atomic.AddInt64(&c.goroutineID, 1)
	c.debugLogger.Printf("[listen-%d] Goroutine started", goroutineID)
	defer c.debugLogger.Printf("[listen-%d] Goroutine exited", goroutineID)

	for {
		select {
		case <-ctx.Done():
			c.debugLogger.Printf("[listen-%d] Context cancelled, exiting", goroutineID)
			return
		case <-c.stopCh:
			c.debugLogger.Printf("[listen-%d] Stop signal received, exiting", goroutineID)
			return
		default:
		}

		// Get connection reference with mutex protection
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			c.debugLogger.Printf("[listen-%d] Connection is nil, exiting listener", goroutineID)
			return
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()

			// Enhanced error logging with diagnostics
			c.debugLogger.Printf("[listen-%d] Connection error: %v", goroutineID, err)

			// Check if this is a server close frame
			if closeErr, ok := err.(*websocket.CloseError); ok {
				c.debugLogger.Printf("[listen-%d] Server close frame | Status: %s (%d) | Reason: %q",
					goroutineID, closeErr.Code, closeErr.Code, closeErr.Reason)
			}

			c.debugLogger.Printf("[listen-%d] Hint: If 'read limited' error, the message exceeded the read limit. Current limit: 10MB", goroutineID)
			c.logConnectionMetrics("disconnect")

			// Get handler reference with mutex protection
			c.mu.RLock()
			handler := c.handler
			c.mu.RUnlock()

			if handler != nil {
				handler.OnDisconnected(err)
			}
			return
		}

		// Track message for diagnostics
		c.trackMessageReceived()

		// Update last message time for health monitoring
		c.updateLastMessageTime()

		// Use a decoder to handle multiple concatenated JSON objects
		// The game server sometimes sends multiple JSON objects in a single message
		decoder := json.NewDecoder(bytes.NewReader(data))
		for {
			var resp protocol.Response
			if err := decoder.Decode(&resp); err != nil {
				if err == io.EOF {
					// All JSON objects decoded successfully
					break
				}
				c.debugLogger.Printf("Failed to parse message: %v | data: %s", err, string(data))
				break
			}

			// Signal ready on first successful message
			c.readyOnce.Do(func() {
				close(c.readyChan)
			})

			// DEBUG: Log received response with full details
			// Skip logging for noisy poi_arrival and poi_departure messages
			if resp.Type != "poi_arrival" && resp.Type != "poi_departure" {
				c.debugLogger.Printf("=== Game Client Receive Debug ===")
				c.debugLogger.Printf("Response Type: '%s'", resp.Type)
				if len(resp.Payload) > 0 {
					payloadJSON, _ := json.Marshal(resp.Payload)
					payloadStr := string(payloadJSON)

					if len(payloadStr) > 200 {
						c.debugLogger.Printf("Response Payload: %s... [truncated]", payloadStr[:200])
					} else {
						c.debugLogger.Printf("Response Payload: %s", payloadStr)
					}
				}
				// Check for error message in payload
				if msg, ok := resp.Payload["message"]; ok {
					c.debugLogger.Printf("Response Message: '%v'", msg)
				}
			}

			// Update state before notifying waiters, so state is current
			// when waitForResponse/waitForAuthResponse returns.
			c.handleResponse(resp)

			// Route to command queue first
			if c.CmdQueue != nil {
				c.CmdQueue.handleResponse(resp)
			}

			// Notify any waiters for this response type (legacy support)
			c.waiterMu.Lock()
			if ch, ok := c.waiters[resp.Type]; ok {
				select {
				case ch <- resp:
				default:
					// Channel full or closed, skip
				}
			}
			c.waiterMu.Unlock()

			// Notify handler for each decoded message
			// Get handler reference with mutex protection
			c.mu.RLock()
			handler := c.handler
			c.mu.RUnlock()

			if handler != nil {
				handler.OnMessage(resp)
			}
		}
	}
}

// handleResponse updates the game state based on server responses
func (c *Client) handleResponse(resp protocol.Response) {
	// Store raw JSON for key response types (has its own locking)
	c.storeRawJSON(resp)

	// Use fine-grained locking - only lock when actually updating state
	// This prevents GetState() from being blocked for long periods

	switch resp.Type {
	case protocol.TypeWelcome:
		c.mu.Lock()
		if tick, ok := resp.Payload["current_tick"].(float64); ok {
			c.state.CurrentTick = int64(tick)
		}
		if version, ok := resp.Payload["version"].(string); ok {
			c.state.ServerVersion = version
			c.debugLogger.Printf("Server version: %s", version)
		}
		c.mu.Unlock()

	case protocol.TypeRegistered:
		payloadJSON, _ := json.Marshal(resp.Payload)
		c.debugLogger.Printf("[RECVD] payload: %s", string(payloadJSON))
		// Support both 'password' (new API) and 'token' (legacy) for backward compatibility
		c.mu.Lock()
		if password, ok := resp.Payload["password"].(string); ok {
			c.state.Password = password
			c.password = password
		} else if token, ok := resp.Payload["token"].(string); ok {
			// Legacy support: token field
			c.state.Password = token
			c.password = token
		}
		c.mu.Unlock()

	case protocol.TypeLoggedIn:
		c.parsePlayerData(resp.Payload)
		c.parseShipData(resp.Payload)
		c.parseSystemData(resp.Payload)
		// Determine initial docked state from the POI data in the login response.
		// The docked_at_base field on the player persists after undocking, so
		// we use the POI type as the authoritative signal at login time.
		if poiData, ok := resp.Payload["poi"].(map[string]any); ok {
			if poiType, ok := poiData["type"].(string); ok {
				c.mu.Lock()
				c.state.Doc = poiType == "station" || poiType == "outpost"
				c.mu.Unlock()
				c.debugLogger.Printf("Login dock state: poi_type=%q → Doc=%v", poiType, c.state.Doc)
			}
		}
		// Parse pending trades from logged_in response
		if trades, ok := resp.Payload["pending_trades"].([]any); ok {
			c.mu.Lock()
			c.state.PendingTrades = make([]map[string]any, 0, len(trades))
			for _, t := range trades {
				if tm, ok := t.(map[string]any); ok {
					c.state.PendingTrades = append(c.state.PendingTrades, tm)
				}
			}
			c.mu.Unlock()
		}

	case protocol.TypeError:
		c.parseErrorState(resp.Payload)

	case protocol.TypeActionError:
		c.parseErrorAction(resp.Payload)

	case protocol.TypeOK:
		// Update tick from response if present (get_system, get_poi, etc. include current tick)
		if tick, ok := resp.Payload["tick"].(float64); ok {
			c.mu.Lock()
			c.state.CurrentTick = int64(tick)
			c.mu.Unlock()
		} else if currentTick, ok := resp.Payload["current_tick"].(float64); ok {
			c.mu.Lock()
			c.state.CurrentTick = int64(currentTick)
			c.mu.Unlock()
		}
		// Update server timestamp if present (from get_notifications response)
		if ts, ok := resp.Payload["timestamp"].(float64); ok {
			c.mu.Lock()
			c.state.ServerTimestamp = int64(ts)
			c.mu.Unlock()
		}
		c.parsePlayerData(resp.Payload)
		c.parseShipData(resp.Payload)
		// Only parse system data when the payload actually contains it
		// (e.g., get_system, get_status responses). Most OK responses don't include system data.
		if _, hasSystem := resp.Payload["system"]; hasSystem {
			c.parseSystemData(resp.Payload)
		}
		c.parsePOIData(resp.Payload)
		c.parseTravelAction(resp.Payload)
		// get_map returns type "ok" with systems array in payload
		if _, hasSystems := resp.Payload["systems"]; hasSystems {
			c.parseMapData(resp.Payload)
		}
		// get_listings returns type "ok" with listings in payload
		if _, hasListings := resp.Payload["listings"]; hasListings {
			c.parseListingsData(resp.Payload)
		}
		// view_market returns type "ok" with items array in payload
		// Only parse as market data if action is "view_market" to avoid
		// misinterpreting other responses that have "items" (cargo, ships, etc)
		if action, ok := resp.Payload["action"].(string); ok && action == "view_market" {
			if _, hasItems := resp.Payload["items"]; hasItems {
				c.parseViewMarketData(resp.Payload)
			}
		}
		// get_ships returns type "ok" with ships in payload
		if _, hasShips := resp.Payload["ships"]; hasShips {
			c.parseShipsData(resp.Payload)
		}
		// get_skills returns type "ok" with player_skills and skills in payload
		if _, hasSkills := resp.Payload["skills"]; hasSkills {
			c.parseSkillsData(resp.Payload)
		}
		// get_chat_history returns type "ok" with messages array in payload
		if action, ok := resp.Payload["action"].(string); ok && action == "get_chat_history" {
			c.parseChatHistoryData(resp.Payload)
		}

	case protocol.TypeActionResult:
		c.parseActionResult(resp.Payload)

	case protocol.TypeDocked:
		c.mu.Lock()
		c.state.Doc = true
		c.state.Traveling = false
		c.state.TravelProgress = nil
		c.mu.Unlock()

	case protocol.TypeUndocked:
		c.mu.Lock()
		c.state.Doc = false
		c.mu.Unlock()

	case protocol.TypeStateUpdate:
		c.mu.Lock()
		if tick, ok := resp.Payload["tick"].(float64); ok {
			c.state.CurrentTick = int64(tick)
		}
		if inCombat, ok := resp.Payload["in_combat"].(bool); ok {
			c.state.InCombat = inCombat
			if !inCombat {
				c.state.InBattle = false
			}
		}
		c.mu.Unlock()
		c.parsePlayerData(resp.Payload)
		c.parseShipData(resp.Payload)
		c.parseTravelProgress(resp.Payload)
		c.parseNearbyPlayers(resp.Payload)

	case protocol.TypeTick:
		c.mu.Lock()
		if tick, ok := resp.Payload["tick"].(float64); ok {
			c.state.CurrentTick = int64(tick)
		}
		c.mu.Unlock()

	case protocol.TypeMiningYield:
		resourceID, _ := resp.Payload["resource_id"].(string)
		quantity, _ := resp.Payload["quantity"].(float64)
		if resourceID != "" && quantity > 0 {
			c.mu.Lock()
			found := false
			for i, item := range c.state.Ship.Cargo {
				if item.ItemID == resourceID {
					c.state.Ship.Cargo[i].Quantity += quantity
					found = true
					break
				}
			}
			if !found {
				c.state.Ship.Cargo = append(c.state.Ship.Cargo, CargoItem{
					ItemID:   resourceID,
					Quantity: quantity,
				})
			}
			c.state.Ship.CargoUsed += quantity
			c.mu.Unlock()
		}

	case protocol.TypeListings:
		c.parseListingsData(resp.Payload)

	case protocol.TypePirateWarning:
		c.debugLogger.Printf("⚠️  PIRATE ATTACK: %v", resp.Payload)
		c.mu.Lock()
		c.state.InCombat = true
		if pirateName, ok := resp.Payload["pirate_name"].(string); ok {
			c.state.PirateName = pirateName
		}
		if pirateTier, ok := resp.Payload["pirate_tier"].(string); ok {
			c.state.PirateTier = pirateTier
		}
		if pirateID, ok := resp.Payload["pirate_id"].(string); ok {
			c.state.PirateID = pirateID
		}
		c.mu.Unlock()

	case protocol.TypePirateCombat:
		c.mu.Lock()
		if pirateName, ok := resp.Payload["pirate_name"].(string); ok {
			if damage, ok := resp.Payload["damage"].(float64); ok {
				c.debugLogger.Printf("⚔️  PIRATE COMBAT: %s dealt %v %v damage",
					pirateName,
					resp.Payload["damage_type"],
					damage)
				c.state.LastDamage = damage
			}
			c.state.PirateName = pirateName
		}
		if pirateTier, ok := resp.Payload["pirate_tier"].(string); ok {
			c.state.PirateTier = pirateTier
		}
		if pirateID, ok := resp.Payload["pirate_id"].(string); ok {
			c.state.PirateID = pirateID
		}
		if yourHull, ok := resp.Payload["your_hull"].(float64); ok {
			c.state.Hull = yourHull
		}
		if yourShield, ok := resp.Payload["your_shield"].(float64); ok {
			c.state.Ship.Shield = yourShield
		}
		c.state.InCombat = true
		c.mu.Unlock()

	case protocol.TypePoliceWarning:
		c.debugLogger.Printf("⚠️  POLICE WARNING: %v", resp.Payload)

	case protocol.TypeReconnected:
		c.debugLogger.Printf("Reconnected to ship: %v", resp.Payload)

	case protocol.TypePoliceSpawn:
		c.debugLogger.Printf("🚔 POLICE SPAWN: %v", resp.Payload)

	case protocol.TypePoliceCombat:
		c.debugLogger.Printf("⚔️  POLICE COMBAT: %v", resp.Payload)

	case protocol.TypePirateDestroyed:
		c.debugLogger.Printf("💀 PIRATE DESTROYED: %v", resp.Payload)
		c.mu.Lock()
		c.state.InCombat = false
		c.state.PirateName = ""
		c.state.PirateTier = ""
		c.state.PirateID = ""
		c.state.LastDamage = 0
		c.mu.Unlock()

	case protocol.TypePirateSpawn:
		c.debugLogger.Printf("⚠️  PIRATE SPAWNED: %v", resp.Payload)

	case protocol.TypePlayerDied:
		c.debugLogger.Printf("💀 PLAYER DIED: %v", resp.Payload)
		c.mu.Lock()
		c.state.InCombat = false
		c.state.InBattle = false
		c.state.BattleState = nil
		c.state.PirateName = ""
		c.state.PirateTier = ""
		c.state.PirateID = ""
		c.state.LastDamage = 0
		c.mu.Unlock()

	case protocol.TypeCombatUpdate:
		c.mu.Lock()
		c.state.InCombat = true
		if damage, ok := resp.Payload["damage"].(float64); ok {
			c.state.LastDamage = damage
		}
		c.mu.Unlock()

	case protocol.TypeChatMessage:
		// Real-time chat message event (e.g., local/system channel messages)
		// Log in debug mode for observability; agents can poll chat history separately
		if sender, ok := resp.Payload["sender"].(string); ok {
			if channel, ok := resp.Payload["channel"].(string); ok {
				c.debugLogger.Printf("[CHAT] %s (%s): %v", sender, channel, resp.Payload["content"])
			}
		} else {
			c.debugLogger.Printf("[CHAT] %v", resp.Payload)
		}

	default:
		logUnhandledResponseType(resp)
	}

	// Check all responses for new/unknown fields from server API changes.
	c.checkForAPIChanges(resp)
}

// unmarshalPayloadKey marshals a payload value back to JSON and unmarshals it into dest.
// Returns true if the key exists and deserialization succeeds.
func unmarshalPayloadKey(payload map[string]any, key string, dest any) bool {
	data, ok := payload[key]
	if !ok {
		return false
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return false
	}
	return json.Unmarshal(jsonBytes, dest) == nil
}

// parsePlayerData extracts player information from payload using serverapi types.
func (c *Client) parsePlayerData(payload map[string]any) {
	var ext serverapi.Player
	if !unmarshalPayloadKey(payload, "player", &ext) {
		return
	}

	player := PlayerFromAPI(ext)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.state.Player = player

	// Sync derived state fields from player data
	c.state.Username = player.Username
	c.state.Credits = player.Credits
	c.state.CurrentSystem = player.CurrentSystem
	c.state.CurrentPOI = player.CurrentPOI
	c.state.System.ShipPOI = player.CurrentPOI

	// Ensure System.ID is set when player data provides a system.
	// This handles the case where parseSystemData hasn't run yet
	// (e.g., after login before get_system response arrives).
	if c.state.System.ID == "" && player.CurrentSystem != "" {
		c.state.System.ID = player.CurrentSystem
		c.debugLogger.Printf("Set System.ID = '%s' from player.CurrentSystem (was empty)", player.CurrentSystem)
	}

	// Note: docked_at_base in the player payload persists as the last-docked
	// station even after undocking. It is NOT a reliable signal for current
	// docked state. Docked state is authoritative from:
	//   - "docked"/"undocked" events (TypeDocked/TypeUndocked)
	//   - "docked_at" field in get_location responses (null = undocked)
	//   - action_result with command "dock"/"undock"
	// We do NOT set state.Doc from docked_at_base here.

	// Sync skill XP to state level
	if len(player.SkillXP) > 0 {
		c.state.SkillXP = player.SkillXP
	}

	// Parse module definitions from player data (map module ID to name/type)
	if len(ext.Modules) > 0 {
		c.state.ModuleDefinitions = make(map[string]ModuleDefinition, len(ext.Modules))
		for modID, modDef := range ext.Modules {
			c.state.ModuleDefinitions[modID] = ModuleDefinitionFromAPI(modDef)
		}
	}
}

// parseSkillsData extracts skill definitions and next-level XP from get_skills response payload.
func (c *Client) parseSkillsData(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// player_skills: array of { skill_id, current_xp, next_level_xp, level, ... }
	var playerSkills []serverapi.PlayerSkill
	if unmarshalPayloadKey(payload, "player_skills", &playerSkills) {
		if c.state.SkillNextLevelXP == nil {
			c.state.SkillNextLevelXP = make(map[string]float64)
		}
		for _, ps := range playerSkills {
			if ps.SkillID != "" {
				c.state.SkillNextLevelXP[ps.SkillID] = ps.NextLevelXP
			}
		}
	}

	// skills: map skill_id -> { xp_per_level: [...], max_level, name, id }
	var skillDefs map[string]serverapi.SkillDefinition
	if unmarshalPayloadKey(payload, "skills", &skillDefs) {
		if c.state.SkillDefinitions == nil {
			c.state.SkillDefinitions = make(map[string]SkillDefinition)
		}
		for skillID, extDef := range skillDefs {
			// Ensure ID is set (may not be in the JSON key)
			if extDef.ID == "" {
				extDef.ID = skillID
			}
			c.state.SkillDefinitions[skillID] = SkillDefinitionFromAPI(extDef)
		}
	}
}

// parseChatHistoryData extracts chat messages from a get_chat_history response
// and stores them in state.LastChatHistory for compound actions to consume.
func (c *Client) parseChatHistoryData(payload map[string]any) {
	var resp serverapi.ChatHistoryResponse
	if !unmarshalPayloadKey(payload, "messages", &resp.Messages) {
		return
	}
	if ch, ok := payload["channel"].(string); ok {
		resp.Channel = ch
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.state.LastChatHistory = make([]ChatMessage, len(resp.Messages))
	for i, m := range resp.Messages {
		c.state.LastChatHistory[i] = ChatMessage{
			ID:        m.ID,
			Channel:   resp.Channel,
			SenderID:  m.SenderID,
			Sender:    m.Sender,
			Content:   m.Content,
			TargetID:  m.TargetID,
			Timestamp: m.TimestampUTC,
		}
	}
}

// parseShipData extracts ship information from payload using serverapi types.
func (c *Client) parseShipData(payload map[string]any) {
	var ext serverapi.Ship
	if unmarshalPayloadKey(payload, "ship", &ext) {
		ship := ShipFromAPI(ext)

		c.mu.Lock()
		// Build legacy cargo format from structured cargo
		legacyCargo := make([]map[string]any, len(ship.Cargo))
		for i, item := range ship.Cargo {
			legacyCargo[i] = map[string]any{
				"item_id":  item.ItemID,
				"quantity": item.Quantity,
			}
		}

		c.state.Ship = ship
		c.state.Hull = ship.Hull
		c.state.MaxHull = ship.MaxHull
		c.state.Fuel = ship.Fuel
		c.state.MaxFuel = ship.MaxFuel
		c.state.MaxCargo = int(ship.CargoCapacity)
		c.state.Cargo = legacyCargo
		c.mu.Unlock()
	}

	// Parse module definitions from payload level (from get_ship response)
	var moduleDefs []serverapi.ModuleDefinition
	if unmarshalPayloadKey(payload, "modules", &moduleDefs) {
		c.mu.Lock()
		if c.state.ModuleDefinitions == nil {
			c.state.ModuleDefinitions = make(map[string]ModuleDefinition)
		}
		for _, extDef := range moduleDefs {
			if extDef.ID != "" {
				c.state.ModuleDefinitions[extDef.ID] = ModuleDefinitionFromAPI(extDef)
			}
		}
		c.mu.Unlock()
	}
}

// parseSystemData extracts system information from payload using serverapi types.
func (c *Client) parseSystemData(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Parse transit status from get_system response (if present)
	// These fields indicate if the ship is currently in transit between systems
	if inTransit, ok := payload["in_transit"].(bool); ok {
		c.state.Traveling = inTransit
		if inTransit {
			c.debugLogger.Printf("Ship is in transit (from=%s to=%s type=%s ticks=%d)",
				payload["from_system"], payload["to_system"],
				payload["transit_type"], payload["ticks_remaining"])
		} else {
			c.debugLogger.Printf("Ship is not in transit")
		}
	}

	// Check for direct system object
	if _, ok := payload["system"]; ok {
		var ext serverapi.SystemData
		if unmarshalPayloadKey(payload, "system", &ext) {
			c.debugLogger.Printf("Parsing system data from 'system' object")
			c.mergeSystemDataLocked(ext)
		}
	} else {
		// System fields might be at top level of payload (e.g., in logged_in response)
		_, hasID := payload["id"]
		_, hasName := payload["name"]
		_, hasPOIs := payload["pois"]
		if hasID || (hasName && hasPOIs) {
			c.debugLogger.Printf("Parsing system data from top-level payload (hasID=%v, hasName=%v, hasPOIs=%v)", hasID, hasName, hasPOIs)
			jsonBytes, err := json.Marshal(payload)
			if err == nil {
				var ext serverapi.SystemData
				if json.Unmarshal(jsonBytes, &ext) == nil {
					c.mergeSystemDataLocked(ext)
				}
			}
		}
	}

	// Check for top-level POIs array (outside of system object)
	var pois []serverapi.POI
	if unmarshalPayloadKey(payload, "pois", &pois) {
		c.state.System.POIs = make([]POI, len(pois))
		for i, p := range pois {
			c.state.System.POIs[i] = POIFromAPI(p)
		}
		c.state.LastMapUpdate = time.Now()
	}
}

// parsePOIData extracts a single POI from get_poi responses and updates the
// matching entry in state.System.POIs so that detailed fields (resources, etc.)
// are available to callers that read the state after the response.
func (c *Client) parsePOIData(payload map[string]any) {
	var ext serverapi.POI
	if !unmarshalPayloadKey(payload, "poi", &ext) || ext.ID == "" {
		return
	}

	poi := POIFromAPI(ext)

	c.mu.Lock()
	defer c.mu.Unlock()

	for i, existing := range c.state.System.POIs {
		if existing.ID == poi.ID {
			c.state.System.POIs[i] = poi
			return
		}
	}
	// POI not in the system list yet — append it
	c.state.System.POIs = append(c.state.System.POIs, poi)
}

// parseMapData extracts map information from get_map response
func (c *Client) parseMapData(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// get_map returns: {"systems": [...], "total_count": N}
	systemsData, ok := payload["systems"].([]any)
	if !ok {
		return
	}

	// Find the current system in the map and update its connections.
	// Fall back to CurrentSystem if System.ID hasn't been populated yet.
	currentSystemID := c.state.System.ID
	if currentSystemID == "" {
		currentSystemID = c.state.CurrentSystem
	}
	for _, s := range systemsData {
		systemMap, ok := s.(map[string]any)
		if !ok {
			continue
		}

		// Get system_id (get_map uses system_id, not id)
		systemID, ok := systemMap["system_id"].(string)
		if !ok {
			// Try alternate field name
			if id, ok := systemMap["id"].(string); ok {
				systemID = id
			} else {
				continue
			}
		}

		// If this is the current system, update its connections
		if strings.EqualFold(systemID, currentSystemID) {
			if connections, ok := systemMap["connections"].([]any); ok {
				c.state.System.Connections = c.state.System.Connections[:0]
				for _, conn := range connections {
					if connStr, ok := conn.(string); ok {
						// get_map returns bare system IDs
						connInfo := ConnectionInfo{
							SystemID: connStr,
							Name:     connStr,
							Distance: 0,
						}
						c.state.System.Connections = append(c.state.System.Connections, connInfo)
					}
				}
				c.debugLogger.Printf("Updated %d connections from get_map", len(c.state.System.Connections))
			}
			break
		}
	}
}

// mergeSystemDataLocked merges a serverapi SystemData into internal state (assumes state.Mu is already locked).
func (c *Client) mergeSystemDataLocked(ext serverapi.SystemData) {
	sys := SystemDataFromAPI(ext)

	if sys.ID != "" {
		c.state.System.ID = sys.ID
		c.debugLogger.Printf("Set System.ID = '%s' from id field", sys.ID)
	}
	if sys.Name != "" {
		c.state.System.Name = sys.Name
		c.state.CurrentSystem = sys.Name
		// If ID is empty, use the name as the ID (server v0.93.0+ sends empty id fields)
		if c.state.System.ID == "" {
			c.state.System.ID = sys.Name
			c.debugLogger.Printf("Set System.ID = '%s' (fallback from name field)", sys.Name)
		}
		c.debugLogger.Printf("Set System.Name = '%s', CurrentSystem = '%s'", sys.Name, sys.Name)
	}
	if sys.Description != "" {
		c.state.System.Description = sys.Description
	}
	if sys.Empire != "" {
		c.state.System.Empire = sys.Empire
	}
	c.state.System.PoliceLevel = sys.PoliceLevel
	c.state.System.SecurityStatus = sys.SecurityStatus
	c.state.System.IsStronghold = sys.IsStronghold
	c.state.System.Discovered = sys.Discovered
	if sys.DiscoveredBy != "" {
		c.state.System.DiscoveredBy = sys.DiscoveredBy
	}
	c.state.System.Position = sys.Position

	if len(sys.Connections) > 0 {
		c.state.System.Connections = sys.Connections
	}
	if len(sys.POIs) > 0 {
		c.state.System.POIs = sys.POIs
		c.state.LastMapUpdate = time.Now()
	}
	// ShipPOI is an internal-only field, preserved across merges
}

// parseErrorState extracts state changes from error messages
func (c *Client) parseErrorState(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Store the last error for diagnostics
	c.lastErrorMu.Lock()
	c.lastError = make(map[string]any)
	for k, v := range payload {
		c.lastError[k] = v
	}
	c.lastErrorMu.Unlock()

	// Handle IP rate limit block
	if code, ok := payload["code"].(string); ok && code == "ip_timed_out" {
		if msg, ok := payload["message"].(string); ok {
			seconds := parseIPBlockSeconds(msg)
			jitter := time.Duration(rand.IntN(int(SleepIPRateLimitJitter.Seconds()))) * time.Second
			blockDuration := time.Duration(seconds)*time.Second + jitter
			c.ipBlockedMu.Lock()
			c.ipBlockedUntil = time.Now().Add(blockDuration)
			c.ipBlockedMu.Unlock()
			c.debugLogger.Printf("IP rate limited for %d seconds + %v jitter = %v total pause", seconds, jitter, blockDuration)
		}
	}

	if errMsg, ok := payload["message"].(string); ok {
		if containsIgnoreCase(errMsg, []string{"already undocked", "not docked", "ship is not docked"}) {
			c.state.Doc = false
		}
		if containsIgnoreCase(errMsg, []string{"already docked", "already at station"}) {
			c.state.Doc = true
		}
	}
}

// ipBlockSecondsRe matches "in N seconds" in IP rate limit messages.
var ipBlockSecondsRe = regexp.MustCompile(`in (\d+) seconds`)

// parseIPBlockSeconds extracts the number of seconds from an IP rate limit message.
func parseIPBlockSeconds(msg string) int {
	if m := ipBlockSecondsRe.FindStringSubmatch(msg); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 300 // default to 5 minutes if parsing fails
}

// parseErrorAction extracts state changes from action_error messages
func (c *Client) parseErrorAction(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Store the last error for diagnostics
	c.lastErrorMu.Lock()
	c.lastError = make(map[string]any)
	for k, v := range payload {
		c.lastError[k] = v
	}
	c.lastErrorMu.Unlock()

	// Handle specific error codes
	if code, ok := payload["code"].(string); ok {
		switch code {
		case "not_docked":
			c.state.Doc = false
		case "already_docked":
			c.state.Doc = true
		}
	}

	// Also check message for backward compatibility
	if errMsg, ok := payload["message"].(string); ok {
		if containsIgnoreCase(errMsg, []string{"not docked", "ship is not docked"}) {
			c.state.Doc = false
		}
		if containsIgnoreCase(errMsg, []string{"already docked", "already at station"}) {
			c.state.Doc = true
		}
	}
}

// parseTravelAction extracts travel state from action responses
func (c *Client) parseTravelAction(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if action, ok := payload["action"].(string); ok {
		switch action {
		case "undock":
			c.state.Doc = false
		case "dock":
			c.state.Doc = true
		case "travel", "jump":
			// Travel initiated, will get progress in state_update
			c.state.Traveling = true
		case "jumped":
			// Jump completed — update current system and location
			c.state.Traveling = false
			c.state.TravelProgress = nil
			c.state.Doc = false
			if sysID, ok := payload["system_id"].(string); ok {
				c.state.System.ID = sysID
				c.state.CurrentSystem = sysID
			}
			if sysName, ok := payload["system"].(string); ok {
				c.state.System.Name = sysName
				c.state.CurrentSystem = sysName
			}
			if poi, ok := payload["poi"].(string); ok {
				c.state.CurrentPOI = poi
				c.state.System.ShipPOI = poi
			}
			c.debugLogger.Printf("Jump complete: now in %s (%s)", c.state.System.Name, c.state.System.ID)
		case "arrived":
			// Travel completed within a system
			c.state.Traveling = false
			c.state.TravelProgress = nil
			c.state.Doc = false
			if poi, ok := payload["poi"].(string); ok {
				c.state.CurrentPOI = poi
				c.state.System.ShipPOI = poi
			}
			if poiID, ok := payload["poi_id"].(string); ok {
				c.state.CurrentPOI = poiID
				c.state.System.ShipPOI = poiID
			}
			c.debugLogger.Printf("Arrived at %s", c.state.CurrentPOI)
		}
	}
}

// parseTravelProgress extracts travel progress from state_update
func (c *Client) parseTravelProgress(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if progress, ok := payload["travel_progress"].(float64); ok {
		if c.state.TravelProgress == nil {
			c.state.TravelProgress = &TravelProgress{}
		}
		c.state.TravelProgress.Progress = progress
		c.state.Traveling = true

		if destination, ok := payload["travel_destination"].(string); ok {
			c.state.TravelProgress.Destination = destination
		}
		if travelType, ok := payload["travel_type"].(string); ok {
			c.state.TravelProgress.Type = travelType
		}
		if arrivalTick, ok := payload["travel_arrival_tick"].(float64); ok {
			c.state.TravelProgress.ArrivalTick = int64(arrivalTick)
		}
	} else {
		// No travel progress means we're not traveling
		c.state.Traveling = false
		c.state.TravelProgress = nil
	}
}

// parseNearbyPlayers extracts nearby player list from state_update using serverapi types.
func (c *Client) parseNearbyPlayers(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if inCombat, ok := payload["in_combat"].(bool); ok {
		c.state.InCombat = inCombat
	}

	var extNearby []serverapi.NearbyPlayer
	if unmarshalPayloadKey(payload, "nearby", &extNearby) {
		c.state.Nearby = make([]NearbyPlayer, len(extNearby))
		for i, n := range extNearby {
			c.state.Nearby[i] = NearbyPlayerFromAPI(n)
		}
	}
}

// parseActionResult handles action_result messages from the server.
// These arrive after a pending action completes on the next tick.
// The payload has {command: "...", result: {...}, tick: N}.
func (c *Client) parseActionResult(payload map[string]any) {
	result, ok := payload["result"].(map[string]any)
	if !ok {
		return
	}

	action, _ := result["action"].(string)
	command, _ := payload["command"].(string)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update tick
	if tick, ok := payload["tick"].(float64); ok {
		c.state.CurrentTick = int64(tick)
	}

	switch action {
	case "arrived":
		c.state.Traveling = false
		c.state.TravelProgress = nil
		c.state.Doc = false
		if poiID, ok := result["poi_id"].(string); ok {
			c.state.CurrentPOI = poiID
			c.state.System.ShipPOI = poiID
		} else if poi, ok := result["poi"].(string); ok {
			c.state.CurrentPOI = poi
			c.state.System.ShipPOI = poi
		}
		// Jump arrival may include a new system
		if command == "jump" {
			if sysID, ok := result["system_id"].(string); ok {
				c.state.System.ID = sysID
				c.state.CurrentSystem = sysID
			}
			if sysName, ok := result["system"].(string); ok {
				c.state.System.Name = sysName
				c.state.CurrentSystem = sysName
			}
		}
		c.debugLogger.Printf("Action result: arrived at %s", c.state.CurrentPOI)

	case "dock":
		c.state.Doc = true
		c.state.Traveling = false
		c.state.TravelProgress = nil
		if story, ok := result["story"].(string); ok {
			c.state.LastDockStory = story
		}
		c.debugLogger.Printf("Action result: docked")

	case "undock":
		c.state.Doc = false
		c.debugLogger.Printf("Action result: undocked")

	case "refuel":
		if fuel, ok := result["fuel"].(float64); ok {
			c.state.Fuel = fuel
			c.state.Ship.Fuel = fuel
		}
		if fuelNow, ok := result["fuel_now"].(float64); ok {
			c.state.Fuel = fuelNow
			c.state.Ship.Fuel = fuelNow
		}
		c.debugLogger.Printf("Action result: refueled to %.0f", c.state.Fuel)

	case "repair":
		if hull, ok := result["hull_now"].(float64); ok {
			c.state.Hull = hull
			c.state.Ship.Hull = hull
		}
		c.debugLogger.Printf("Action result: repaired to %.0f", c.state.Hull)

	case "deposit_items":
		if cargoSpace, ok := result["cargo_space"].(float64); ok {
			c.state.Ship.CargoCapacity = cargoSpace
		}
		c.debugLogger.Printf("Action result: deposited items")

	case "craft":
		outputID, _ := result["output_id"].(string)
		outputName, _ := result["output_name"].(string)
		count, _ := result["quantity"].(float64)
		recipeID, _ := result["recipe_id"].(string)

		// Remove consumed inputs from cargo
		if consumed, ok := result["from_storage"].([]any); ok {
			for _, raw := range consumed {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				itemID, _ := item["item_id"].(string)
				qty, _ := item["quantity"].(float64)
				if itemID == "" || qty <= 0 {
					continue
				}
				for i := range c.state.Ship.Cargo {
					if c.state.Ship.Cargo[i].ItemID == itemID {
						c.state.Ship.Cargo[i].Quantity -= qty
						break
					}
				}
			}
			// Remove zero/negative quantity entries
			filtered := c.state.Ship.Cargo[:0]
			for _, item := range c.state.Ship.Cargo {
				if item.Quantity > 0 {
					filtered = append(filtered, item)
				}
			}
			c.state.Ship.Cargo = filtered
		}

		// Add crafted output to cargo
		if outputID != "" && count > 0 {
			found := false
			for i := range c.state.Ship.Cargo {
				if c.state.Ship.Cargo[i].ItemID == outputID {
					c.state.Ship.Cargo[i].Quantity += count
					found = true
					break
				}
			}
			if !found {
				c.state.Ship.Cargo = append(c.state.Ship.Cargo, CargoItem{
					ItemID:   outputID,
					Quantity: count,
				})
			}
		}

		// Recalculate cargo used
		var cargoUsed float64
		for _, item := range c.state.Ship.Cargo {
			cargoUsed += item.Quantity
		}
		c.state.Ship.CargoUsed = cargoUsed

		if outputName != "" {
			c.debugLogger.Printf("Action result: crafted %.0f x %s (recipe: %s)", count, outputName, recipeID)
		} else {
			c.debugLogger.Printf("Action result: crafted %.0f x %s (recipe: %s)", count, outputID, recipeID)
		}

	case "create_sell_order":
		// Bulk sell order result — remove successfully listed items from cargo
		if results, ok := result["results"].([]any); ok {
			successCount := 0
			failCount := 0
			for _, r := range results {
				entry, ok := r.(map[string]any)
				if !ok {
					continue
				}
				if errMsg, _ := entry["error"].(string); errMsg != "" {
					failCount++
					continue
				}
				successCount++
				itemID, _ := entry["item_id"].(string)
				qty, _ := entry["quantity"].(float64)
				if itemID == "" || qty <= 0 {
					continue
				}
				for i := range c.state.Ship.Cargo {
					if c.state.Ship.Cargo[i].ItemID == itemID {
						c.state.Ship.Cargo[i].Quantity -= qty
						break
					}
				}
			}
			// Remove zero/negative quantity entries
			filtered := c.state.Ship.Cargo[:0]
			for _, item := range c.state.Ship.Cargo {
				if item.Quantity > 0 {
					filtered = append(filtered, item)
				}
			}
			c.state.Ship.Cargo = filtered

			// Recalculate cargo used
			var cargoUsed float64
			for _, item := range c.state.Ship.Cargo {
				cargoUsed += item.Quantity
			}
			c.state.Ship.CargoUsed = cargoUsed

			c.debugLogger.Printf("Action result: create_sell_order (%d listed, %d failed)", successCount, failCount)
		}

	case "create_buy_order":
		if results, ok := result["results"].([]any); ok {
			successCount := 0
			failCount := 0
			for _, r := range results {
				entry, ok := r.(map[string]any)
				if !ok {
					continue
				}
				if errMsg, _ := entry["error"].(string); errMsg != "" {
					failCount++
					continue
				}
				successCount++
			}
			c.debugLogger.Printf("Action result: create_buy_order (%d created, %d failed)", successCount, failCount)
		} else {
			c.debugLogger.Printf("Action result: create_buy_order (single)")
		}

	default:
		c.debugLogger.Printf("Action result: %s (unhandled)", action)
	}
}

// containsIgnoreCase checks if any of the substrings exist in the text (case-insensitive)
func containsIgnoreCase(text string, substrings []string) bool {
	lower := text
	for _, s := range substrings {
		if len(lower) >= len(s) {
			// Simple case-insensitive check
			for i := 0; i <= len(lower)-len(s); i++ {
				match := true
				for j := 0; j < len(s); j++ {
					c1 := lower[i+j]
					c2 := s[j]
					if c1 >= 'A' && c1 <= 'Z' {
						c1 += 32
					}
					if c2 >= 'A' && c2 <= 'Z' {
						c2 += 32
					}
					if c1 != c2 {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	return false
}

// Close closes the connection and cleans up all resources
func (c *Client) Close() error {
	// Grab the cancel func under the lock to avoid racing with Reconnect
	// which replaces goroutineCancel.
	c.mu.Lock()
	cancel := c.goroutineCancel
	c.mu.Unlock()

	// Signal all goroutines to stop
	cancel()

	// Close the stop channel to signal legacy code
	select {
	case <-c.stopCh:
		// Already closed
	default:
		close(c.stopCh)
	}

	// Stop health monitoring
	select {
	case c.stopPing <- struct{}{}:
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Close(websocket.StatusNormalClosure, "")
		c.connected = false
		c.conn = nil
		// Wait for goroutines outside the lock
		_ = err
	}

	// Wait for goroutines to exit (with timeout)
	done := make(chan struct{})
	go func() {
		c.goroutineWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.debugLogger.Printf("All goroutines exited cleanly on Close")
	case <-time.After(5 * time.Second):
		c.debugLogger.Printf("Warning: Timeout waiting for goroutines to exit on Close")
	}

	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Ready returns a channel that is closed when the WebSocket connection is ready
// (i.e., when the first message has been received from the server)
func (c *Client) Ready() <-chan struct{} {
	return c.readyChan
}

// GetMarketListings returns the most recently fetched market listings
func (c *Client) GetMarketListings() []MarketListing {
	c.listingsMu.RLock()
	defer c.listingsMu.RUnlock()

	result := make([]MarketListing, len(c.latestListings))
	copy(result, c.latestListings)
	return result
}

// storeRawJSON stores raw JSON payloads for key response types
func (c *Client) storeRawJSON(resp protocol.Response) {
	// Always cache the last response for interactive tools like play_as
	if jsonData, err := json.Marshal(resp.Payload); err == nil {
		c.rawJSONMu.Lock()
		c.latestRawJSON["_last"] = jsonData
		c.rawJSONMu.Unlock()
	}

	// Only store specific response types that are useful for data collection
	var storeKey string
	var shouldStore bool

	// Track additional keys to also store the payload under.
	// This handles cases where the server response contains an "action" field
	// that indicates the original request type, allowing lookup by both
	// content-based key and action-based key.
	var extraKeys []string

	switch resp.Type {
	case protocol.TypeOK:
		// Use the "action" field from the payload to derive a canonical storage key.
		// The server includes "action" in responses like get_system and get_poi,
		// which helps disambiguate when payload keys overlap across response types.
		if action, ok := resp.Payload["action"].(string); ok {
			switch action {
			case "get_system":
				storeKey = "system"
				shouldStore = true
			case "get_poi":
				storeKey = "poi"
				shouldStore = true
			case "get_status":
				storeKey = "status"
				shouldStore = true
			case "get_ship":
				storeKey = "ship"
				shouldStore = true
			case "get_base":
				storeKey = "base"
				shouldStore = true
			}
		}

		// Fall through to content-based detection for responses without "action"
		// or to add extra storage keys for responses that contain nested data.

		// Store full status response (Player, Ship, System, POI, etc.)
		if storeKey == "" {
			if _, hasPlayer := resp.Payload["player"]; hasPlayer {
				storeKey = "status"
				shouldStore = true
			} else if _, hasUsername := resp.Payload["username"]; hasUsername {
				storeKey = "status"
				shouldStore = true
			}
		}
		// Store ship response
		if _, hasShip := resp.Payload["ship"]; hasShip && storeKey == "" {
			storeKey = "ship"
			shouldStore = true
		}
		// Store POI response
		if _, hasPOI := resp.Payload["poi"]; hasPOI {
			if storeKey == "" {
				storeKey = "poi"
				shouldStore = true
			} else if storeKey != "poi" {
				// Also store under "poi" when present but primary key is something else
				extraKeys = append(extraKeys, "poi")
			}
		}
		if _, hasListings := resp.Payload["listings"]; hasListings {
			if storeKey == "" {
				storeKey = "listings"
			}
			shouldStore = true
		}
		// Store storage data (from view_storage response)
		// base_id is the reliable indicator — items and ships may be omitted when empty
		if _, hasBaseID := resp.Payload["base_id"]; hasBaseID {
			if storeKey == "" {
				storeKey = "storage"
			}
			shouldStore = true

			// Fire storage update callback
			c.onStorageMu.RLock()
			cb := c.onStorageUpdate
			c.onStorageMu.RUnlock()
			if cb != nil {
				c.fireStorageCallback(cb, resp)
			}
		}
		// Store catalog responses (ships, skills, recipes, items)
		// Check for catalog-specific fields before generic "items" check
		if _, hasPage := resp.Payload["page"]; hasPage {
			if _, hasItems := resp.Payload["items"]; hasItems {
				if storeKey == "" {
					storeKey = "catalog"
				}
				shouldStore = true
			}
		}
		// Store market listings with "items" field (but not from catalog)
		if _, hasItems := resp.Payload["items"]; hasItems {
			if storeKey == "" {
				storeKey = "market"
			}
			shouldStore = true
		}
		if _, hasShips := resp.Payload["ships"]; hasShips {
			if storeKey == "" {
				storeKey = "ships"
			}
			shouldStore = true
		}
		// Store shipyard data (from shipyard_showroom response)
		if _, hasShipyard := resp.Payload["shipyard"]; hasShipyard {
			if storeKey == "" {
				storeKey = "shipyard"
			}
			shouldStore = true
		}
		// Only store as "system" if it has pois (full get_system response)
		// Jump responses also have "system" field but lack pois/position/police_level
		if _, hasPOIs := resp.Payload["pois"]; hasPOIs && storeKey == "" {
			storeKey = "system"
			shouldStore = true
		}
		// Store recipes
		if _, hasRecipes := resp.Payload["recipes"]; hasRecipes {
			if storeKey == "" {
				storeKey = "recipes"
			}
			shouldStore = true
		}
		// Store notifications
		if _, hasNotifications := resp.Payload["notifications"]; hasNotifications {
			if storeKey == "" {
				storeKey = "notifications"
			}
			shouldStore = true
		}
		// Store wrecks
		if _, hasWrecks := resp.Payload["wrecks"]; hasWrecks {
			if storeKey == "" {
				storeKey = "wrecks"
			}
			shouldStore = true
		}
		// Store drones
		if _, hasDrones := resp.Payload["drones"]; hasDrones {
			if storeKey == "" {
				storeKey = "drones"
			}
			shouldStore = true
		}
		// Store base info
		if _, hasBase := resp.Payload["base"]; hasBase {
			if storeKey == "" {
				storeKey = "base"
			} else if storeKey != "base" {
				extraKeys = append(extraKeys, "base")
			}
			shouldStore = true
		}
		// Store faction info
		// Faction data is returned directly in payload with fields like is_member, leader_id, etc.
		if storeKey == "" {
			if _, hasFaction := resp.Payload["faction"]; hasFaction {
				storeKey = "faction_info"
				shouldStore = true
			} else if _, hasIsMember := resp.Payload["is_member"]; hasIsMember {
				storeKey = "faction_info"
				shouldStore = true
			} else if _, hasLeaderID := resp.Payload["leader_id"]; hasLeaderID {
				storeKey = "faction_info"
				shouldStore = true
			}
		}
		// Store captain's log (can be "captains_log" or "entry")
		if _, hasCaptainsLog := resp.Payload["captains_log"]; hasCaptainsLog {
			if storeKey == "" {
				storeKey = "captains_log_list"
			}
			shouldStore = true
		}
		if _, hasEntry := resp.Payload["entry"]; hasEntry {
			if storeKey == "" {
				storeKey = "captains_log_list"
			}
			shouldStore = true
		}
		// Store player skills (from get_skills response)
		if _, hasPlayerSkills := resp.Payload["player_skills"]; hasPlayerSkills {
			if storeKey == "" {
				storeKey = "skills"
			}
			shouldStore = true
		}
		// Store nearby players (from get_nearby response)
		if _, hasNearby := resp.Payload["nearby"]; hasNearby {
			if storeKey == "" {
				storeKey = "nearby"
			}
			shouldStore = true
		}
		// Store map data (from get_map response)
		if _, hasSystems := resp.Payload["systems"]; hasSystems {
			if storeKey == "" {
				storeKey = "systems"
			}
			shouldStore = true
		}
		// Store cargo data (from get_cargo response)
		// Content-based detection: has "cargo" array and "capacity" field
		if _, hasCargoItems := resp.Payload["cargo"]; hasCargoItems {
			if _, hasCapacity := resp.Payload["capacity"]; hasCapacity {
				if storeKey == "" {
					storeKey = "cargo"
				}
				shouldStore = true
			}
		}
		// Store missions data (from get_missions response)
		// Content-based detection: has "missions" array and "base_id" field
		if _, hasMissions := resp.Payload["missions"]; hasMissions {
			if _, hasBaseID := resp.Payload["base_id"]; hasBaseID {
				if storeKey == "" {
					storeKey = "missions"
				}
				shouldStore = true
			}
		}
		// Store active missions data (from get_active_missions response)
		// Content-based detection: has "total_count" and "max_missions" fields
		if _, hasTotalCount := resp.Payload["total_count"]; hasTotalCount {
			if _, hasMaxMissions := resp.Payload["max_missions"]; hasMaxMissions {
				if storeKey == "" {
					storeKey = "active_missions"
				}
				shouldStore = true
			}
		}
		// Store notes data (from get_notes response)
		// Content-based detection: has "notes" field and "total_count"
		if _, hasNotes := resp.Payload["notes"]; hasNotes {
			if _, hasTotalCount := resp.Payload["total_count"]; hasTotalCount {
				if storeKey == "" {
					storeKey = "notes"
				}
				shouldStore = true
			}
		}
		// Store insurance quote data (from get_insurance_quote response)
		// Content-based detection: has "quote" or "insured_value" field
		if _, hasQuote := resp.Payload["quote"]; hasQuote {
			if storeKey == "" {
				storeKey = "insurance_quote"
			}
			shouldStore = true
		}
		if _, hasInsuredValue := resp.Payload["insured_value"]; hasInsuredValue {
			if storeKey == "" {
				storeKey = "insurance_quote"
			}
			shouldStore = true
		}
		// Store version data (from get_version response)
		// Content-based detection: has "version" field
		if _, hasVersion := resp.Payload["version"]; hasVersion {
			if storeKey == "" {
				storeKey = "version"
			}
			shouldStore = true
		}
		// Store commands data (from get_commands response)
		// Content-based detection: has "commands" array
		if _, hasCommands := resp.Payload["commands"]; hasCommands {
			if storeKey == "" {
				storeKey = "commands"
			}
			shouldStore = true
		}
		// Action-based detection for cargo
		if action, ok := resp.Payload["action"].(string); ok {
			switch action {
			case "get_cargo":
				if storeKey == "" {
					storeKey = "cargo"
				}
				shouldStore = true
			case "get_battle_status":
				if storeKey == "" {
					storeKey = "battle_status"
				}
				shouldStore = true
			case "view_orders":
				// Override "base" storeKey for view_orders
				storeKey = "orders"
				shouldStore = true
			case "view_market":
				if storeKey == "" {
					storeKey = "market"
				}
				shouldStore = true
			case "get_missions":
				if storeKey == "" {
					storeKey = "missions"
				}
				shouldStore = true
			case "get_active_missions":
				if storeKey == "" {
					storeKey = "active_missions"
				}
				shouldStore = true
			case "view_storage":
				if storeKey == "" {
					storeKey = "storage"
				}
				shouldStore = true
			case "list_ships":
				if storeKey == "" {
					storeKey = "owned_ships"
				}
				shouldStore = true
			case "get_notes":
				if storeKey == "" {
					storeKey = "notes"
				}
				shouldStore = true
			case "get_insurance_quote":
				if storeKey == "" {
					storeKey = "insurance_quote"
				}
				shouldStore = true
			case "get_version":
				if storeKey == "" {
					storeKey = "version"
				}
				shouldStore = true
			case "get_commands":
				if storeKey == "" {
					storeKey = "commands"
				}
				shouldStore = true
			}
		}
	case protocol.TypeError:
		// Don't store error responses in the same keys as success data
		// Errors are tracked in lastError field instead
		return
	}

	if shouldStore {
		c.rawJSONMu.Lock()
		defer c.rawJSONMu.Unlock()

		// Marshal the entire response to JSON
		jsonData, err := json.Marshal(resp.Payload)
		if err != nil {
			c.debugLogger.Printf("Failed to marshal raw JSON for %s: %v", storeKey, err)
			return
		}

		c.latestRawJSON[storeKey] = jsonData
		c.debugLogger.Printf("Stored raw JSON for %s (%d bytes)", storeKey, len(jsonData))

		// Also store under extra keys for cross-referenced data
		for _, key := range extraKeys {
			c.latestRawJSON[key] = jsonData
			c.debugLogger.Printf("Stored raw JSON for %s (extra key, %d bytes)", key, len(jsonData))
		}
	}
}

// GetRawJSON retrieves the raw JSON payload for a given key
func (c *Client) GetRawJSON(key string) []byte {
	c.rawJSONMu.RLock()
	defer c.rawJSONMu.RUnlock()

	if data, ok := c.latestRawJSON[key]; ok {
		// Return a copy to prevent external modification
		result := make([]byte, len(data))
		copy(result, data)
		return result
	}
	return nil
}

// GetLastError returns the most recent error response
func (c *Client) GetLastError() map[string]any {
	c.lastErrorMu.RLock()
	defer c.lastErrorMu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]any)
	for k, v := range c.lastError {
		result[k] = v
	}
	return result
}

// ClearLastError clears the stored error
func (c *Client) ClearLastError() {
	c.lastErrorMu.Lock()
	defer c.lastErrorMu.Unlock()
	c.lastError = make(map[string]any)
}

// parseListingsData extracts market listings from a listings response using serverapi types.
func (c *Client) parseListingsData(payload map[string]any) {
	var extListings []serverapi.MarketListing
	if !unmarshalPayloadKey(payload, "listings", &extListings) {
		return
	}

	c.listingsMu.Lock()
	c.latestListings = make([]MarketListing, 0, len(extListings))
	for _, ext := range extListings {
		listing := MarketListingFromAPI(ext)

		// Infer item_type from item_id if not provided by server
		if listing.ItemType == "" && listing.ItemID != "" {
			listing.ItemType = inferItemType(listing.ItemID)
		}

		// Default to "sell" type for NPC listings
		if listing.Type == "" {
			listing.Type = "sell"
		}

		c.latestListings = append(c.latestListings, listing)
	}
	c.listingsMu.Unlock()

	c.debugLogger.Printf("Parsed %d market listings", len(c.latestListings))
}

// parseViewMarketData extracts market data from a view_market response.
// The view_market response sends aggregated order book data under the "items" key,
// with a different shape than get_listings. This converts it to MarketListing
// format for compatibility with existing market code.
func (c *Client) parseViewMarketData(payload map[string]any) {
	var items []serverapi.ViewMarketItem
	if !unmarshalPayloadKey(payload, "items", &items) {
		return
	}

	// Debug: Log first few items to understand the data structure
	if len(items) > 0 {
		c.debugLogger.Printf("view_market: First item: item_id=%s, best_buy=%.2f, best_sell=%.2f, buy_orders=%d, sell_orders=%d",
			items[0].ItemID, items[0].BestBuy, items[0].BestSell,
			len(items[0].BuyOrders), len(items[0].SellOrders))
	}

	c.listingsMu.Lock()
	c.latestListings = make([]MarketListing, 0, len(items)*2)
	itemsWithOrders := 0
	for _, item := range items {
		// Create a synthetic sell listing from the best sell price
		if item.BestSell > 0 {
			listing := MarketListing{
				ItemID:       item.ItemID,
				ItemType:     inferItemType(item.ItemID),
				PricePerUnit: item.BestSell,
				Type:         "sell",
			}
			// Use total quantity from sell orders
			for _, order := range item.SellOrders {
				listing.Quantity += order.Quantity
			}
			c.latestListings = append(c.latestListings, listing)
			itemsWithOrders++
		}
		// Create a synthetic buy listing from the best buy price
		if item.BestBuy > 0 {
			listing := MarketListing{
				ItemID:       item.ItemID,
				ItemType:     inferItemType(item.ItemID),
				PricePerUnit: item.BestBuy,
				Type:         "buy",
			}
			// Use total quantity from buy orders
			for _, order := range item.BuyOrders {
				listing.Quantity += order.Quantity
			}
			c.latestListings = append(c.latestListings, listing)
			if item.BestSell == 0 {
				itemsWithOrders++
			}
		}
	}
	c.listingsMu.Unlock()

	c.debugLogger.Printf("Parsed %d market items into %d listings (from view_market), %d items had orders",
		len(items), len(c.latestListings), itemsWithOrders)
}

// inferItemType infers the item type from an item ID prefix.
func inferItemType(itemID string) string {
	prefixTypes := []struct {
		prefix   string
		itemType string
	}{
		{"ore_", "ore"},
		{"weapon_", "weapon"},
		{"mining_", "module"},
		{"shield_", "shield"},
		{"cargo_", "cargo"},
	}
	for _, pt := range prefixTypes {
		if len(itemID) >= len(pt.prefix) && itemID[:len(pt.prefix)] == pt.prefix {
			return pt.itemType
		}
	}
	return "unknown"
}

// parseShipsData extracts ship listings from a get_ships response
func (c *Client) parseShipsData(payload map[string]any) {
	c.shipsMu.Lock()
	defer c.shipsMu.Unlock()

	// Store the entire ships payload
	// The response typically contains ships data which can be an array or map
	if ships, ok := payload["ships"]; ok {
		c.latestShips = make(map[string]any)
		c.latestShips["ships"] = ships
		c.debugLogger.Printf("Parsed ship listings data")
	}
}

// waitForResponse waits for a response of a specific type with a timeout
func (c *Client) waitForResponse(ctx context.Context, messageType string, timeout time.Duration) (protocol.Response, error) {
	respChan := make(chan protocol.Response, 1)

	c.waiterMu.Lock()
	c.waiters[messageType] = respChan
	c.waiterMu.Unlock()

	defer func() {
		c.waiterMu.Lock()
		delete(c.waiters, messageType)
		c.waiterMu.Unlock()
	}()

	select {
	case resp := <-respChan:
		return resp, nil
	case <-time.After(timeout):
		return protocol.Response{}, fmt.Errorf("timeout waiting for %s response", messageType)
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}

// waitForAuthResponse waits for either a success response or an error response
// This is used for authentication operations that can return either success or error
func (c *Client) waitForAuthResponse(ctx context.Context, successType string, timeout time.Duration) (protocol.Response, error) {
	successChan := make(chan protocol.Response, 1)
	errorChan := make(chan protocol.Response, 1)

	c.waiterMu.Lock()
	c.waiters[successType] = successChan
	c.waiters[protocol.TypeError] = errorChan
	c.waiterMu.Unlock()

	defer func() {
		c.waiterMu.Lock()
		delete(c.waiters, successType)
		delete(c.waiters, protocol.TypeError)
		c.waiterMu.Unlock()
	}()

	select {
	case resp := <-successChan:
		return resp, nil
	case resp := <-errorChan:
		if msg, ok := resp.Payload["message"].(string); ok {
			return resp, fmt.Errorf("%s", msg)
		}
		return resp, fmt.Errorf("operation failed")
	case <-time.After(timeout):
		return protocol.Response{}, fmt.Errorf("timeout waiting for %s response", successType)
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}

// waitForActionResponse waits for either "ok" or "error" response for game actions
func (c *Client) waitForActionResponse(ctx context.Context, timeout time.Duration) error {
	okChan := make(chan protocol.Response, 1)
	errorChan := make(chan protocol.Response, 1)
	actionErrorChan := make(chan protocol.Response, 1)
	actionResultChan := make(chan protocol.Response, 1)
	miningYieldChan := make(chan protocol.Response, 1)
	scanResultChan := make(chan protocol.Response, 1)

	c.waiterMu.Lock()
	c.waiters[protocol.TypeOK] = okChan
	c.waiters[protocol.TypeError] = errorChan
	c.waiters[protocol.TypeActionError] = actionErrorChan
	c.waiters[protocol.TypeActionResult] = actionResultChan
	c.waiters[protocol.TypeMiningYield] = miningYieldChan
	c.waiters[protocol.TypeScanResult] = scanResultChan
	c.waiterMu.Unlock()

	defer func() {
		c.waiterMu.Lock()
		delete(c.waiters, protocol.TypeOK)
		delete(c.waiters, protocol.TypeError)
		delete(c.waiters, protocol.TypeActionError)
		delete(c.waiters, protocol.TypeActionResult)
		delete(c.waiters, protocol.TypeMiningYield)
		delete(c.waiters, protocol.TypeScanResult)
		c.waiterMu.Unlock()
	}()

	deadline := time.After(timeout)

	for {
		select {
		case <-miningYieldChan:
			// mining_yield is the completion signal for pending mine actions
			return nil
		case <-scanResultChan:
			// scan_result is the completion signal for pending scan actions
			return nil
		case resp := <-okChan:
			// Check if this is a pending action response
			if pending, ok := resp.Payload["pending"].(bool); ok && pending {
				pendingCmd, _ := resp.Payload["command"].(string)
				c.debugLogger.Printf("Action pending (%s) - waiting for completion", pendingCmd)
				// Reset deadline: give a full timeout window for the action to complete
				deadline = time.After(timeout)
				continue
			}
			// Check if this is a jump/travel in-progress response (not yet arrived)
			if action, _ := resp.Payload["action"].(string); action == "jump" || action == "travel" {
				if _, hasArrival := resp.Payload["arrival_tick"]; hasArrival {
					c.debugLogger.Printf("Action in progress (%s) - waiting for arrival", action)
					deadline = time.After(timeout)
					continue
				}
			}
			// Not pending, this is the actual completion
			return nil
		case resp := <-actionErrorChan:
			// action_error is the server's response for pending actions that failed
			// on the next tick. Handle it the same as error responses.
			errorChan <- resp
			continue
		case resp := <-errorChan:
			// Check error code and categorize response
			if code, ok := resp.Payload["code"].(string); ok {
				switch code {
				// BENIGN: Goal already achieved (treat as success)
				case "already_there":
					c.debugLogger.Printf("Already at destination (success)")
					return nil
				case "already_docked":
					c.debugLogger.Printf("Already docked (success)")
					return nil
				case "not_docked":
					// When trying to undock but already undocked
					c.debugLogger.Printf("Already undocked (success)")
					return nil

				// ACTION_PENDING: Another action is in-flight, wait for it to complete
				case "action_pending":
					pendingCmd, _ := resp.Payload["pending_command"].(string)
					c.debugLogger.Printf("Action pending (%s) - waiting for completion", pendingCmd)
					// Reset deadline and continue listening for the real response
					deadline = time.After(timeout)
					continue

				// INFORMATIONAL: Agent should adapt strategy but not fail
				case "already_traveling", "already_jumping":
					// Already in transit - wait for arrival
					c.debugLogger.Printf("Already in transit: %s", code)
					return fmt.Errorf("already in transit - wait for arrival")

				case "docked":
					// Must undock first before this action
					c.debugLogger.Printf("Must undock before this action")
					return fmt.Errorf("must undock first - currently docked at station")

				case "no_fuel":
					c.debugLogger.Printf("Insufficient fuel for action")
					return fmt.Errorf("insufficient fuel - dock at station to refuel")

				case "no_credits":
					c.debugLogger.Printf("Insufficient credits")
					return fmt.Errorf("insufficient credits - need to earn money first")

				case "no_cargo_space":
					c.debugLogger.Printf("Cargo hold full")
					return fmt.Errorf("cargo hold full - dock at station to sell items")

				case "missing_materials":
					c.debugLogger.Printf("Missing crafting materials")
					return fmt.Errorf("missing required materials for crafting")

				case "cannot_craft":
					msg, _ := resp.Payload["message"].(string)
					if msg == "" {
						msg = "cannot craft this recipe"
					}
					c.debugLogger.Printf("Cannot craft: %s", msg)
					return fmt.Errorf("%s", msg)

				case "no_cloak", "no_crafting_service":
					c.debugLogger.Printf("Missing equipment/service: %s", code)
					msg := resp.Payload["message"].(string)
					return fmt.Errorf("%s", msg)

				// ACTUAL ERRORS: Invalid attempts
				case "rate_limited":
					// This shouldn't happen with proper timing, but handle it
					waitTime := "unknown"
					if wait, ok := resp.Payload["wait_seconds"].(float64); ok {
						waitTime = fmt.Sprintf("%.1fs", wait)
					}
					c.debugLogger.Printf("Rate limited - wait %s", waitTime)
					return fmt.Errorf("rate limited - wait %s before next action", waitTime)

				default:
					// All other error codes - log and return as error
					c.debugLogger.Printf("Action failed with code: %s", code)
				}
			}

			// Extract error message from payload
			if msg, ok := resp.Payload["message"].(string); ok {
				return fmt.Errorf("%s", msg)
			}
			if code, ok := resp.Payload["code"].(string); ok {
				return fmt.Errorf("error: %s", code)
			}
			return fmt.Errorf("action failed")
		case resp := <-actionResultChan:
			// action_result arrives after the server processes a pending action.
			// parseActionResult already updated state; check for errors in results.
			if result, ok := resp.Payload["result"].(map[string]any); ok {
				if results, ok := result["results"].([]any); ok {
					// Bulk result — check if any items had errors
					var errors []string
					for _, r := range results {
						if entry, ok := r.(map[string]any); ok {
							if errMsg, ok := entry["error"].(string); ok && errMsg != "" {
								errors = append(errors, errMsg)
							}
						}
					}
					if len(errors) > 0 {
						return fmt.Errorf("%d item(s) failed: %s", len(errors), errors[0])
					}
				}
			}
			return nil
		case <-deadline:
			return fmt.Errorf("timeout waiting for action response")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// waitForInitialResponse waits for the first OK or error response from the server.
// Unlike waitForActionResponse, it does NOT loop on pending/in-progress — it returns
// the first response and lets the caller decide what to do.
func (c *Client) waitForInitialResponse(ctx context.Context, timeout time.Duration) (protocol.Response, error) {
	okChan := make(chan protocol.Response, 1)
	errorChan := make(chan protocol.Response, 1)
	actionErrorChan := make(chan protocol.Response, 1)
	actionResultChan := make(chan protocol.Response, 1)

	c.waiterMu.Lock()
	c.waiters[protocol.TypeOK] = okChan
	c.waiters[protocol.TypeError] = errorChan
	c.waiters[protocol.TypeActionError] = actionErrorChan
	c.waiters[protocol.TypeActionResult] = actionResultChan
	c.waiterMu.Unlock()

	defer func() {
		c.waiterMu.Lock()
		delete(c.waiters, protocol.TypeOK)
		delete(c.waiters, protocol.TypeError)
		delete(c.waiters, protocol.TypeActionError)
		delete(c.waiters, protocol.TypeActionResult)
		c.waiterMu.Unlock()
	}()

	deadline := time.After(timeout)

	for {
		select {
		case resp := <-okChan:
			// If pending, keep waiting for the real initial response.
			if pending, ok := resp.Payload["pending"].(bool); ok && pending {
				c.debugLogger.Printf("Action pending — waiting for server to start")
				deadline = time.After(timeout)
				continue
			}
			return resp, nil

		case resp := <-actionResultChan:
			// action_result arrives when the server processes a pending action
			// on the next tick. Treat it as the initial response.
			c.debugLogger.Printf("Received action_result as initial response")
			return resp, nil

		case resp := <-errorChan:
			if code, ok := resp.Payload["code"].(string); ok {
				switch code {
				case "already_there", "already_docked", "not_docked":
					return resp, nil // Benign — caller handles these
				case "action_pending":
					pendingCmd, _ := resp.Payload["pending_command"].(string)
					return resp, fmt.Errorf("action pending: another action (%s) is in progress", pendingCmd)
				}
			}
			msg, _ := resp.Payload["message"].(string)
			if msg == "" {
				msg = "server error"
			}
			return resp, fmt.Errorf("%s", msg)

		case resp := <-actionErrorChan:
			msg, _ := resp.Payload["message"].(string)
			if msg == "" {
				msg = "action error"
			}
			return resp, fmt.Errorf("%s", msg)

		case <-deadline:
			return protocol.Response{}, fmt.Errorf("timeout waiting for initial response")

		case <-ctx.Done():
			return protocol.Response{}, ctx.Err()
		}
	}
}

// waitForStateChange polls the client state until check returns true.
// It returns nil on success, or an error on timeout/context cancellation.
func (c *Client) waitForStateChange(ctx context.Context, check func(*State) bool, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for state change after %v", timeout)
		case <-ticker.C:
			if check(c.GetState()) {
				return nil
			}
		}
	}
}

// monitorConnectionHealth monitors the WebSocket connection health
// and attempts to reconnect if no messages are received within the timeout.
// The server handles ping/pong keepalive automatically - we only monitor for dead connections.
func (c *Client) monitorConnectionHealth(ctx context.Context) {
	goroutineID := atomic.AddInt64(&c.goroutineID, 1)
	c.debugLogger.Printf("[health-%d] Health monitor started | Timeout: %v (server handles ping/pong)", goroutineID, c.pongTimeout)
	defer c.debugLogger.Printf("[health-%d] Health monitor exited", goroutineID)

	healthTicker := time.NewTicker(c.pingInterval)
	defer healthTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.debugLogger.Printf("[health-%d] Context cancelled, exiting", goroutineID)
			return
		case <-c.stopPing:
			c.debugLogger.Printf("[health-%d] Stop signal received, exiting", goroutineID)
			return
		case <-healthTicker.C:
			if !c.IsConnected() {
				continue
			}

			// Check if we've received a message recently
			c.lastMessageMu.RLock()
			lastMsg := c.lastMessageTime
			c.lastMessageMu.RUnlock()

			timeSinceLastMsg := time.Since(lastMsg)
			if timeSinceLastMsg > c.pongTimeout {
				c.debugLogger.Printf("[health-%d] No messages received for %v (timeout: %v), connection may be dead", goroutineID, timeSinceLastMsg, c.pongTimeout)
				c.logConnectionMetrics("health_timeout")
				// Trigger a reconnection by notifying the handler
				c.mu.RLock()
				handler := c.handler
				c.mu.RUnlock()

				if handler != nil {
					handler.OnDisconnected(fmt.Errorf("connection timeout - no messages for %v", timeSinceLastMsg))
				}
			}
		}
	}
}

// updateLastMessageTime updates the last message time for health monitoring
func (c *Client) updateLastMessageTime() {
	c.lastMessageMu.Lock()
	defer c.lastMessageMu.Unlock()
	c.lastMessageTime = time.Now()
}

// logConnectionMetrics logs diagnostic information about the connection
func (c *Client) logConnectionMetrics(event string) {
	c.diagnosticMu.RLock()
	defer c.diagnosticMu.RUnlock()

	c.lastMessageMu.RLock()
	lastMsg := c.lastMessageTime
	c.lastMessageMu.RUnlock()

	duration := time.Since(c.connectTime)
	sent := atomic.LoadInt64(&c.messagesSent)
	received := atomic.LoadInt64(&c.messagesReceived)

	c.debugLogger.Printf("=== Connection Metrics [%s] ===", event)
	c.debugLogger.Printf("  Connection ID: %s", c.connectionID)
	c.debugLogger.Printf("  Uptime: %v", duration.Round(time.Millisecond))
	c.debugLogger.Printf("  Messages sent: %d | received: %d | total: %d", sent, received, sent+received)
	c.debugLogger.Printf("  Last server message: %v ago", time.Since(lastMsg).Round(time.Millisecond))
	if !c.lastSendTime.IsZero() {
		c.debugLogger.Printf("  Last client send: %v ago", time.Since(c.lastSendTime).Round(time.Millisecond))
	}
	if !c.lastReceiveTime.IsZero() {
		c.debugLogger.Printf("  Last client receive: %v ago", time.Since(c.lastReceiveTime).Round(time.Millisecond))
	}
}

// trackMessageSent records a sent message for diagnostics
func (c *Client) trackMessageSent() {
	atomic.AddInt64(&c.messagesSent, 1)
	c.diagnosticMu.Lock()
	c.lastSendTime = time.Now()
	c.diagnosticMu.Unlock()
}

// trackMessageReceived records a received message for diagnostics
func (c *Client) trackMessageReceived() {
	atomic.AddInt64(&c.messagesReceived, 1)
	c.diagnosticMu.Lock()
	c.lastReceiveTime = time.Now()
	c.diagnosticMu.Unlock()
}

// generateConnectionID creates a unique connection ID
func generateConnectionID() string {
	return fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405"), time.Now().UnixNano()%1000)
}

// WaitForReady waits for the connection to be ready (first message received)
func (c *Client) WaitForReady(ctx context.Context, timeout time.Duration) error {
	select {
	case <-c.Ready():
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for connection to be ready")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// EnsureConnected ensures the client is connected, reconnecting if necessary
func (c *Client) EnsureConnected(ctx context.Context) error {
	if !c.IsConnected() {
		c.debugLogger.Printf("Not connected, attempting to reconnect...")
		if err := c.Reconnect(ctx); err != nil {
			return fmt.Errorf("failed to reconnect: %w", err)
		}
		// Wait for the connection to be ready
		if err := c.WaitForReady(ctx, 10*time.Second); err != nil {
			return fmt.Errorf("connection not ready after reconnect: %w", err)
		}
	}
	return nil
}

// SendQueued sends a command using the queue system for reliable sequential execution
// This is the recommended way to send commands for agents that need guaranteed delivery
// and proper response matching.
func (c *Client) SendQueued(ctx context.Context, msg protocol.Message, timeout time.Duration) (protocol.Response, error) {
	if c.CmdQueue == nil {
		return protocol.Response{}, fmt.Errorf("command queue not initialized")
	}

	// Start the queue if not already running
	c.CmdQueue.Start(ctx)

	// Enqueue the command and wait for response
	return c.CmdQueue.Enqueue(ctx, msg, timeout)
}

// ===== QUEUED COMMAND METHODS =====
// These methods use the command queue for reliable sequential execution

// DockQueued docks at a station using the queue
func (c *Client) DockQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "dock",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// UndockQueued undocks from a station using the queue
func (c *Client) UndockQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "undock",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// TravelQueued travels to a POI using the queue
func (c *Client) TravelQueued(ctx context.Context, targetPOI string) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "travel",
		Payload:   map[string]any{"target_poi": targetPOI},
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// JumpQueued jumps to another system using the queue
func (c *Client) JumpQueued(ctx context.Context, targetSystem string) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "jump",
		Payload:   map[string]any{"target_system": targetSystem},
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// MineQueued mines resources using the queue
func (c *Client) MineQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "mine",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// RefuelQueued refuels using the queue
func (c *Client) RefuelQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "refuel",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// RepairQueued repairs using the queue
func (c *Client) RepairQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "repair",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// SellQueued sells items using the queue
func (c *Client) SellQueued(ctx context.Context, itemID string, quantity float64) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "sell",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// BuyQueued buys items using the queue
func (c *Client) BuyQueued(ctx context.Context, itemID string, quantity float64) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "buy",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// GetSystemQueued gets system info using the queue
func (c *Client) GetSystemQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "get_system",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// GetStatusQueued gets status using the queue
func (c *Client) GetStatusQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "get_status",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// GetPOIQueued gets POI info using the queue
func (c *Client) GetPOIQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "get_poi",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// GetListingsQueued gets market listings using the queue
func (c *Client) GetListingsQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "view_market",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// CraftQueued crafts an item using the queue
func (c *Client) CraftQueued(ctx context.Context, recipeID string, quantity int) error {
	payload := map[string]any{"recipe_id": recipeID}
	if quantity > 1 {
		payload["quantity"] = quantity
	}
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "craft",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// GetCargoQueued gets cargo contents using the queue
func (c *Client) GetCargoQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "get_cargo",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// GetBaseQueued gets base info using the queue
func (c *Client) GetBaseQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "get_base",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// GetShipQueued gets ship info using the queue
func (c *Client) GetShipQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "get_ship",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// GetNearbyQueued gets nearby players using the queue
func (c *Client) GetNearbyQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "get_nearby",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// ViewStorageQueued views station storage using the queue
func (c *Client) ViewStorageQueued(ctx context.Context) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "view_storage",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// WithdrawItemsQueued withdraws items from storage using the queue
func (c *Client) WithdrawItemsQueued(ctx context.Context, itemID string, quantity float64) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "withdraw_items",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// DepositCreditsQueued deposits credits to storage using the queue
func (c *Client) DepositCreditsQueued(ctx context.Context, amount float64) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "deposit_credits",
		Payload:   map[string]any{"amount": amount},
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// AcceptMissionQueued accepts a mission using the queue
func (c *Client) AcceptMissionQueued(ctx context.Context, missionID string) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "accept_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// CompleteMissionQueued completes a mission using the queue
func (c *Client) CompleteMissionQueued(ctx context.Context, missionID string) error {
	_, err := c.SendQueued(ctx, protocol.Message{
		Type:      "complete_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
	return err
}

// fireStorageCallback parses a view_storage response and invokes the storage update callback.
func (c *Client) fireStorageCallback(cb func(StorageUpdateEvent), resp protocol.Response) {
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		return
	}
	var storageResp serverapi.ViewStorageResponse
	if err := json.Unmarshal(raw, &storageResp); err != nil {
		return
	}
	cb(StorageUpdateEvent{
		BaseID:  storageResp.BaseID,
		Credits: storageResp.Credits,
		Items:   storageResp.Items,
		Ships:   storageResp.Ships,
	})
}
