package game

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"math/rand/v2"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/calllog"
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

	// debugPayloadMaxLen caps the "Response Payload:" debug-log line. 0
	// means no cap (log the whole payload). Default 200, tuned to keep the
	// log readable for short responses.
	debugPayloadMaxLen int

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

	router      *responseRouter
	inflight    *inflight
	actionLocks *actionLockMap
	mutationMu  sync.Mutex

	// pendingReplay holds messages staged for re-send after a reconnect
	// triggered by close=1000. Populated by replayPending and drained
	// by Reconnect after a successful login.
	pendingReplay   []protocol.Message
	pendingReplayMu sync.Mutex

	// tickProvider, if set, returns the current game tick. Used by Travel /
	// Jump to capture an authoritative StartTick — state.CurrentTick only
	// advances on certain server frames and can lag the real tick by tens of
	// ticks on idle sessions, which silently corrupts the "ticks elapsed"
	// estimator. Set via SetTickProvider; nil-safe at every read site.
	tickProvider func() int64

	sendOverride func(ctx context.Context, msg protocol.Message) error // Test hook

	// Storage update callback — fired when a view_storage response is received
	onStorageUpdate func(resp StorageUpdateEvent)
	onStorageMu     sync.RWMutex

	// Chat message callback — fired when a real-time chat push event is received
	onChatMessage func(msg serverapi.ChatMessage)
	onChatMu      sync.RWMutex

	// Structured call logger for request/response pairs
	CallLogger      *calllog.Logger
	lastSentMsg     json.RawMessage // most recent message sent via Send(), for pairing with response
	lastSentMsgType string          // message type of lastSentMsg
	lastSentMsgMu   sync.Mutex

	// XP observation tracking — fires whenever skill XP changes in state
	XPCallback     XPCallbackFunc
	xpLastSkills   map[string]Skill   // last known skill state
	xpLastXP       map[string]float64 // last known SkillXP
	xpLastAction   string             // most recent action sent
	xpLastTarget   string             // most recent action target
	xpLastQuantity int                // most recent action quantity (default 1)
	xpMu           sync.Mutex
}

// XPCallbackFunc is the signature fired after a successful action that
// (potentially) affected player skill XP. It receives the before/after
// skill and SkillXP maps so observers can compute deltas.
type XPCallbackFunc func(action, target string, quantity int, before, after map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64)

// XPCallbackSetter is implemented by game clients that support XP
// observation callbacks. Both the WebSocket *Client and *MCPGameClient
// satisfy this interface, so observers can wire in regardless of transport.
type XPCallbackSetter interface {
	SetXPCallback(fn XPCallbackFunc)
}

// SetXPCallback installs an XP observation callback. Passing nil disables
// callbacks. Safe to call before or after Connect.
func (c *Client) SetXPCallback(fn XPCallbackFunc) {
	c.XPCallback = fn
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
	reconnecting atomic.Bool    // Prevents multiple concurrent reconnections
	wg           sync.WaitGroup // Track reconnection goroutine lifecycle

	// Session-contention detection. Counts consecutive reconnects whose
	// prior connection died in under SessionContentionMinUptime. Cleared
	// whenever a connection survives long enough. See onSessionContention.
	shortLivedCount     int
	onSessionContention func(consecutiveShortLived int, lastUptime time.Duration)
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

// SetOnSessionContention overrides the default behavior (process exit with
// code 2) that fires when SessionContentionMaxShortLived consecutive
// reconnects die in under SessionContentionMinUptime — the signature of
// another client fighting us for the same credentials. Tests set this to
// capture the event without killing the test process.
func (r *ReconnectingHandler) SetOnSessionContention(fn func(consecutiveShortLived int, lastUptime time.Duration)) {
	r.onSessionContention = fn
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
	// Snapshot how long the dying connection lived BEFORE Reconnect resets
	// connectTime. Used to decide whether we're in a session-contention loop.
	uptime := r.client.Uptime()

	if r.handler != nil {
		r.handler.OnDisconnected(err)
	}

	// Only start reconnection if not already reconnecting
	if !r.reconnecting.CompareAndSwap(false, true) {
		return
	}

	if uptime > 0 && uptime < SessionContentionMinUptime {
		r.shortLivedCount++
	} else {
		r.shortLivedCount = 0
	}

	if r.shortLivedCount >= SessionContentionMaxShortLived {
		r.reconnecting.Store(false) // not actually reconnecting
		r.logger.Printf("Session contention detected: %d consecutive connects died in under %v (last uptime %v).",
			r.shortLivedCount, SessionContentionMinUptime, uptime.Round(time.Millisecond))
		r.logger.Printf("Another client is likely logged in with the same credentials.")
		r.logger.Printf("Stop other clients, or run one of them with --transport=mcp.")
		if r.onSessionContention != nil {
			r.onSessionContention(r.shortLivedCount, uptime)
			return
		}
		os.Exit(2)
	}

	r.wg.Add(1)
	go r.attemptReconnection()
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
		stopCh:             make(chan struct{}),
		readyChan:          make(chan struct{}),
		waiters:            make(map[string]chan protocol.Response),
		router:             newResponseRouter(),
		inflight:           newInflight(16),
		actionLocks:        newActionLockMap(),
		debugLogger:        debugLogger,
		debugPayloadMaxLen: 200,
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

// SetOnChatMessage registers a callback that fires when a real-time chat push
// event is received from the server. This allows packages like mbox to capture
// chat messages without polling.
func (c *Client) SetOnChatMessage(fn func(msg serverapi.ChatMessage)) {
	c.onChatMu.Lock()
	defer c.onChatMu.Unlock()
	c.onChatMessage = fn
}

// SetDebugLogging controls whether the game client logs WebSocket messages.
// When disabled, the debug logger output is discarded.
func (c *Client) SetDebugLogging(enabled bool) {
	if !enabled {
		c.debugLogger.SetOutput(io.Discard)
	}
}

// SetTickProvider installs a function returning the authoritative current
// game tick. When set, Travel/Jump use it for StartTick instead of the
// possibly-stale state.CurrentTick. Pass nil to clear.
func (c *Client) SetTickProvider(fn func() int64) {
	c.mu.Lock()
	c.tickProvider = fn
	c.mu.Unlock()
}

// currentTick returns the best available current tick: the injected
// tickProvider when set, otherwise state.CurrentTick. Safe to call without
// holding c.mu.
func (c *Client) currentTick() int64 {
	c.mu.RLock()
	fn := c.tickProvider
	c.mu.RUnlock()
	if fn != nil {
		return fn()
	}
	return c.GetState().CurrentTick
}

// SetDebugPayloadMaxLen configures how much of each received response's
// payload is emitted on the "Response Payload:" debug line. Pass 0 to
// disable truncation (log the full payload regardless of length). Negative
// values are treated as 0. The default is 200.
func (c *Client) SetDebugPayloadMaxLen(n int) {
	if n < 0 {
		n = 0
	}
	c.debugPayloadMaxLen = n
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

	// Start active ping loop to keep the connection alive and detect death promptly
	c.goroutineWg.Add(1)
	go func() {
		defer c.goroutineWg.Done()
		c.sendPingLoop(c.goroutineCtx)
	}()

	return nil
}

// Disconnect closes the WebSocket connection
func (c *Client) Disconnect() error {
	// Do the stateful work under the mutex, then release before waiting on
	// goroutines. Holding the mutex during goroutineWg.Wait() deadlocks with
	// any goroutine (e.g. listen) that needs c.mu to clean up on its way out;
	// the wait then times out after 10s, eating ~10s per reconnect.
	c.mu.Lock()
	c.logConnectionMetrics("client_disconnect")
	c.goroutineCancel()
	select {
	case c.stopPing <- struct{}{}:
	default:
	}
	conn := c.conn
	c.conn = nil
	c.connected = false
	c.mu.Unlock()

	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "client disconnect")
		c.debugLogger.Printf("Disconnected from server")
	}

	done := make(chan struct{})
	go func() {
		c.goroutineWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.debugLogger.Printf("All goroutines exited cleanly")
	case <-time.After(2 * time.Second):
		c.debugLogger.Printf("Warning: goroutines slow to exit after 2s; continuing")
	}

	return nil
}

// handleClose runs the close-code policy: replay outstanding mutations
// on graceful closes, fail-fast otherwise. Called by the listen loop
// after a close frame is observed.
func (c *Client) handleClose(closeErr *websocket.CloseError) {
	policy, known := lookupClosePolicy(closeErr.Code, closeErr.Reason)
	if !known {
		log.Printf("WARN: unknown WebSocket close: code=%d reason=%q — please document; default action=%v",
			int(closeErr.Code), closeErr.Reason, policy.action)
	} else {
		c.debugLogger.Printf("close policy: %s (code=%d reason=%q)", policy.note, int(closeErr.Code), closeErr.Reason)
	}

	switch policy.action {
	case closeReplay:
		c.replayPending(closeErr)
	default: // closeFailFast
		c.failPending(&ConnectionClosed{Code: closeErr.Code, Reason: closeErr.Reason})
	}
}

// failPending delivers err to every outstanding id-keyed subscription
// and unregisters them. Used on fail-fast closes.
func (c *Client) failPending(err error) {
	subs := c.router.snapshotByID()
	for _, sub := range subs {
		c.router.dispatch(protocol.Response{
			Type:      protocol.TypeError,
			RequestID: sub.id,
			Payload:   map[string]any{"code": "connection_closed", "message": err.Error()},
		})
	}
}

// replayPending stages every outstanding mutation for re-send under a
// fresh UUIDv7. Caller has already torn down the connection; the
// staged messages are drained by Reconnect after a successful login.
//
// Per spec: server v0.296.1 graceful closes lose all in-flight state,
// so no double-execution risk. The caller's handle.Result() never
// observes the close.
func (c *Client) replayPending(closeErr *websocket.CloseError) {
	subs := c.router.snapshotByID()
	if len(subs) == 0 {
		return
	}
	c.debugLogger.Printf("replay: staging %d pending mutation(s) under fresh request_ids (close code=%d reason=%q)",
		len(subs), int(closeErr.Code), closeErr.Reason)

	c.pendingReplayMu.Lock()
	defer c.pendingReplayMu.Unlock()

	for _, sub := range subs {
		if sub.replayMsg == nil {
			// No retained message (subscription created outside Submit).
			// Fail it instead.
			c.router.dispatch(protocol.Response{
				Type:      protocol.TypeError,
				RequestID: sub.id,
				Payload:   map[string]any{"code": "connection_closed", "message": "no replay payload"},
			})
			continue
		}
		newID := uuid.Must(uuid.NewV7()).String()
		msg := *sub.replayMsg
		msg.RequestID = newID
		c.router.rekey(sub, newID)
		if sub.handle != nil {
			sub.handle.setID(newID)
		}
		c.pendingReplay = append(c.pendingReplay, msg)
	}
}

// drainPendingReplay sends every staged replay message. Called by
// Reconnect after a successful login. Send failures are surfaced as
// synthetic connection_lost errors so callers see SOMETHING instead
// of hanging forever.
func (c *Client) drainPendingReplay(ctx context.Context) {
	c.pendingReplayMu.Lock()
	pending := c.pendingReplay
	c.pendingReplay = nil
	c.pendingReplayMu.Unlock()

	if len(pending) == 0 {
		return
	}
	c.debugLogger.Printf("replay: draining %d staged message(s)", len(pending))

	for _, msg := range pending {
		if err := c.send(ctx, msg); err != nil {
			c.debugLogger.Printf("replay: send failed for new id=%s: %v", msg.RequestID, err)
			c.router.dispatch(protocol.Response{
				Type:      protocol.TypeError,
				RequestID: msg.RequestID,
				Payload:   map[string]any{"code": "connection_lost", "message": err.Error()},
			})
		}
	}
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

		// Drain any messages staged by replayPending during the prior close.
		c.drainPendingReplay(ctx)

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

// send is the private wire primitive used by Submit. Currently a thin
// wrapper around the (deprecated) public Send so Submit doesn't have
// to depend on the deprecated method directly. Task 21 will invert
// this: Send becomes private and this wrapper goes away.
func (c *Client) send(ctx context.Context, msg protocol.Message) error {
	return c.Send(ctx, msg)
}

// Send sends a message to the game server.
//
// Deprecated: prefer execQuery / execMutation / subscribePush. Send is the
// low-level fire-and-forget wire primitive; direct callers lose response
// correlation. New code must use the response-router primitives.
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

	// Track the latest action for XP observation attribution
	if c.XPCallback != nil {
		c.xpMu.Lock()
		c.xpLastAction = msg.Type
		c.xpLastTarget = extractActionTarget(msg)
		c.xpLastQuantity = extractQuantityFromArgs(msg.Payload)
		c.xpMu.Unlock()
	}

	// Store the sent message for call logging (paired with response later)
	if c.CallLogger != nil {
		c.lastSentMsgMu.Lock()
		c.lastSentMsg = json.RawMessage(data)
		c.lastSentMsgType = msg.Type
		c.lastSentMsgMu.Unlock()
	}

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

	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("failed to send login: %w", err)
	}
	resp, err := h.Result(ctx)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	if resp.Type != protocol.TypeLoggedIn {
		return fmt.Errorf("login: unexpected response type %q", resp.Type)
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
	if registrationCode != "" {
		payload["registration_code"] = registrationCode
	}

	msg := protocol.Message{
		Type:      "register",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}

	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("failed to send register: %w", err)
	}
	resp, err := h.Result(ctx)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	if resp.Type != protocol.TypeRegistered {
		return fmt.Errorf("register: unexpected response type %q", resp.Type)
	}
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
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnAction), WithTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("claim: submit: %w", err)
	}
	if _, err := h.Result(ctx); err != nil {
		return fmt.Errorf("claim failed: %w", err)
	}
	return nil
}

// Undock undocks from the current station.
//
// Terminates on either the legacy TypeUndocked push event (no command field)
// or a TypeActionResult whose top-level command is "undock" — newer servers
// only emit the latter on completion.
func (c *Client) Undock(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "undock",
		Timestamp: time.Now().UnixMilli(),
	}
	_, terminate := dockTransitionMatchers("undock", protocol.TypeUndocked)
	h, err := c.Submit(ctx, msg, WithTerminator(terminate), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// Dock docks at a station in the current system.
//
// Terminates on either the legacy TypeDocked push event (no command field)
// or a TypeActionResult whose top-level command is "dock" — newer servers
// only emit the latter on completion.
func (c *Client) Dock(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "dock",
		Timestamp: time.Now().UnixMilli(),
	}
	_, terminate := dockTransitionMatchers("dock", protocol.TypeDocked)
	h, err := c.Submit(ctx, msg, WithTerminator(terminate), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// dockTransitionMatchers builds the (Classifier, Terminator) pair for Dock
// and Undock. command is the wire command name ("dock" or "undock");
// legacyType is the corresponding standalone push type the older protocol
// emitted (TypeDocked / TypeUndocked).
//
// Three terminal frames are accepted, covering server protocol drift:
//   - TypeOK with action=command and no pending=true (the synchronous follow-up
//     after the OK-pending ack — what current servers send for dock/undock)
//   - TypeActionResult with command=command (queued-tick mutation form)
//   - the legacy TypeDocked / TypeUndocked push event
//
// TypeActionError / TypeError terminate with a server error.
func dockTransitionMatchers(command, legacyType string) (Classifier, Terminator) {
	isPending := func(p map[string]any) bool {
		v, _ := p["pending"].(bool)
		return v
	}
	classifier := func(resp protocol.Response) bool {
		switch resp.Type {
		case legacyType, protocol.TypeActionError, protocol.TypeError:
			return true
		case protocol.TypeActionResult:
			cmd, _ := resp.Payload["command"].(string)
			return cmd == command
		case protocol.TypeOK:
			act, _ := resp.Payload["action"].(string)
			return act == command && !isPending(resp.Payload)
		}
		return false
	}
	terminate := func(resp protocol.Response) (bool, error) {
		switch resp.Type {
		case legacyType:
			return true, nil
		case protocol.TypeActionError, protocol.TypeError:
			return true, serverErrorFromPayload(resp.Payload)
		case protocol.TypeActionResult:
			if cmd, _ := resp.Payload["command"].(string); cmd == command {
				return true, nil
			}
		case protocol.TypeOK:
			if act, _ := resp.Payload["action"].(string); act == command && !isPending(resp.Payload) {
				return true, nil
			}
		}
		return false, nil
	}
	return classifier, terminate
}

// sendAwaitingPending sends msg, waits for the initial server response, and
// — if the server replies action_pending because a previous travel/jump is
// still in flight — blocks until that pending action arrives, then re-sends
// once. Returns the post-retry response. Without this, a stale pending
// action causes every subsequent Travel/Jump in a tight loop to fail
// instantly with action_pending and burn through the per-session rate-limit
// budget within a second.
func (c *Client) sendAwaitingPending(ctx context.Context, msg protocol.Message, timeout time.Duration) (protocol.Response, error) {
	if err := c.Send(ctx, msg); err != nil {
		return protocol.Response{}, err
	}
	resp, err := c.waitForInitialResponse(ctx, timeout)
	if err == nil {
		return resp, nil
	}
	if resp.Type != protocol.TypeError {
		return resp, err
	}
	code, _ := resp.Payload["code"].(string)
	if code != "action_pending" {
		return resp, err
	}
	// Wait for the in-flight travel/jump to land. The cap matches Travel's
	// internal SleepTravelMaxWait — we won't wait longer for somebody else's
	// action than we'd wait for our own.
	waitErr := c.waitForStateChange(ctx, func(s *State) bool {
		return !s.Traveling
	}, SleepTravelMaxWait)
	if waitErr != nil {
		return resp, err // surface the original action_pending error
	}
	if sendErr := c.Send(ctx, msg); sendErr != nil {
		return protocol.Response{}, sendErr
	}
	return c.waitForInitialResponse(ctx, timeout)
}

// Travel travels to a POI within the current system
// Travel travels to a POI within the current system.
// It blocks until the ship arrives at the destination or an error occurs.
// The returned TravelResult contains the final POI the ship ended up at.
func (c *Client) Travel(ctx context.Context, targetPOI string) (*TravelResult, error) {
	msg := protocol.Message{
		Type:      "travel",
		Payload:   map[string]any{"target_poi": targetPOI},
		Timestamp: time.Now().UnixMilli(),
	}
	resp, err := c.sendAwaitingPending(ctx, msg, SleepActionStartTimeout)
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

	// Capture the start tick at the moment of the server ACK so callers can
	// diff actual vs. estimated duration after arrival. Prefer the injected
	// tickProvider over state.CurrentTick — the latter only advances when the
	// server happens to send a frame carrying tick/current_tick, so on idle
	// sessions it can lag the real tick by tens of ticks.
	startTick := c.currentTick()

	// Compute timeout from arrival_tick if available, else use generous default.
	timeout := 90 * time.Second
	var arrivalTick int64
	if at, ok := resp.Payload["arrival_tick"].(float64); ok {
		arrivalTick = int64(at)
		ticksRemaining := arrivalTick - startTick
		if ticksRemaining < 1 {
			ticksRemaining = 1
		}
		// Each tick ~10s, plus 30s buffer for safety.
		timeout = time.Duration(ticksRemaining)*SleepTick + 30*time.Second
	}
	// Cap the wait so a stale local CurrentTick (the lag between server
	// truth and our last received tick frame) can't inflate the timeout
	// past anything reasonable for within-system travel. waitForStateChange
	// returns immediately on arrival anyway — this is purely a safety bound.
	if timeout > SleepTravelMaxWait {
		timeout = SleepTravelMaxWait
	}

	c.debugLogger.Printf("Travel to %s: waiting up to %v for arrival", targetPOI, timeout)

	// Block until state.Traveling becomes false (arrival or interruption).
	if err := c.waitForStateChange(ctx, func(s *State) bool {
		return !s.Traveling
	}, timeout); err != nil {
		return &TravelResult{Canceled: true, ArrivalTick: arrivalTick, StartTick: startTick}, fmt.Errorf("travel to %s: %w", targetPOI, err)
	}

	state := c.GetState()
	return &TravelResult{
		POI:         state.CurrentPOI,
		Canceled:    false,
		ArrivalTick: arrivalTick,
		StartTick:   startTick,
	}, nil
}

// Jump jumps to another system
// Jump jumps to another system.
// It blocks until the ship arrives in the new system or an error occurs.
// The returned JumpResult contains the destination system info.
func (c *Client) Jump(ctx context.Context, targetSystem string) (*JumpResult, error) {
	msg := protocol.Message{
		Type:      "jump",
		Payload:   map[string]any{"target_system": targetSystem},
		Timestamp: time.Now().UnixMilli(),
	}
	resp, err := c.sendAwaitingPending(ctx, msg, SleepActionStartTimeout)
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
		currentTick := c.currentTick()
		ticksRemaining := int64(arrivalTick) - currentTick
		if ticksRemaining < 1 {
			ticksRemaining = 1
		}
		timeout = time.Duration(ticksRemaining)*SleepTick + 30*time.Second
	}
	// Cap so a stale local CurrentTick can't inflate the wait window.
	if timeout > SleepJumpMaxWait {
		timeout = SleepJumpMaxWait
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

// Mine mines resources at the current location.
//
// Wraps the execMutation error through maybeGoalReached so a cargo_full server
// error becomes a *GoalReachedError sentinel that the play_as loop recognises
// as a successful exit condition. Intermediate mining_yield push events have
// no "command" field so matchCommand("mine") correctly skips them.
func (c *Client) Mine(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "mine",
		Timestamp: time.Now().UnixMilli(),
	}
	// Newer servers deliver the mine result as a TypeMiningYield push event
	// (carries cargo + xp_gained) rather than an action_result with
	// command="mine". Accept either as terminal; classic action_error/error
	// continue to terminate with the server-supplied error.
	terminate := func(resp protocol.Response) (bool, error) {
		switch resp.Type {
		case protocol.TypeMiningYield:
			return true, nil
		case protocol.TypeActionResult:
			return true, nil
		case protocol.TypeActionError, protocol.TypeError:
			return true, serverErrorFromPayload(resp.Payload)
		}
		return false, nil
	}
	h, err := c.Submit(ctx, msg, WithTerminator(terminate), WithTimeout(SleepActionStartTimeout))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return maybeGoalReached("mine", err)
}

// Attack attacks a target player or NPC
func (c *Client) Attack(ctx context.Context, targetID string) error {
	msg := protocol.Message{
		Type:      "attack",
		Payload:   map[string]any{"target_id": targetID, "weapon_idx": 0},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// Scan scans the current area
func (c *Client) Scan(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "scan",
		Payload:   map[string]any{"target_id": "area"},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// SurveySystem scans for hidden POIs in the current system
// Requires a survey scanner module installed
func (c *Client) SurveySystem(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "survey_system",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// FindRoute finds a route to a target system using the server's pathfinding.
// Returns the route steps (excluding the current system) or an error.
func (c *Client) FindRoute(ctx context.Context, targetSystem string) ([]RouteStep, error) {
	msg := protocol.Message{
		Type:      "find_route",
		Payload:   map[string]any{"target_system": targetSystem},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepTick))
	if err != nil {
		return nil, err
	}
	resp, err := h.Result(ctx)
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
	msg := protocol.Message{
		Type:      "get_system",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_system returns type=ok with action="get_system"; storeRawJSON stores under "system".
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
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

	msg := protocol.Message{
		Type:      "get_map",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_map returns type=ok with "systems" key; storeRawJSON stores under "systems".
	// No action field in response.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
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
	msg := protocol.Message{
		Type:      "get_poi",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_poi returns type=ok with no "action" field on the wire (the
	// storeRawJSON action case is dead code for the current server).
	// The distinctive payload key is "poi" — the POI object itself.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// GetStatus requests player status.
// Blocks until the server responds.
func (c *Client) GetStatus(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_status",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_status returns type=ok with no "action" field on the wire (the
	// storeRawJSON action case is dead code for the current server).
	// The distinctive payload key is "player" — the full player snapshot.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// GetNotifications is a no-op for the WebSocket client.
//
// The WS server rejects get_notifications ("not needed over WebSocket") because
// tick, chat, trade, combat, and other notifications are delivered as push
// events in real time (see handleResponse / TypeTick). Callers that poll this
// method for tick freshness (e.g., GameClock.syncLoop, agent runner) can rely
// on the pushed state instead; we return nil so shared code paths keep working
// without logging spurious errors.
func (c *Client) GetNotifications(_ context.Context) error {
	return nil
}

// GetListings requests market listings for the current station.
// Blocks until the server responds.
func (c *Client) GetListings(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_market",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// GetShips requests ship listings from the current station.
// Blocks until the server responds.
func (c *Client) GetShips(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_ships",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_ships returns type=ok with "ships" array (distinct from browse_ships
	// which uses "listings"). station_id and station_name are also present.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
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
	msg := protocol.Message{
		Type:      "sell",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
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

	msg := protocol.Message{
		Type:      "create_sell_order",
		Payload:   map[string]any{"orders": orders},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
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

	msg := protocol.Message{
		Type:      "deposit_items",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
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

	// Refresh cargo from the server before iterating. Many responses update
	// state.Ship.Cargo as a side effect, but some flows (e.g. running
	// deposit_all right after login) leave the client-side cargo slice empty
	// even when the server's cargo is full. Ask explicitly and then read the
	// freshly-parsed state.
	//
	// GetCargo is fire-and-forget over the WebSocket, so Send returns before
	// the reply is processed into state. Without a wait here the outer loop
	// iterates stale cargo — we've seen this surface as "requested quantity
	// 12 exceeds available 8" when the pre-refresh quantity no longer
	// matches the server's.
	if err := c.GetCargo(ctx); err != nil {
		c.debugLogger.Printf("DepositAllItems: get_cargo refresh failed: %v", err)
		// Fall through and try with whatever state we have.
	} else {
		// GetCargo now blocks via execQuery until parseGetCargoData has
		// populated State, so the snapshot here reflects fresh server
		// cargo. The previous SleepQuick workaround is no longer needed.
		state = c.GetState()
	}

	if len(state.Ship.Cargo) == 0 {
		fmt.Printf("📦 Cargo is empty, nothing to deposit\n")
		return &GoalReachedError{
			Command: "deposit_all",
			Code:    "empty_cargo",
			Message: "Cargo is already empty",
		}
	}

	fmt.Printf("📥 Depositing %d cargo items to storage...\n", len(state.Ship.Cargo))

	// Deposit each item in cargo
	depositErrors := 0
	successfulDeposits := 0
	for i, item := range state.Ship.Cargo {
		if item.Quantity <= 0 {
			continue
		}

		// Check context before each deposit
		if err := ctx.Err(); err != nil {
			return err
		}

		// Wait before each deposit to avoid action_pending errors AND to let
		// any in-flight server responses (from the previous deposit or the
		// opening get_cargo) land in state before we snapshot it below.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(SleepQuick):
		}

		// Refresh state AFTER the wait so we pick up fresh quantities.
		// Sampling before the wait can see a stale snapshot where a
		// previously-deposited item still appears, or where an item we're
		// about to deposit has a different quantity than the server holds.
		currentState := c.GetState()

		// Find this item in the current cargo and check actual quantity
		var currentQty float64
		for _, cargoItem := range currentState.Ship.Cargo {
			if cargoItem.ItemID == item.ItemID {
				currentQty = cargoItem.Quantity
				break
			}
		}

		// Skip if item no longer exists in cargo or quantity is 0
		if currentQty <= 0 {
			fmt.Printf("   [%d/%d] ⊘ Skipping %s (no longer in cargo)\n", i+1, len(state.Ship.Cargo), item.ItemID)
			continue
		}

		// Deposit the current quantity (not the snapshot quantity)
		if err := c.DepositItems(ctx, item.ItemID, currentQty); err != nil {
			var se *ServerError
			if errors.As(err, &se) && se.Code == "empty_cargo" {
				fmt.Printf("📦 Cargo now empty after %d deposit(s)\n", successfulDeposits)
				return &GoalReachedError{
					Command: "deposit_all",
					Code:    "empty_cargo",
					Message: "Cargo is empty",
				}
			}
			fmt.Printf("   [%d/%d] ✗ Failed to deposit %.0f x %s: %v\n", i+1, len(state.Ship.Cargo), currentQty, item.ItemID, err)
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
			fmt.Printf("   [%d/%d] ✓ Deposited %.0f x %s to storage\n", i+1, len(state.Ship.Cargo), currentQty, item.ItemID)
			c.debugLogger.Printf("Deposited %s x%.0f", item.ItemID, currentQty)
			successfulDeposits++
			// Brief delay between deposits
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(SleepShort):
			}
		}
	}

	fmt.Printf("📥 Deposit complete: %d successful, %d failed\n", successfulDeposits, depositErrors)

	if depositErrors > 0 {
		return fmt.Errorf("failed to deposit %d out of %d items", depositErrors, len(state.Ship.Cargo))
	}

	return nil
}

// Refuel refills the ship's fuel tank at the current station.
//
// Wraps the Submit error through maybeGoalReached so a tank_full server
// error becomes a *GoalReachedError sentinel that the play_as loop
// recognizes as a successful exit condition.
func (c *Client) Refuel(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "refuel",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return maybeGoalReached("refuel", err)
}

// Repair repairs the ship's hull. At station uses credits; in space uses repair kits.
// v0.240: optional params for item_id, quantity, and target (remote repair).
//
// Wraps the Submit error through maybeGoalReached so a no_damage
// server error becomes a *GoalReachedError.
func (c *Client) Repair(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "repair",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return maybeGoalReached("repair", err)
}

// RepairWith repairs using specific options (repair kits, remote target, etc.).
func (c *Client) RepairWith(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "repair",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return maybeGoalReached("repair", err)
}

// Fleet manages player fleet operations (create, invite, accept, decline, leave, kick, disband, status).
// v0.240: new command.
func (c *Client) Fleet(ctx context.Context, action string, playerID string) error {
	payload := map[string]any{"action": action}
	if playerID != "" {
		payload["player_id"] = playerID
	}
	msg := protocol.Message{
		Type:      "fleet",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// DistressSignal broadcasts a distress signal to nearby players.
// v0.240: accepts optional distress_type ("fuel", "repair", "combat").
func (c *Client) DistressSignal(ctx context.Context, distressType string) error {
	payload := map[string]any{}
	if distressType != "" {
		payload["distress_type"] = distressType
	}
	msg := protocol.Message{
		Type:      "distress_signal",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// Buy purchases items or modules at the current station
func (c *Client) Buy(ctx context.Context, itemID string, quantity float64) error {
	msg := protocol.Message{
		Type:      "buy",
		Payload:   map[string]any{"item_id": itemID, "quantity": quantity},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
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

			// Check if this is a server close frame and run the close policy.
			if closeErr, ok := err.(*websocket.CloseError); ok {
				c.debugLogger.Printf("[listen-%d] Server close frame | Status: %s (%d) | Reason: %q",
					goroutineID, closeErr.Code, closeErr.Code, closeErr.Reason)
				c.handleClose(closeErr)
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

					if max := c.debugPayloadMaxLen; max > 0 && len(payloadStr) > max {
						c.debugLogger.Printf("Response Payload: %s... [truncated]", payloadStr[:max])
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

			// Fan out through the new response router. Runs after state
			// parsers so callers reading State inside their response
			// handler see fresh data. Legacy CmdQueue/waiters remain
			// below until the last method finishes migrating.
			if c.router != nil {
				c.router.dispatch(resp)
			}

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

// updateTickFromPayload bumps CurrentTick if the payload carries a tick or
// current_tick field. Also handles the action_result shape where the tick
// lives alongside a "result" object, and a few payloads (combat_update,
// pirate_warning, mining events) that include tick at the top level but
// aren't otherwise tick-aware in the switch below.
func (c *Client) updateTickFromPayload(payload map[string]any) {
	var nextTick int64
	if tick, ok := payload["tick"].(float64); ok {
		nextTick = int64(tick)
	} else if tick, ok := payload["current_tick"].(float64); ok {
		nextTick = int64(tick)
	} else {
		return
	}
	c.mu.Lock()
	if nextTick > c.state.CurrentTick {
		c.state.CurrentTick = nextTick
	}
	c.mu.Unlock()
}

// handleResponse updates the game state based on server responses
func (c *Client) handleResponse(resp protocol.Response) {
	// Store raw JSON for key response types (has its own locking)
	c.storeRawJSON(resp)

	// Centralized tick tracking: any response carrying a tick (or current_tick)
	// field advances CurrentTick. The per-type branches below may also update
	// it (idempotent), but this catches frames the switch doesn't otherwise
	// process — pirate/combat events, action_result frames whose result.tick
	// lives at top level, etc.
	c.updateTickFromPayload(resp.Payload)

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
		c.parseInlineFuelAndHull(resp.Payload)
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
		// get_cargo returns type "ok" with cargo[] / used / capacity / available
		// at the top level (not nested under "ship"), so parseShipData doesn't
		// populate state.Ship.Cargo. Parse it here so callers like
		// DepositAllItems see fresh cargo after a get_cargo refresh.
		if _, hasCargo := resp.Payload["cargo"]; hasCargo {
			if _, hasCapacity := resp.Payload["capacity"]; hasCapacity {
				c.parseGetCargoData(resp.Payload)
			}
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
		c.parseInlineFuelAndHull(resp.Payload)
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

			// Update XP context: POI as target and mined quantity for observation.
			if c.XPCallback != nil {
				c.xpMu.Lock()
				if c.state.CurrentPOI != "" {
					c.xpLastTarget = c.state.CurrentPOI
				}
				c.xpLastQuantity = int(quantity)
				c.xpMu.Unlock()
			}
			c.mu.Unlock()
		}
		// Track xp_gained into state.SkillXP and fire the callback, matching
		// the pattern used for craft/buy/sell/repair so the XPTracker + KB
		// observer see mining XP. Server payload shape:
		//   "xp_gained": {"deep_core_mining": 2, "mining": 25, "piloting": 4}
		if xpGained, ok := resp.Payload["xp_gained"].(map[string]any); ok && len(xpGained) > 0 {
			c.mu.Lock()
			if c.state.Player.Skills == nil {
				c.state.Player.Skills = make(map[string]Skill)
			}
			if c.state.SkillXP == nil {
				c.state.SkillXP = make(map[string]float64)
			}
			for skillID, xpAmount := range xpGained {
				if xpFloat, ok := xpAmount.(float64); ok && xpFloat > 0 {
					c.state.SkillXP[skillID] += xpFloat
					c.debugLogger.Printf("Mine XP gained: %s %.1f XP", skillID, xpFloat)
				}
			}
			c.mu.Unlock()
			c.checkXPChanges()
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

	case protocol.TypeScanDetected:
		c.debugLogger.Printf("👁️  SCAN DETECTED: %v", resp.Payload)

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

	case protocol.TypeBattleAlert:
		// Informational: someone else's battle is starting in the same
		// system. We don't mutate our own state — the alert doesn't mean
		// we're participating. Just log for visibility.
		msg, _ := resp.Payload["message"].(string)
		battleID, _ := resp.Payload["battle_id"].(string)
		systemID, _ := resp.Payload["system_id"].(string)
		c.debugLogger.Printf("[BATTLE ALERT] %s (battle=%s system=%s)", msg, battleID, systemID)

	case protocol.TypeChatMessage:
		var chatMsg serverapi.ChatMessage
		if data, err := json.Marshal(resp.Payload); err == nil {
			if err := json.Unmarshal(data, &chatMsg); err == nil {
				c.onChatMu.RLock()
				cb := c.onChatMessage
				c.onChatMu.RUnlock()
				if cb != nil {
					cb(chatMsg)
				}
			}
		}
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

	// Many server responses carry a partial Player payload — e.g. only the
	// skills whose XP/level changed, or a Skills map with XP=0 (level-only).
	// Preserve the existing Skills/SkillXP so unrelated skills don't briefly
	// disappear and re-appear, which would fire spurious XP-change callbacks.
	preservedSkills := c.state.Player.Skills
	preservedSkillXP := c.state.SkillXP

	c.state.Player = player

	if preservedSkills != nil {
		if c.state.Player.Skills == nil {
			c.state.Player.Skills = make(map[string]Skill, len(preservedSkills))
		}
		for k, oldSkill := range preservedSkills {
			if newSkill, ok := c.state.Player.Skills[k]; ok {
				if newSkill.XP == 0 && oldSkill.XP > 0 && newSkill.Level == oldSkill.Level {
					newSkill.XP = oldSkill.XP
					c.state.Player.Skills[k] = newSkill
				}
			} else {
				c.state.Player.Skills[k] = oldSkill
			}
		}
	}

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

	// Sync skill XP to state level — merge rather than replace so partial
	// updates (only the changed skill) don't wipe cached values for others.
	if len(player.SkillXP) > 0 {
		if preservedSkillXP == nil {
			c.state.SkillXP = player.SkillXP
		} else {
			maps.Copy(preservedSkillXP, player.SkillXP)
			c.state.SkillXP = preservedSkillXP
		}
	} else {
		c.state.SkillXP = preservedSkillXP
	}

	// Check for XP changes (called with c.mu held)
	c.checkXPChanges()

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

	// Extract per-player live state (xp, level) from the skills map.
	// The live get_skills response embeds both catalog metadata and per-player
	// state in each skills entry. Update state.SkillXP and state.Player.Skills
	// so checkXPChanges can detect passive XP gains captured by the runner's
	// periodic GetSkills poll.
	rawSkills, ok := payload["skills"].(map[string]any)
	if ok {
		if c.state.SkillXP == nil {
			c.state.SkillXP = make(map[string]float64)
		}
		if c.state.Player.Skills == nil {
			c.state.Player.Skills = make(map[string]Skill)
		}
		for skillID, raw := range rawSkills {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			sk := c.state.Player.Skills[skillID]
			if xp, ok := entry["xp"].(float64); ok {
				c.state.SkillXP[skillID] = xp
				sk.XP = xp
			}
			if lvl, ok := entry["level"].(float64); ok {
				sk.Level = int(lvl)
			}
			c.state.Player.Skills[skillID] = sk
		}

		// Seed the XP baseline for any skill not already tracked so the first
		// comprehensive get_skills snapshot after reconnect establishes baseline
		// rather than reporting cumulative XP as spurious deltas. Skills already
		// in the baseline still produce real deltas for genuine passive gains.
		c.xpMu.Lock()
		if c.xpLastXP != nil {
			for skillID, xp := range c.state.SkillXP {
				if _, exists := c.xpLastXP[skillID]; !exists {
					c.xpLastXP[skillID] = xp
				}
			}
		}
		if c.xpLastSkills != nil {
			for skillID, sk := range c.state.Player.Skills {
				if _, exists := c.xpLastSkills[skillID]; !exists {
					c.xpLastSkills[skillID] = sk
				}
			}
		}
		c.xpMu.Unlock()

		c.checkXPChanges()
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
//
// Tries the canonical "ship" key first; if absent and the payload looks like
// a get_ship response (top-level cargo_max + a "class" object whose owner_id
// is the current player), falls back to "class", which since v0.275 holds
// the ship-instance fields — confusingly named for legacy reasons.
func (c *Client) parseShipData(payload map[string]any) {
	var ext serverapi.Ship
	matched := unmarshalPayloadKey(payload, "ship", &ext)
	if !matched {
		// Only treat "class" as the ship instance for the get_ship shape,
		// which is identifiable by top-level cargo_max. Other responses
		// (browse_ships, list_ships, recipes) may carry "class" as a label
		// or class-spec object — we must not overwrite ship state from those.
		if _, hasCargoMax := payload["cargo_max"]; hasCargoMax {
			matched = unmarshalPayloadKey(payload, "class", &ext)
		}
	}
	if matched {
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

	// get_ship (server v0.275+) returns cargo_used/cargo_max at the top
	// level instead of nested under "ship". Use them as the authoritative
	// resync — mining_yield etc. increment CargoUsed locally and can drift
	// past CargoCapacity if a refresh path doesn't reset it.
	if cargoUsed, ok := payload["cargo_used"].(float64); ok {
		c.mu.Lock()
		c.state.Ship.CargoUsed = cargoUsed
		c.mu.Unlock()
	}
	if cargoMax, ok := payload["cargo_max"].(float64); ok {
		c.mu.Lock()
		c.state.Ship.CargoCapacity = cargoMax
		c.state.MaxCargo = int(cargoMax)
		c.mu.Unlock()
	}

	// Parse module definitions from payload level (from get_ship response)
	var moduleDefs []serverapi.ShipModule
	if unmarshalPayloadKey(payload, "modules", &moduleDefs) {
		c.mu.Lock()
		if c.state.ModuleDefinitions == nil {
			c.state.ModuleDefinitions = make(map[string]ModuleDefinition)
		}
		for _, extDef := range moduleDefs {
			if extDef.ID != "" {
				c.state.ModuleDefinitions[extDef.ID] = ModuleDefinitionFromShipModule(extDef)
			}
		}
		c.mu.Unlock()
	}
}

// parseGetCargoData extracts the top-level cargo[]/used/capacity/available
// fields from a get_cargo response and updates state.Ship.Cargo accordingly.
// Unlike parseShipData this does not require a nested "ship" object; get_cargo
// returns the cargo list directly at payload level.
func (c *Client) parseGetCargoData(payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	var resp serverapi.GetCargoResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return
	}

	cargo := make([]CargoItem, 0, len(resp.Cargo))
	for _, item := range resp.Cargo {
		cargo = append(cargo, CargoItemFromAPI(item))
	}

	legacy := make([]map[string]any, len(cargo))
	for i, item := range cargo {
		legacy[i] = map[string]any{
			"item_id":  item.ItemID,
			"quantity": item.Quantity,
		}
	}

	c.mu.Lock()
	c.state.Ship.Cargo = cargo
	c.state.Ship.CargoUsed = float64(resp.Used)
	if resp.Capacity > 0 {
		c.state.Ship.CargoCapacity = float64(resp.Capacity)
		c.state.MaxCargo = resp.Capacity
	}
	c.state.Cargo = legacy
	c.mu.Unlock()
}

// parseInlineFuelAndHull extracts top-level fuel and hull fields from OK responses.
// Many responses (refuel, repair, use_item, dock) return fuel_now/fuel_max and
// hull_now/hull_max at the payload level rather than inside a "ship" object.
func (c *Client) parseInlineFuelAndHull(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Fuel: prefer fuel_now over fuel (fuel_now is the post-action value)
	if fuelNow, ok := payload["fuel_now"].(float64); ok {
		c.state.Fuel = fuelNow
		c.state.Ship.Fuel = fuelNow
	} else if fuel, ok := payload["fuel"].(float64); ok {
		// Only update from top-level "fuel" if no "ship" key was present
		// (parseShipData already handled the ship.fuel case)
		if _, hasShip := payload["ship"]; !hasShip {
			c.state.Fuel = fuel
			c.state.Ship.Fuel = fuel
		}
	}
	if fuelMax, ok := payload["fuel_max"].(float64); ok {
		c.state.MaxFuel = fuelMax
		c.state.Ship.MaxFuel = fuelMax
	}

	// Hull: same pattern for repair and use_item responses
	if hullNow, ok := payload["hull_now"].(float64); ok {
		c.state.Hull = hullNow
		c.state.Ship.Hull = hullNow
	}
	if maxHull, ok := payload["max_hull"].(float64); ok {
		if _, hasShip := payload["ship"]; !hasShip {
			c.state.MaxHull = maxHull
			c.state.Ship.MaxHull = maxHull
		}
	}

	// Shield: use_item responses can restore shields
	if shield, ok := payload["shield"].(float64); ok {
		if _, hasShip := payload["ship"]; !hasShip {
			c.state.Ship.Shield = shield
		}
	}
	if maxShield, ok := payload["max_shield"].(float64); ok {
		if _, hasShip := payload["ship"]; !hasShip {
			c.state.Ship.MaxShield = maxShield
		}
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
	// POI not in the system list — skip appending to avoid stale cross-system POIs.
	// The authoritative POI list comes from get_system; get_poi only enriches existing entries.
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

	// Stamp the tick when we received live system data (i.e. we are physically in this system).
	if tick := c.state.CurrentTick; tick > 0 {
		c.state.System.LastVisitedTick = tick
	}

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
	}
	// Absence of travel_progress is NOT used to clear Traveling: a state_update
	// arriving on the same tick as a "Will execute on next tick" travel ack has
	// no progress yet, and clearing here would let Travel() return before the
	// ship actually moves — causing every subsequent command in a tight loop
	// to hit "another action pending". Clears come from the explicit terminal
	// signals (action_result.action ∈ {arrived, jumped} in parseActionResult,
	// the same actions in parseTravelAction on TypeOK, and TypeDocked).
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

		// Handle xp_gained if present
		if xpGained, ok := result["xp_gained"].(map[string]any); ok && len(xpGained) > 0 {
			for skillID, xpAmount := range xpGained {
				if xpFloat, ok := xpAmount.(float64); ok && xpFloat > 0 {
					// Update player skill XP
					if c.state.Player.Skills == nil {
						c.state.Player.Skills = make(map[string]Skill)
					}
					if c.state.SkillXP == nil {
						c.state.SkillXP = make(map[string]float64)
					}

					c.state.SkillXP[skillID] += xpFloat

					// Check for level up (simplified check)
					// The server will send full skill data in next get_skills, but this
					// ensures the callback fires with the updated XP
					c.debugLogger.Printf("Craft XP gained: %s %.1f XP", skillID, xpFloat)
				}
			}
			// Check for XP changes after processing xp_gained
			c.checkXPChanges()
		}

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
		// Some action_result frames omit the "action" field and key off
		// "command" instead. Dispatch by command before logging "unhandled".
		switch command {
		case "buy_listed_ship":
			if credits, ok := result["credits_left"].(float64); ok {
				c.state.Player.Credits = credits
				c.state.Credits = credits
			}
			if shipID, ok := result["ship_id"].(string); ok && shipID != "" {
				c.state.Ship.ID = shipID
			}
			if classID, ok := result["class_id"].(string); ok && classID != "" {
				c.state.Ship.ClassID = classID
			}
			c.debugLogger.Printf("Action result: bought ship %s (credits left: %.0f)", c.state.Ship.ID, c.state.Player.Credits)
		default:
			if command != "" {
				c.debugLogger.Printf("Action result: %s (unhandled)", command)
			} else {
				c.debugLogger.Printf("Action result: %s (unhandled)", action)
			}
		}
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

// pushOnlyResponseTypes are server-initiated events that are never a reply to
// a client command. They get their own dedicated parsers/callbacks and must
// NOT overwrite the "_last" raw JSON slot — otherwise interactive tools like
// play_as see "last response" flip to a random push event (tick, incoming
// chat, a combat update, etc.) between the moment their command returns and
// the moment they read _last.
var pushOnlyResponseTypes = map[string]struct{}{
	protocol.TypeTick:               {},
	protocol.TypeStateUpdate:        {},
	protocol.TypeChatMessage:        {},
	protocol.TypeCombatUpdate:       {},
	protocol.TypeBattleAlert:        {},
	protocol.TypePirateWarning:      {},
	protocol.TypePoliceWarning:      {},
	protocol.TypePlayerDied:         {},
	protocol.TypeScanDetected:       {},
	protocol.TypeTradeOfferReceived: {},
	protocol.TypePilotlessShip:      {},
	protocol.TypeReconnected:        {},
	protocol.TypeSkillLevelUp:       {},
}

// storeRawJSON stores raw JSON payloads for key response types
func (c *Client) storeRawJSON(resp protocol.Response) {
	// Cache the last response for interactive tools like play_as — but skip
	// unsolicited push events (see pushOnlyResponseTypes) so they don't clobber
	// the reply the caller is about to read.
	if _, isPush := pushOnlyResponseTypes[resp.Type]; !isPush {
		if jsonData, err := json.Marshal(resp.Payload); err == nil {
			c.rawJSONMu.Lock()
			c.latestRawJSON["_last"] = jsonData
			c.rawJSONMu.Unlock()
		}
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
			case "survey_system":
				storeKey = "survey"
				shouldStore = true
				// Update XP target to use system_id from response
				if systemID, ok := resp.Payload["system_id"].(string); ok && c.XPCallback != nil {
					c.xpMu.Lock()
					c.xpLastTarget = systemID
					c.xpMu.Unlock()
				}
			case "buy":
				storeKey = "buy"
				shouldStore = true
				// Handle xp_gained if present (buy commands may grant trading XP)
				if xpGained, ok := resp.Payload["xp_gained"].(float64); ok && xpGained > 0 {
					c.mu.Lock()
					// Assume buy grants trading XP (common for commerce actions)
					skillID := "trading"
					if c.state.Player.Skills == nil {
						c.state.Player.Skills = make(map[string]Skill)
					}
					if c.state.SkillXP == nil {
						c.state.SkillXP = make(map[string]float64)
					}
					c.state.SkillXP[skillID] += xpGained
					c.debugLogger.Printf("Buy XP gained: %s %.1f XP", skillID, xpGained)
					c.mu.Unlock()
					// Check for XP changes after processing xp_gained
					c.checkXPChanges()
				}
			case "sell":
				storeKey = "sell"
				shouldStore = true
				// Handle xp_gained if present (sell commands may grant trading XP)
				if xpGained, ok := resp.Payload["xp_gained"].(float64); ok && xpGained > 0 {
					c.mu.Lock()
					// Assume sell grants trading XP (common for commerce actions)
					skillID := "trading"
					if c.state.Player.Skills == nil {
						c.state.Player.Skills = make(map[string]Skill)
					}
					if c.state.SkillXP == nil {
						c.state.SkillXP = make(map[string]float64)
					}
					c.state.SkillXP[skillID] += xpGained
					c.debugLogger.Printf("Sell XP gained: %s %.1f XP", skillID, xpGained)
					c.mu.Unlock()
					// Check for XP changes after processing xp_gained
					c.checkXPChanges()
				}
			case "repair_module":
				storeKey = "repair"
				shouldStore = true
				// Handle xp_gained if present (object type like craft)
				if xpGained, ok := resp.Payload["xp_gained"].(map[string]any); ok && len(xpGained) > 0 {
					c.mu.Lock()
					if c.state.Player.Skills == nil {
						c.state.Player.Skills = make(map[string]Skill)
					}
					if c.state.SkillXP == nil {
						c.state.SkillXP = make(map[string]float64)
					}
					for skillID, xpAmount := range xpGained {
						if xpFloat, ok := xpAmount.(float64); ok && xpFloat > 0 {
							c.state.SkillXP[skillID] += xpFloat
							c.debugLogger.Printf("Repair module XP gained: %s %.1f XP", skillID, xpFloat)
						}
					}
					c.mu.Unlock()
					// Check for XP changes after processing xp_gained
					c.checkXPChanges()
				}
			case "salvage_wreck":
				storeKey = "salvage"
				shouldStore = true
				// Handle xp_gained if present (integer type like buy/sell)
				if xpGained, ok := resp.Payload["xp_gained"].(float64); ok && xpGained > 0 {
					c.mu.Lock()
					// Salvage typically grants salvaging XP
					skillID := "salvaging"
					if c.state.Player.Skills == nil {
						c.state.Player.Skills = make(map[string]Skill)
					}
					if c.state.SkillXP == nil {
						c.state.SkillXP = make(map[string]float64)
					}
					c.state.SkillXP[skillID] += xpGained
					c.debugLogger.Printf("Salvage wreck XP gained: %s %.1f XP", skillID, xpGained)
					c.mu.Unlock()
					// Check for XP changes after processing xp_gained
					c.checkXPChanges()
				}
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
		// Store storage data (from view_storage response).
		// base_id is the reliable indicator for view_storage — items and
		// ships may be omitted when empty. But several other commands
		// (get_missions, get_active_missions, view_orders, facility list,
		// etc.) also include base_id; exclude those by checking for their
		// distinctive keys so storeKey/the storage callback don't get
		// hijacked.
		if _, hasBaseID := resp.Payload["base_id"]; hasBaseID {
			_, hasMissions := resp.Payload["missions"]
			_, hasOrders := resp.Payload["orders"]
			_, hasServices := resp.Payload["services"]
			_, hasStationFac := resp.Payload["station_facilities"]
			_, hasFactionFac := resp.Payload["faction_facilities"]
			_, hasPlayerFac := resp.Payload["player_facilities"]
			_, hasFactionID := resp.Payload["faction_id"]
			// view_market / view_orders responses now carry base_id too; the
			// action field is the cheapest disambiguator. Anything explicitly
			// labelled with a non-storage action shouldn't be misclassified.
			actionStr, _ := resp.Payload["action"].(string)
			isStorageAction := actionStr == "" || actionStr == "view_storage" || actionStr == "view_faction_storage"
			isFacility := hasStationFac || hasFactionFac || hasPlayerFac
			isStorageShape := isStorageAction && !hasMissions && !hasOrders && !hasServices && !isFacility
			if isStorageShape {
				if storeKey == "" {
					if hasFactionID {
						storeKey = "faction_storage"
					} else {
						storeKey = "storage"
					}
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
		// Store get_system_agents responses ({agents:[], system_id, count}).
		// system_id disambiguates from other agents-bearing payloads.
		if _, hasAgents := resp.Payload["agents"]; hasAgents {
			if _, hasSystemID := resp.Payload["system_id"]; hasSystemID {
				if storeKey == "" {
					storeKey = "get_system_agents"
				}
				shouldStore = true
			}
		}
		// Store facility responses. Sync queries (list/types/upgrades/help/
		// faction_list) come back as type=ok with no command field, so they
		// fall through here. Async terminals are stored later via the
		// command-keyed TypeActionResult path.
		_, hasStationFac := resp.Payload["station_facilities"]
		_, hasFactionFac := resp.Payload["faction_facilities"]
		_, hasPlayerFac := resp.Payload["player_facilities"]
		if hasStationFac || hasFactionFac || hasPlayerFac {
			if storeKey == "" {
				storeKey = "facility"
			}
			shouldStore = true
		}
		if action, _ := resp.Payload["action"].(string); action != "" {
			switch action {
			case "types", "upgrades", "help", "faction_list",
				"personal_visit", "personal_decorate":
				if storeKey == "" {
					storeKey = "facility"
				}
				shouldStore = true
			}
		}
		// Store get_notes response (notes list with total_count).
		if _, hasNotes := resp.Payload["notes"]; hasNotes {
			if storeKey == "" {
				storeKey = "notes"
			}
			shouldStore = true
		}
		// Store read_note response (single note details with note_id + content).
		if _, hasNoteID := resp.Payload["note_id"]; hasNoteID {
			if _, hasContent := resp.Payload["content"]; hasContent {
				if storeKey == "" {
					storeKey = "note"
				}
				shouldStore = true
			}
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
		// Store get_action_log response. Identified by "entries" array
		// alongside "has_more"; the pair distinguishes it from other arrays.
		if _, hasEntries := resp.Payload["entries"]; hasEntries {
			if _, hasHasMore := resp.Payload["has_more"]; hasHasMore {
				if storeKey == "" {
					storeKey = "action_log"
				}
				shouldStore = true
			}
		}
		// Store get_skills response. Older builds keyed off "player_skills";
		// newer builds may omit it (empty fleet) or replace the inline
		// definitions map. Match on either key, plus the action/command
		// fallback for robustness.
		_, hasPlayerSkills := resp.Payload["player_skills"]
		_, hasSkillsMap := resp.Payload["skills"]
		actionField, _ := resp.Payload["action"].(string)
		commandField, _ := resp.Payload["command"].(string)
		if hasPlayerSkills || hasSkillsMap || actionField == "get_skills" || commandField == "get_skills" {
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
	case protocol.TypeActionResult:
		// action_result responses are the terminal of mutation commands.
		// Store the full payload under a key derived from the "command"
		// field so the REPL's lookupRawJSON can find them via
		// rawJSONKeyForCommand[<command>] mapping. Without this, callers
		// like `facility list` find nothing because the OK-only switch
		// above doesn't match action_result frames.
		if cmd, ok := resp.Payload["command"].(string); ok && cmd != "" {
			storeKey = cmd
			shouldStore = true
		}
	case protocol.TypeMiningYield:
		// Newer servers terminate `mine` by pushing a mining_yield event
		// (carrying quantity, resource, depletion, xp_gained) instead of
		// emitting an action_result with command="mine". Without this case
		// the only thing in latestRawJSON["mine"] is the pending-ack frame
		// the server returned synchronously, and formatMine would render
		// blanks. Storing the yield under "mine" gives the REPL the actual
		// terminal payload it expects.
		storeKey = "mine"
		shouldStore = true
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

// ClearRawJSON drops the stored payload for key (no-op if absent). Callers
// use this to invalidate a stale slot before issuing a new request, so if
// the current response never lands (dropped, lost to a racing waiter, etc.)
// downstream consumers see nothing rather than the previous command's data.
func (c *Client) ClearRawJSON(key string) {
	c.rawJSONMu.Lock()
	defer c.rawJSONMu.Unlock()
	delete(c.latestRawJSON, key)
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

// waitForResponse waits for a response of a specific type with a timeout.
//
// Deprecated: use execQuery with an appropriate Classifier. Type-keyed
// single-slot waiter; multiple callers collide.
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

// waitForInitialResponse waits for the first OK or error response from the server.
// Unlike Submit's terminator, it does NOT loop on pending/in-progress — it returns
// the first response and lets the caller decide what to do.
func (c *Client) waitForInitialResponse(ctx context.Context, timeout time.Duration) (protocol.Response, error) {
	// Log the final response paired with the last sent request
	var finalResp *protocol.Response
	defer func() {
		if finalResp != nil {
			c.logCallResponse(*finalResp)
		}
	}()

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
				c.debugLogger.Printf("Action queued by server — waiting for next-tick execution")
				deadline = time.After(timeout)
				continue
			}
			finalResp = &resp
			return resp, nil

		case resp := <-actionResultChan:
			// action_result arrives when the server processes a pending action
			// on the next tick. Treat it as the initial response.
			c.debugLogger.Printf("Received action_result as initial response")
			finalResp = &resp
			return resp, nil

		case resp := <-errorChan:
			finalResp = &resp
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
			finalResp = &resp
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

// sendPingLoop actively sends WebSocket protocol-level pings at
// SleepWSPingInterval. This keeps NAT/proxy flow tables warm (most drop idle
// TCP flows after 60-120s) and gives us an authoritative liveness signal
// independent of application traffic. A successful pong refreshes
// lastMessageTime. After PingMaxConsecutiveFailures ping failures in a row we
// force-close the connection so listen()'s blocked Read() errors out and
// triggers the normal reconnect path — otherwise a half-open TCP socket can
// keep Read() parked indefinitely while the 5-minute passive timeout waits.
func (c *Client) sendPingLoop(ctx context.Context) {
	goroutineID := atomic.AddInt64(&c.goroutineID, 1)
	c.debugLogger.Printf("[ping-%d] Ping loop started | Interval: %v | Timeout: %v", goroutineID, SleepWSPingInterval, SleepWSPingTimeout)
	defer c.debugLogger.Printf("[ping-%d] Ping loop exited", goroutineID)

	ticker := time.NewTicker(SleepWSPingInterval)
	defer ticker.Stop()

	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopPing:
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			connected := c.connected
			c.mu.RUnlock()
			if conn == nil || !connected {
				consecutiveFailures = 0
				continue
			}

			pingCtx, cancel := context.WithTimeout(ctx, SleepWSPingTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				consecutiveFailures++
				c.debugLogger.Printf("[ping-%d] Ping failed (%d/%d): %v", goroutineID, consecutiveFailures, PingMaxConsecutiveFailures, err)
				if consecutiveFailures >= PingMaxConsecutiveFailures {
					c.debugLogger.Printf("[ping-%d] Max consecutive ping failures reached, force-closing connection to trigger reconnect", goroutineID)
					_ = conn.Close(websocket.StatusGoingAway, "ping timeout")
					consecutiveFailures = 0
				}
				continue
			}
			consecutiveFailures = 0
			c.updateLastMessageTime()
		}
	}
}

// Uptime returns how long the current connection has been alive. Zero if
// the client has never connected. Used by ReconnectingHandler to detect
// the session-contention pattern (rapid back-to-back reconnects).
func (c *Client) Uptime() time.Duration {
	c.diagnosticMu.RLock()
	defer c.diagnosticMu.RUnlock()
	if c.connectTime.IsZero() {
		return 0
	}
	return time.Since(c.connectTime)
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

// logCallResponse logs the most recently sent request paired with the given response.
// It is called from waitForActionResponse / waitForInitialResponse at the resolution point.
func (c *Client) logCallResponse(resp protocol.Response) {
	if c.CallLogger == nil {
		return
	}
	c.lastSentMsgMu.Lock()
	req := c.lastSentMsg
	msgType := c.lastSentMsgType
	c.lastSentMsg = nil
	c.lastSentMsgType = ""
	c.lastSentMsgMu.Unlock()

	if req == nil {
		return
	}

	// Build state snapshot from current game state
	snap := c.buildStateSnapshot()

	respJSON, err := json.Marshal(resp)
	if err != nil {
		c.debugLogger.Printf("calllog: failed to marshal response: %v", err)
		return
	}
	if err := c.CallLogger.Log(msgType, snap, req, respJSON); err != nil {
		c.debugLogger.Printf("calllog: failed to write log: %v", err)
	}
}

// buildStateSnapshot captures the current location and ship state for call logging.
func (c *Client) buildStateSnapshot() calllog.StateSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Resolve module IDs to names where definitions are available
	modules := make([]string, len(c.state.Ship.Modules))
	for i, modID := range c.state.Ship.Modules {
		if def, ok := c.state.ModuleDefinitions[modID]; ok {
			modules[i] = def.Name
		} else {
			modules[i] = modID
		}
	}

	return calllog.StateSnapshot{
		Location: calllog.LocationInfo{
			System:    c.state.CurrentSystem,
			POI:       c.state.CurrentPOI,
			Docked:    c.state.Doc,
			Traveling: c.state.Traveling,
		},
		Ship: calllog.ShipInfo{
			Name:          c.state.Ship.Name,
			ClassID:       c.state.Ship.ClassID,
			Hull:          c.state.Hull,
			MaxHull:       c.state.MaxHull,
			Shield:        c.state.Ship.Shield,
			MaxShield:     c.state.Ship.MaxShield,
			Fuel:          c.state.Fuel,
			MaxFuel:       c.state.MaxFuel,
			CargoUsed:     c.state.Ship.CargoUsed,
			CargoCapacity: c.state.Ship.CargoCapacity,
			Modules:       modules,
		},
	}
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

// fireXPCallback compares skill state after a successful action against the
// snapshot taken in Send(), and fires the XPCallback with any deltas.
// checkXPChanges compares current skill state against last known state.
// Called after parsePlayerData updates the state. Must be called with c.mu held.
func (c *Client) checkXPChanges() {
	if c.XPCallback == nil {
		return
	}

	currentSkills := copySkillMap(c.state.Player.Skills)
	currentXP := copyStringFloatMap(c.state.SkillXP)
	gameTick := c.state.CurrentTick

	c.xpMu.Lock()
	beforeSkills := c.xpLastSkills
	beforeXP := c.xpLastXP
	action := c.xpLastAction
	target := c.xpLastTarget
	quantity := c.xpLastQuantity

	// Update last known state
	c.xpLastSkills = currentSkills
	c.xpLastXP = currentXP
	c.xpMu.Unlock()

	// Skip first call (no previous state to compare)
	// Use OR because we need both baselines to do a proper comparison
	if beforeSkills == nil || beforeXP == nil {
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
	// Also check if any skills were removed from beforeXP
	if !changed {
		for k := range beforeXP {
			if _, exists := currentXP[k]; !exists {
				changed = true
				break
			}
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
	// Also check if any skills were removed from beforeSkills
	if !changed {
		for k := range beforeSkills {
			if _, exists := currentSkills[k]; !exists {
				changed = true
				break
			}
		}
	}
	if !changed {
		return
	}

	c.XPCallback(action, target, quantity, beforeSkills, currentSkills, beforeXP, currentXP, gameTick)
}

// copySkillMap returns a shallow copy of a skill map.
func copySkillMap(m map[string]Skill) map[string]Skill {
	if m == nil {
		return nil
	}
	out := make(map[string]Skill, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// extractActionTarget pulls the most relevant target identifier from a message payload.
func extractActionTarget(msg protocol.Message) string {
	return extractTargetFromArgs(msg.Payload)
}

// extractTargetFromArgs pulls the most relevant target identifier from a
// generic args map. Used by both WS (protocol.Message.Payload) and MCP
// (callTool args) code paths for XP observation attribution.
func extractTargetFromArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	// Check common target field names in order of specificity
	for _, key := range []string{"target_id", "poi_id", "system_id", "listing_id", "ship_id", "ship_class", "item_id", "recipe_id", "mission_id", "wreck_id", "commission_id"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	// Fallback: return first string value
	for _, v := range args {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractQuantityFromArgs pulls the quantity parameter from a generic args map.
// Returns 1 if no quantity is specified (the default for single actions).
func extractQuantityFromArgs(args map[string]any) int {
	if len(args) == 0 {
		return 1
	}
	// Check for quantity field
	if v, ok := args["quantity"]; ok {
		// Handle different numeric types
		switch val := v.(type) {
		case int:
			if val > 0 {
				return val
			}
		case float64:
			if val > 0 {
				return int(val)
			}
		case int32:
			if val > 0 {
				return int(val)
			}
		case int64:
			if val > 0 {
				return int(val)
			}
		}
	}
	return 1 // default to 1 for single actions
}
