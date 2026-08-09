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
	"sort"
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

	// debugOut is the debug logger's original output writer, captured at
	// construction so SetDebugLogging(true) can restore it after a prior
	// SetDebugLogging(false) redirected output to io.Discard.
	debugOut io.Writer

	// debugPayloadMaxLen caps the "Response Payload:" debug-log line. 0
	// means no cap (log the whole payload). Default 200, tuned to keep the
	// log readable for short responses.
	debugPayloadMaxLen int

	// quietEventTypes are server response types whose receive-side debug
	// logging is suppressed even when --debug is on. The listen loop already
	// hardcodes a skip for poi_arrival/poi_departure; this set lets callers
	// (e.g. play_as via --quiet-events) silence other high-frequency pushes
	// such as mining_yield emitted by mining drones every tick. Read under
	// quietEventMu; populated by SetQuietEventTypes.
	quietEventTypes map[string]struct{}
	quietEventMu    sync.RWMutex

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
	connectionID     string    // Unique ID for this connection instance
	connectTime      time.Time // When this connection was established
	messagesSent     int64     // Counter for messages sent
	messagesReceived int64     // Counter for messages received
	lastSendTime     time.Time // Time of last send
	lastReceiveTime  time.Time // Time of last receive
	diagnosticMu     sync.RWMutex
	goroutineID      int64 // Counter for tracking goroutine instances

	router      *responseRouter
	inflight    *inflight
	actionLocks *actionLockMap

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

	// Crafting update callback — fired when a crafting_update push event is received
	onCraftingUpdate func(ev serverapi.CraftingUpdateEvent)
	onCraftingMu     sync.RWMutex

	// Player observer callback — fired when handleResponse parses a
	// payload containing player records (get_nearby, get_system_agents,
	// battle alerts, chat). See pkg/game/observed_player.go.
	playerObserver   PlayerObserver
	playerObserverMu sync.RWMutex

	// passengerObserver, if set, is invoked for each parsed response payload
	// containing passenger records (list_station_passengers, list_passengers,
	// load_passenger, dock arrivals). See pkg/game/observed_passenger.go.
	passengerObserver   PassengerObserver
	passengerObserverMu sync.RWMutex

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

	// reconnectFn performs a single reconnection attempt. Defaults to
	// client.Reconnect; overridable in tests to drive the retry loop without
	// real network I/O.
	reconnectFn func(ctx context.Context) error
	// reconnectBackoffUnit is the base unit of the exponential backoff between
	// reconnection attempts. Zero means time.Second (production default); tests
	// set it tiny to keep the retry loop fast.
	reconnectBackoffUnit time.Duration

	// gate, when set, coordinates reconnect dials across every client sharing
	// the host IP: it spaces dials apart so a mass disconnect (e.g. a server
	// restart) does not stampede the login endpoint, and propagates a per-IP
	// rate-limit block to the whole fleet so it expires instead of escalating.
	// Nil disables coordination (the default for direct/test construction).
	gate *ReconnectGate
}

// reconnectGateCooldown is the minimum spacing between reconnect dials across
// all clients on one host (~12/min, ~2x under the observed per-IP throttle).
const reconnectGateCooldown = 5 * time.Second

// reconnectBlockDefault is how long the fleet holds off on a bare per-IP block
// (HTTP 429 with no stated duration).
const reconnectBlockDefault = 60 * time.Second

// SetReconnectGate attaches a host-wide reconnect coordinator. Call it on the
// production agent path; leaving it unset keeps reconnects uncoordinated.
func (r *ReconnectingHandler) SetReconnectGate(g *ReconnectGate) {
	r.gate = g
}

// NewReconnectingHandler creates a handler that automatically reconnects on disconnect
func NewReconnectingHandler(client *Client, handler MessageHandler, ctx context.Context, logger *log.Logger) *ReconnectingHandler {
	return &ReconnectingHandler{
		client:      client,
		handler:     handler,
		ctx:         ctx,
		logger:      logger,
		reconnectFn: client.Reconnect,
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

// reconnectMaxAttempts is how many backoff loops a single reconnection burst
// makes before going dormant. We deliberately do NOT retry forever in the
// background: an extended outage would otherwise spin indefinitely. Instead the
// handler gives up after this many tries and waits to be re-woken — see
// TriggerReconnect, which the REPL calls on user input. A transient blip is
// recovered automatically; a long outage is recovered the moment the player
// next interacts.
const reconnectMaxAttempts = 3

// TriggerReconnect schedules a fresh reconnection burst if the client is
// disconnected and no reconnection is already in progress. It is safe to call
// repeatedly (e.g. from the REPL on every command) — the reconnecting CAS guard
// means a burst already running, or a live connection, makes this a no-op.
// Returns true when it actually started a new burst.
func (r *ReconnectingHandler) TriggerReconnect() bool {
	if r.client.IsConnected() {
		return false
	}
	if !r.reconnecting.CompareAndSwap(false, true) {
		return false
	}
	r.wg.Add(1)
	go r.attemptReconnection()
	return true
}

func (r *ReconnectingHandler) attemptReconnection() {
	defer r.wg.Done()
	defer r.reconnecting.Store(false)

	unit := r.reconnectBackoffUnit
	if unit <= 0 {
		unit = time.Second
	}

	for attempt := 1; attempt <= reconnectMaxAttempts; attempt++ {
		if r.ctx.Err() != nil {
			r.logger.Printf("Context cancelled, stopping reconnection attempts")
			return
		}

		// Wait for the host-wide gate before dialing: this serializes reconnects
		// across all clients on the IP and waits out any fleet-wide rate-limit
		// block. Nil gate (tests/direct construction) is a no-op.
		if err := r.gate.Acquire(r.ctx); err != nil {
			r.logger.Printf("Context cancelled while waiting for reconnect gate")
			return
		}

		// Try immediately on the first attempt so a user-triggered wake is
		// responsive; back off only between subsequent retries.
		if err := r.reconnectFn(r.ctx); err == nil {
			r.logger.Printf("✓ Reconnected successfully")
			return
		} else {
			r.logger.Printf("Reconnection attempt %d/%d failed: %v", attempt, reconnectMaxAttempts, err)
			// Propagate a per-IP block to the whole fleet so every client holds
			// off and the block can expire instead of being re-triggered.
			if d, ok := rateLimitBlock(err.Error(), reconnectBlockDefault); ok {
				_ = r.gate.RecordBlock(d)
			}
		}

		if attempt == reconnectMaxAttempts {
			break
		}
		// Exponential backoff, capped so a burst stays brief.
		backoff := min(unit<<uint(min(attempt, 5)), SleepReconnect)
		select {
		case <-r.ctx.Done():
			r.logger.Printf("Context cancelled, stopping reconnection attempts")
			return
		case <-time.After(backoff):
		}
	}

	r.logger.Printf("Reconnect burst gave up after %d attempts; will retry on next command", reconnectMaxAttempts)
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
		url:      url,
		username: username,
		password: password,
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
		router:             newResponseRouter(),
		inflight:           newInflight(16),
		actionLocks:        newActionLockMap(),
		debugLogger:        debugLogger,
		debugOut:           debugLogger.Writer(),
		debugPayloadMaxLen: 200,
		latestListings:     make([]MarketListing, 0),
		latestShips:        make(map[string]any),
		latestRawJSON:      make(map[string][]byte),
		lastError:          make(map[string]any),
		pingInterval:       SleepWSHealthCheck,
		pongTimeout:        SleepWSPongTimeout,
		stopPing:           make(chan struct{}),
		goroutineCtx:       goroutineCtx,
		goroutineCancel:    goroutineCancel,
	}
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

// SetOnCraftingUpdate registers a callback that fires when a crafting_update
// push event is received (server v0.389.0+). This lets consumers track async
// crafting job progress and storage deposits without polling.
func (c *Client) SetOnCraftingUpdate(fn func(ev serverapi.CraftingUpdateEvent)) {
	c.onCraftingMu.Lock()
	defer c.onCraftingMu.Unlock()
	c.onCraftingUpdate = fn
}

// SetPlayerObserver registers a callback that fires when handleResponse
// parses a payload containing player records. Used by consumers (play_as
// REPL, agent runners) to persist sightings into a knowledge base.
func (c *Client) SetPlayerObserver(fn PlayerObserver) {
	c.playerObserverMu.Lock()
	defer c.playerObserverMu.Unlock()
	c.playerObserver = fn
}

// SetPassengerObserver registers a callback that fires when handleResponse
// parses a payload containing passenger records. Used by consumers (play_as
// REPL, agent runners) to persist the passenger catalog into a knowledge base.
func (c *Client) SetPassengerObserver(fn PassengerObserver) {
	c.passengerObserverMu.Lock()
	defer c.passengerObserverMu.Unlock()
	c.passengerObserver = fn
}

// SetDebugLogging controls whether the game client logs WebSocket messages.
// When disabled, the debug logger output is discarded; when enabled, it is
// restored to the writer the logger was constructed with. Safe to call at
// runtime to toggle debug logging on and off.
func (c *Client) SetDebugLogging(enabled bool) {
	if enabled {
		out := c.debugOut
		if out == nil {
			// Client was constructed without NewClient (e.g. a struct literal
			// in tests); leave the logger pointed at its current writer.
			out = c.debugLogger.Writer()
		}
		c.debugLogger.SetOutput(out)
	} else {
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

// SetQuietEventTypes replaces the set of server response types whose
// receive-side debug logging is suppressed. Pass nil or an empty slice to
// clear. Type names are matched verbatim against protocol.Response.Type
// (e.g. "mining_yield"). State updates and downstream handlers still run
// normally; only the "=== Game Client Receive Debug ===" block and the
// matching "Stored raw JSON for ..." line are silenced.
func (c *Client) SetQuietEventTypes(types []string) {
	c.quietEventMu.Lock()
	defer c.quietEventMu.Unlock()
	if len(types) == 0 {
		c.quietEventTypes = nil
		return
	}
	set := make(map[string]struct{}, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t != "" {
			set[t] = struct{}{}
		}
	}
	c.quietEventTypes = set
}

// isQuietEventType reports whether resp.Type debug logging should be skipped.
// Safe to call before SetQuietEventTypes; an unconfigured set returns false.
func (c *Client) isQuietEventType(t string) bool {
	c.quietEventMu.RLock()
	defer c.quietEventMu.RUnlock()
	if c.quietEventTypes == nil {
		return false
	}
	_, ok := c.quietEventTypes[t]
	return ok
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
			// Enable permessage-deflate to cut bandwidth on the repetitive
			// JSON game-state stream. ContextTakeover reuses a 32 KB sliding
			// window for the best ratio; it falls back to NoContextTakeover,
			// then to disabled, if the server does not advertise support.
			CompressionMode: websocket.CompressionContextTakeover,
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

		// Resync authoritative state after the reconnect. A mid-run drop — the
		// daily server-restart is the common cause — leaves our cached State stale,
		// in particular Traveling and CurrentPOI, which the standing behaviors gate
		// on. The login payload does not refresh them, so a worker that was dropped
		// mid-jump would come back and act on a stale location (the path that
		// stranded the shuttle). Reconnect is only ever the "already running"
		// reconnect path, so this is the natural place to pull a fresh get_system
		// (location/traveling) and get_status (wallet/hull/fuel) before handing
		// control back, letting the next standing pass run its stranded-recovery
		// against reality. Best-effort: a resync failure must not fail an otherwise
		// good reconnect — the next standing pass refreshes state anyway.
		if err := c.GetSystem(ctx); err != nil {
			c.debugLogger.Printf("post-reconnect get_system resync failed: %v", err)
		}
		if err := c.GetStatus(ctx); err != nil {
			c.debugLogger.Printf("post-reconnect get_status resync failed: %v", err)
		}

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

// RequestReconnect wakes the auto-reconnect handler to start a fresh reconnection
// burst if the client is disconnected and the handler has gone dormant after
// exhausting an earlier burst. It is a no-op (returning false) when connected,
// when a burst is already running, or when no *ReconnectingHandler is installed.
// Intended to be called from an interactive loop on user input.
func (c *Client) RequestReconnect() bool {
	c.mu.Lock()
	h := c.handler
	c.mu.Unlock()
	if rh, ok := h.(*ReconnectingHandler); ok {
		return rh.TriggerReconnect()
	}
	return false
}

// Send is a thin public wrapper around the private send primitive. It
// exists only for the small set of callers that legitimately need
// fire-and-forget without response correlation:
//
//   - pkg/observe/session.go (generic relay: responses are dispatched
//     to a WS client by message type, not correlated to a specific
//     command — incompatible with Submit's exclusive subscription).
//   - cmd/tools/server-cmd and cmd/debug/play-simple (debug REPLs
//     that manage their own response handlers and intentionally
//     collect zero-or-many unsolicited responses).
//
// All other code MUST use Submit / subscribePush. Direct Send loses
// response correlation, the per-action lock, and the in-flight cap.
func (c *Client) Send(ctx context.Context, msg protocol.Message) error {
	return c.send(ctx, msg)
}

// send is the private wire primitive used by Submit (and the public
// Send shim). It performs IP rate-limit gating, marshals, and writes
// to the websocket. Test hook: sendOverride short-circuits this entirely.
func (c *Client) send(ctx context.Context, msg protocol.Message) error {
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
	if msg.RequestID != "" {
		c.debugLogger.Printf("Message RequestID: '%s'", msg.RequestID)
	}
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
	resp, err := c.await(ctx, h)
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
	resp, err := c.await(ctx, h)
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
	if _, err := c.await(ctx, h); err != nil {
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
	if err := c.send(ctx, msg); err != nil {
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
	if sendErr := c.send(ctx, msg); sendErr != nil {
		return protocol.Response{}, sendErr
	}
	return c.waitForInitialResponse(ctx, timeout)
}

// Travel travels to a POI within the current system
// Travel travels to a POI within the current system.
// It blocks until the ship arrives at the destination or an error occurs.
// The returned TravelResult contains the final POI the ship ended up at.
// arrivalWaitTimeout returns how long to wait for a travel or jump to complete,
// given the server-reported arrival tick and our best guess at the current tick.
//
// This is a SAFETY bound, not a precise deadline: waitForStateChange returns the
// instant the ship stops traveling, so the timeout only matters when the ship
// never arrives. The start reference comes from the free-running GameClock,
// whose periodic sync only ever moves the tick FORWARD (never back), so it can
// drift ahead of the true server tick. When it does, arrivalTick-startTick
// under-estimates — and can collapse to <=0 — which previously produced a
// 1*SleepTick+30s = 40s wait that timed out mid-journey. We therefore floor the
// result so a bad estimate can't shorten the wait below a realistic journey, and
// cap it so a far-behind clock can't inflate it.
func arrivalWaitTimeout(arrivalTick, startTick int64, floor, maxWait time.Duration) time.Duration {
	ticksRemaining := arrivalTick - startTick
	if ticksRemaining < 1 {
		ticksRemaining = 1
	}
	timeout := time.Duration(ticksRemaining)*SleepTick + 30*time.Second
	if timeout < floor {
		timeout = floor
	}
	if timeout > maxWait {
		timeout = maxWait
	}
	return timeout
}

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

	// Compute the arrival-wait safety bound from arrival_tick. The travel ACK
	// carries no current-tick field, so startTick comes from the free-running
	// GameClock, which can drift AHEAD of the server (its sync only moves
	// forward) — see arrivalWaitTimeout for why we floor the result.
	var arrivalTick int64
	if at, ok := resp.Payload["arrival_tick"].(float64); ok {
		arrivalTick = int64(at)
	}
	timeout := arrivalWaitTimeout(arrivalTick, startTick, 9*SleepTick, SleepTravelMaxWait)

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

	// Compute the arrival-wait safety bound from arrival_tick. As with Travel,
	// the start reference is the free-running GameClock (which can drift ahead),
	// so arrivalWaitTimeout floors the result to avoid a too-short wait.
	var arrivalTick int64
	if at, ok := resp.Payload["arrival_tick"].(float64); ok {
		arrivalTick = int64(at)
	}
	timeout := arrivalWaitTimeout(arrivalTick, c.currentTick(), 9*SleepTick, SleepJumpMaxWait)

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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
	resp, err := c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
	}
	return maybeGoalReached("refuel", err)
}

// RefuelShip transfers fuel from this ship to target's ship (ship-to-ship,
// needs a refuel_rig fitted). quantity <= 0 lets the server pick its default.
func (c *Client) RefuelShip(ctx context.Context, target string, quantity int) error {
	payload := map[string]any{"target": target}
	if quantity > 0 {
		payload["quantity"] = quantity
	}
	msg := protocol.Message{
		Type:      "refuel",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
		_, err = c.await(ctx, h)
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
			// Skip logging for noisy poi_arrival and poi_departure messages,
			// plus any types installed via SetQuietEventTypes (e.g. mining_yield
			// when the operator launched mining drones that emit a yield push
			// every tick).
			if resp.Type != "poi_arrival" && resp.Type != "poi_departure" && !c.isQuietEventType(resp.Type) {
				c.debugLogger.Printf("=== Game Client Receive Debug ===")
				c.debugLogger.Printf("Response Type: '%s'", resp.Type)
				if resp.RequestID != "" {
					c.debugLogger.Printf("Response RequestID: '%s'", resp.RequestID)
				}
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

			// Update state before notifying subscribers, so state is current
			// when waitForInitialResponse returns.
			c.handleResponse(resp)

			// Fan out through the response router. Runs after state
			// parsers so callers reading State inside their response
			// handler see fresh data.
			if c.router != nil {
				c.router.dispatch(resp)
			}

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
		// Side-channel auto-dock/auto-undock notifications. The server emits
		// these as plain OK frames (carrying the issuing command's request_id
		// so they're traceable) when an action requires the ship to be in a
		// different dock state — e.g. `craft` auto-docks at the nearest station.
		// They are NOT the command's terminal response; the action_result
		// still arrives separately. Update state.Doc here so callers see the
		// correct dock status, regardless of whether anyone is listening on
		// the request handle's ack channel.
		if t, _ := resp.Payload["type"].(string); t == "auto_dock" || t == "auto_undock" {
			c.mu.Lock()
			c.state.Doc = (t == "auto_dock")
			c.mu.Unlock()
			c.debugLogger.Printf("State: %s (request_id=%s)", t, resp.RequestID)
		}
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
		// get_battle_status carries participants/sides/battle_id but NOT an
		// "action" key (confirmed by capture), so this — the only writer of
		// State.BattleState — has to be shape-gated. An empty battle picture
		// reads as "the fight is over" to everything downstream.
		if isBattleStatusPayload(resp.Payload) {
			c.parseBattleStatusData(resp.Payload)
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

	case protocol.TypeCraftingUpdate:
		// Async crafting progress push (server v0.389.0+). Output items are
		// deposited to station/faction storage server-side. Decode and fire the
		// OnCraftingUpdate callback so consumers can track job progress.
		var ev serverapi.CraftingUpdateEvent
		if data, err := json.Marshal(resp.Payload); err == nil {
			if err := json.Unmarshal(data, &ev); err == nil {
				c.debugLogger.Printf("[CRAFTING_UPDATE] tick=%d jobs=%d", ev.Tick, len(ev.Jobs))
				c.onCraftingMu.RLock()
				cb := c.onCraftingUpdate
				c.onCraftingMu.RUnlock()
				if cb != nil {
					cb(ev)
				}
			}
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

	case protocol.TypeFacilityRentWarning:
		// Faction facility rent is overdue. If left unpaid for grace_cycles
		// consecutive cycles the station repossesses the facilities, so surface
		// it loudly rather than burying it in the debug log only.
		if msg, ok := resp.Payload["message"].(string); ok && msg != "" {
			c.debugLogger.Printf("🏚️  FACILITY RENT WARNING: %s", msg)
		} else {
			c.debugLogger.Printf("🏚️  FACILITY RENT WARNING: %v", resp.Payload)
		}

	case protocol.TypeServerRestartWarning:
		// The server announces an imminent restart (a version deploy), e.g.
		//   {"message":"Server restarting in 60s ...",
		//    "seconds_until_restart":60,"target_version":"0.485.0"}
		// The disconnect is brief (~10-20s) and workers reconnect on their own,
		// so this is informational — but it is the server telling us a restart
		// is coming, and it is the hook a future graceful pre-restart drain
		// would hang off rather than letting workers eat an EOF.
		//
		// The event has no schema in openapi.json or ws.md, so every field is
		// read defensively with a whole-payload fallback.
		msg, _ := resp.Payload["message"].(string)
		target, _ := resp.Payload["target_version"].(string)
		secs, hasSecs := resp.Payload["seconds_until_restart"].(float64)
		switch {
		case msg != "":
			c.debugLogger.Printf("🔄 SERVER RESTART WARNING: %s", msg)
		case hasSecs:
			c.debugLogger.Printf("🔄 SERVER RESTART WARNING: restarting in %ds (target %s)", int(secs), target)
		default:
			c.debugLogger.Printf("🔄 SERVER RESTART WARNING: %v", resp.Payload)
		}

	case protocol.TypeScanDetected:
		// Always surface scans on stderr (regardless of debug settings) with a
		// high-contrast banner; loudest in lawless space, where a scan often
		// precedes an attack. The raw payload still goes to the debug log.
		c.debugLogger.Printf("👁️  SCAN DETECTED: %v", resp.Payload)
		scanAlertLogger.Print(formatScanAlert(resp.Payload, c.systemIsLawless()))

	case protocol.TypeReconnected:
		c.debugLogger.Printf("Reconnected to ship: %v", resp.Payload)

	case protocol.TypePilotlessShip:
		// Server-initiated notification that a player's ship went pilotless
		// (they disconnected, often mid-combat) and sits adrift at a POI until
		// it expires — a potential salvage/loot target. Informational: another
		// player's ship, not ours, so we don't mutate State. Log for visibility.
		var ev serverapi.PilotlessShip
		if data, err := json.Marshal(resp.Payload); err == nil {
			_ = json.Unmarshal(data, &ev)
		}
		c.debugLogger.Printf("[PILOTLESS SHIP] %s (%s) %s adrift at %s/%s — %d ticks until expire (tick %d)",
			ev.PlayerUsername, ev.PlayerID, ev.ShipClass, ev.SystemID, ev.POIID, ev.TicksRemaining, ev.ExpireTick)

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

		var parts []serverapi.BattleParticipant
		if unmarshalPayloadKey(resp.Payload, "participants", &parts) {
			c.notifyPlayersFromBattle("combat_update", parts)
		}

	case protocol.TypeBattleAlert:
		// Informational: someone else's battle is starting in the same
		// system. We don't mutate our own state — the alert doesn't mean
		// we're participating. Just log for visibility.
		msg, _ := resp.Payload["message"].(string)
		battleID, _ := resp.Payload["battle_id"].(string)
		systemID, _ := resp.Payload["system_id"].(string)
		c.debugLogger.Printf("[BATTLE ALERT] %s (battle=%s system=%s)", msg, battleID, systemID)

		var parts []serverapi.BattleParticipant
		if unmarshalPayloadKey(resp.Payload, "participants", &parts) {
			c.notifyPlayersFromBattle("battle_alert", parts)
		}

	case protocol.TypeBattleStarted:
		// A tactical battle this player is participating in has begun. Unlike
		// battle_alert (someone else's battle), we are a combatant — mark our
		// combat/battle state. The flags are cleared by the next state_update
		// carrying in_combat:false.
		var ev serverapi.BattleStarted
		if data, err := json.Marshal(resp.Payload); err == nil {
			_ = json.Unmarshal(data, &ev)
		}
		c.mu.Lock()
		c.state.InCombat = true
		c.state.InBattle = true
		c.mu.Unlock()
		c.notifyPlayersFromBattleStart("battle_started", ev.Participants)
		c.debugLogger.Printf("[BATTLE STARTED] battle=%s system=%s participants=%d", ev.BattleID, ev.SystemID, len(ev.Participants))

	case protocol.TypeBattleUpdate:
		// Periodic authoritative snapshot of a battle we are in.
		var ev serverapi.BattleUpdate
		if data, err := json.Marshal(resp.Payload); err == nil {
			_ = json.Unmarshal(data, &ev)
		}
		c.mu.Lock()
		c.state.InCombat = true
		c.state.InBattle = true
		c.mu.Unlock()
		c.notifyPlayersFromBattleUpdate("battle_update", ev.Participants)
		c.debugLogger.Printf("[BATTLE UPDATE] battle=%s tick=%d your_side=%d stance=%s zone=%s target=%s participants=%d",
			ev.BattleID, ev.Tick, ev.YourSideID, ev.YourStance, ev.YourZone, ev.YourTargetID, len(ev.Participants))

	case protocol.TypeBattleDamage:
		// Per-hit combat telemetry. Record damage taken when we are the target.
		var ev serverapi.BattleDamage
		if data, err := json.Marshal(resp.Payload); err == nil {
			_ = json.Unmarshal(data, &ev)
		}
		c.mu.Lock()
		c.state.InCombat = true
		if ev.TargetID != "" && ev.TargetID == c.state.Player.ID && ev.TotalDamage > 0 {
			c.state.LastDamage = ev.TotalDamage
		}
		c.mu.Unlock()
		c.debugLogger.Printf("[BATTLE DAMAGE] %s -> %s: %.0f (%s) hit=%v weapons=%v",
			ev.AttackerName, ev.TargetName, ev.TotalDamage, ev.DamageType, ev.HitSuccess, ev.WeaponsFired)

	case protocol.TypeBattleEnded:
		// A tactical battle we were in has concluded. Clear our combat/battle
		// flags proactively (don't wait for the next state_update) and record
		// the combatants as sightings. winning_side is -1 for a stalemate.
		var ev serverapi.BattleEnded
		if data, err := json.Marshal(resp.Payload); err == nil {
			_ = json.Unmarshal(data, &ev)
		}
		c.mu.Lock()
		c.state.InCombat = false
		c.state.InBattle = false
		c.mu.Unlock()
		c.notifyPlayersFromBattleEnd("battle_ended", ev.Participants)
		c.debugLogger.Printf("[BATTLE ENDED] battle=%s reason=%s winning_side=%d duration=%d ships_destroyed=%d total_damage=%.0f participants=%d",
			ev.BattleID, ev.Reason, ev.WinningSide, ev.Duration, ev.ShipsDestroyed, ev.TotalDamage, len(ev.Participants))

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
				c.notifyPlayerFromChat(chatMsg)
			}
		}
		// No per-message chat log here: listen() already dumps the full
		// Response Payload for every non-quiet frame through debugLogger, and
		// play_as renders the user-facing chat line itself. A "[CHAT] ..."
		// line here would just duplicate both.

	case protocol.TypeSkillLevelUp:
		// Server-initiated notification that a skill leveled up. Refresh the
		// cached level so GetState() is accurate before the next full skill
		// sync. We intentionally do NOT call checkXPChanges() here: the
		// old->new level transition is surfaced by the subsequent player/skill
		// data sync, which compares against the baseline snapshot. Firing the
		// callback here (or advancing that baseline) would either duplicate the
		// notification or hide the transition.
		skillID, _ := resp.Payload["skill_id"].(string)
		skillName, _ := resp.Payload["skill_name"].(string)
		newLevel, _ := resp.Payload["new_level"].(float64)
		xpGained, _ := resp.Payload["xp_gained"].(float64)
		if skillID != "" && newLevel > 0 {
			c.mu.Lock()
			if c.state.Player.Skills == nil {
				c.state.Player.Skills = make(map[string]Skill)
			}
			sk := c.state.Player.Skills[skillID]
			sk.Level = int(newLevel)
			c.state.Player.Skills[skillID] = sk
			c.mu.Unlock()
		}
		label := skillName
		if label == "" {
			label = skillID
		}
		c.debugLogger.Printf("[SKILL LEVEL UP] %s reached level %.0f (+%.0f xp)", label, newLevel, xpGained)

	case protocol.TypeFactionPromote:
		// Server-initiated notification that this player's faction rank changed.
		// Refresh the cached rank so GetState() reflects it before the next
		// faction/player sync.
		factionName, _ := resp.Payload["faction_name"].(string)
		newRole, _ := resp.Payload["new_role"].(string)
		oldRole, _ := resp.Payload["old_role"].(string)
		promotedBy, _ := resp.Payload["promoted_by"].(string)
		if newRole != "" {
			c.mu.Lock()
			c.state.Player.FactionRank = newRole
			c.mu.Unlock()
		}
		c.debugLogger.Printf("[FACTION PROMOTE] %s: %s -> %s (by %s)", factionName, oldRole, newRole, promotedBy)

	case protocol.TypeFactionInvite:
		// Server-initiated notification that this player was invited to a
		// faction. This is an offer, not membership — we do not mutate the
		// player's own faction state until they accept. Log for visibility.
		factionName, _ := resp.Payload["faction_name"].(string)
		factionID, _ := resp.Payload["faction_id"].(string)
		invitedBy, _ := resp.Payload["invited_by"].(string)
		c.debugLogger.Printf("[FACTION INVITE] %s (%s) invited by %s", factionName, factionID, invitedBy)

	case protocol.TypeFactionAllianceProposal:
		// Server-initiated notification that another faction proposed an alliance
		// with ours. An offer, not a formed alliance — ratify out-of-band with
		// faction_accept_ally. Log for visibility; tags arrive space-padded (see
		// the faction_info handler), so trim before display.
		fromFactionName, _ := resp.Payload["from_faction_name"].(string)
		fromFactionID, _ := resp.Payload["from_faction_id"].(string)
		fromFactionTag, _ := resp.Payload["from_faction_tag"].(string)
		fromFactionTag = strings.TrimSpace(fromFactionTag)
		c.debugLogger.Printf("[FACTION ALLIANCE PROPOSAL] %s [%s] (%s) proposes an alliance", fromFactionName, fromFactionTag, fromFactionID)

	case protocol.TypeFactionAllianceFormed:
		// Server-initiated notification that an alliance was ratified (a faction
		// accepted our proposal). Log for visibility; alliance relations are not
		// tracked in State. Tags arrive space-padded, so trim before display.
		withFactionName, _ := resp.Payload["with_faction_name"].(string)
		withFactionID, _ := resp.Payload["with_faction_id"].(string)
		withFactionTag, _ := resp.Payload["with_faction_tag"].(string)
		withFactionTag = strings.TrimSpace(withFactionTag)
		c.debugLogger.Printf("[FACTION ALLIANCE FORMED] alliance formed with %s [%s] (%s)", withFactionName, withFactionTag, withFactionID)

	case protocol.TypeCompleteMission:
		// Server-initiated notification that a mission completed without an
		// explicit complete_mission command — e.g. a distress/escort objective
		// that resolves on arrival. Distinct shape from the command response
		// (CompleteMissionResponse): mission_title + a nested rewards object.
		// We log for visibility and leave credits/XP to the next state sync to
		// avoid double-counting an optimistic apply.
		missionID, _ := resp.Payload["mission_id"].(string)
		title, _ := resp.Payload["mission_title"].(string)
		rewardCredits := 0.0
		var rewardParts []string
		if rewards, ok := resp.Payload["rewards"].(map[string]any); ok {
			rewardCredits, _ = rewards["credits"].(float64)
			if rewardCredits > 0 {
				rewardParts = append(rewardParts, fmt.Sprintf("+%.0f cr", rewardCredits))
			}
			if skillXP, ok := rewards["skill_xp"].(map[string]any); ok {
				skills := make([]string, 0, len(skillXP))
				for skill := range skillXP {
					skills = append(skills, skill)
				}
				sort.Strings(skills)
				for _, skill := range skills {
					if xp, ok := skillXP[skill].(float64); ok {
						rewardParts = append(rewardParts, fmt.Sprintf("%s +%.0f", skill, xp))
					}
				}
			}
		}
		reward := "no rewards"
		if len(rewardParts) > 0 {
			reward = strings.Join(rewardParts, ", ")
		}
		c.debugLogger.Printf("[MISSION COMPLETE] %s (%s) — %s", title, missionID, reward)

	case protocol.TypeAchievementUnlocked:
		// Server-initiated notification that one or more achievements were
		// unlocked (e.g. completing your first jump). Carries an "achievements"
		// array of fully-formed Achievement entries. We log for visibility and
		// leave any earned-count/points roll-up to the next get_achievements or
		// state sync to avoid maintaining a partial cache here.
		var unlocked []serverapi.Achievement
		if unmarshalPayloadKey(resp.Payload, "achievements", &unlocked) {
			for _, a := range unlocked {
				c.debugLogger.Printf("[ACHIEVEMENT UNLOCKED] %s (%s) — %s [+%d pts]",
					a.Name, a.ID, a.Description, a.Points)
			}
		}

	case protocol.TypeShipCommissionComplete:
		// Server-initiated notification that a commissioned ship finished
		// building and is ready for pickup at the commissioning base. We log
		// for visibility and leave the fleet roster to the next list_ships /
		// state sync rather than splicing a partial ship entry in here.
		shipName, _ := resp.Payload["ship_name"].(string)
		shipClass, _ := resp.Payload["ship_class"].(string)
		baseName, _ := resp.Payload["base_name"].(string)
		commissionID, _ := resp.Payload["commission_id"].(string)
		label := shipName
		if label == "" {
			label = shipClass
		}
		c.debugLogger.Printf("[SHIP COMMISSION COMPLETE] %s (%s) ready for pickup at %s [commission %s]",
			label, shipClass, baseName, commissionID)

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
	preservedStandings := c.state.Player.Standings

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

	// Standings ride only on a FULL player payload; a partial one decodes them
	// as nil and would wipe a durable unlock signal (the pirate baseline that
	// gates stronghold access). Same shape as the Skills merge above.
	if c.state.Player.Standings == nil && preservedStandings != nil {
		c.state.Player.Standings = preservedStandings
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

// isBattleStatusPayload reports whether a payload is a get_battle_status
// reply, by shape. Two shapes exist: a live battle (battle_id + participants)
// and the bare "you are not in a battle" answer, which carries only
// is_participant — the second matters because it is how a poll loop learns the
// fight has ended. The action-based route is not usable: the reply has no
// "action" key.
func isBattleStatusPayload(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if action, ok := payload["action"].(string); ok && action == "get_battle_status" {
		return true
	}
	_, hasBattleID := payload["battle_id"]
	_, hasParticipants := payload["participants"]
	if hasBattleID && hasParticipants {
		return true
	}
	_, hasIsParticipant := payload["is_participant"]

	return hasIsParticipant
}

// parseBattleStatusData populates state.BattleState from a get_battle_status
// response. The server reports hull/shield as percentages plus zone/stance and
// per-participant target. This is the structured read the spar harness and the
// future smart battle handler consume; the raw payload also goes to the monitor
// store, but that is unstructured.
func (c *Client) parseBattleStatusData(payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	var resp serverapi.GetBattleStatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}

	battle := BattleStateFromResponse(&resp)
	parts := battle.Participants

	c.mu.Lock()
	defer c.mu.Unlock()
	if resp.BattleID == "" && len(parts) == 0 {
		// A bare "you are not in a battle" reply is what the server sends once
		// the fight is over. Returning here without touching anything left the
		// previous battle picture in place, so a loop polling for the fight to
		// end never saw it end — it kept reading the last live snapshot. Clear
		// the participation flags instead; there is nothing else to update.
		if !resp.IsParticipant {
			c.state.InBattle = false
			if c.state.BattleState != nil {
				c.state.BattleState.IsParticipant = false
			}
		}
		return
	}
	c.state.BattleState = battle
	// Keep InBattle in sync with this authoritative poll: a status showing we
	// are no longer a participant (battle ended) must clear it, not leave it
	// stale for the next consumer (e.g. the future smart battle handler).
	c.state.InBattle = resp.IsParticipant
}

// BattleStateFromResponse converts a get_battle_status reply into the cached
// battle picture. Exported so a caller driving a fight (and a test standing in
// for the server) shares exactly one mapping with the client rather than
// keeping a second, drifting copy.
func BattleStateFromResponse(resp *serverapi.GetBattleStatusResponse) *BattleState {
	if resp == nil {
		return nil
	}
	parts := make([]BattleParticipant, 0, len(resp.Participants))
	for _, p := range resp.Participants {
		parts = append(parts, BattleParticipant{
			PlayerID:     p.PlayerID,
			Username:     p.Username,
			ShipClass:    p.ShipClass,
			ShipName:     p.ShipName,
			Kind:         p.Kind,
			IsNPC:        p.IsNPC,
			SideID:       string(p.SideID),
			Zone:         p.Zone,
			ZoneDistance: p.ZoneDistance,
			Stance:       p.Stance,
			TargetID:     p.TargetID,
			HullPct:      p.HullPct,
			ShieldPct:    p.ShieldPct,
			AutoPilot:    p.AutoPilot,
		})
	}
	st := &BattleState{
		BattleID:      resp.BattleID,
		SystemID:      resp.SystemID,
		IsParticipant: resp.IsParticipant,
		Participants:  parts,
		TickDuration:  resp.TickDuration,
	}
	if cs := resp.CombatState; cs != nil {
		st.CombatState = &CombatState{
			CanEscape:      cs.CanEscape,
			EffectiveSpeed: cs.EffectiveSpeed,
			EMDisrupted:    cs.EMDisrupted,
			FleeCounter:    cs.FleeCounter,
			FleeRequired:   cs.FleeRequired,
			MaxWeaponReach: cs.MaxWeaponReach,
			WarpDisrupted:  cs.WarpDisrupted,
			Webbed:         cs.Webbed,
		}
	}

	return st
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

	// get_system's "empire" field means regional space affiliation, and the
	// server omits it entirely for many systems. A bare non-empty guard lets
	// a previous system's empire value bleed onto the next system once we
	// travel/jump, since the field is then simply never touched again.
	// Determine whether this payload describes a different system than the
	// one currently held BEFORE System.ID is overwritten below, so we know
	// whether to reset Empire outright instead of merging it.
	incomingID := sys.ID
	if incomingID == "" {
		incomingID = sys.Name
	}
	sameSystem := incomingID == "" || incomingID == c.state.System.ID

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
	if sameSystem {
		// Partial update for the same system: don't let an omitted empire
		// field (common — get_system frequently omits it) clear a value we
		// already know.
		if sys.Empire != "" {
			c.state.System.Empire = sys.Empire
		}
	} else {
		// A different system than what we had: the old empire never applies
		// here, so assign unconditionally, including the empty case.
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

	// Some actions auto-undock the ship before executing (e.g. travel/jump
	// issued while docked); the server reports this with a top-level
	// auto_undocked flag. Reflect it in dock state so callers don't keep
	// treating the ship as docked.
	if auto, ok := payload["auto_undocked"].(bool); ok && auto {
		c.state.Doc = false
		c.debugLogger.Printf("Action result: auto-undocked")
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
		// The docked base id lives on the player object, and only a full player
		// payload carries it — a partial one decodes it as "" and clobbers what
		// we had. dock's own reply names the base, so record it here: callers
		// that key off DockedAtBase (handoff passes, station pins) otherwise sit
		// idle believing they are docked nowhere.
		if base, ok := result["base"].(map[string]any); ok {
			if id, ok := base["id"].(string); ok && id != "" {
				c.state.Player.DockedAtBase = id
			}
		}
		if story, ok := result["story"].(string); ok {
			c.state.LastDockStory = story
		}
		c.debugLogger.Printf("Action result: docked at %s", c.state.Player.DockedAtBase)

	case "undock":
		c.state.Doc = false
		c.state.Player.DockedAtBase = ""
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
		// v0.240+: outputs is an array of {item_id, name, quantity, bonus_quantity};
		// recipe is the display name. The top-level "quantity" is the requested batch
		// count, not the produced amount, so output quantities come from the array.
		recipeName, _ := result["recipe"].(string)
		type craftedOutput struct {
			itemID   string
			name     string
			quantity float64
		}
		var outputs []craftedOutput
		if rawOutputs, ok := result["outputs"].([]any); ok {
			for _, raw := range rawOutputs {
				o, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				itemID, _ := o["item_id"].(string)
				name, _ := o["name"].(string)
				qty, _ := o["quantity"].(float64)
				outputs = append(outputs, craftedOutput{itemID: itemID, name: name, quantity: qty})
			}
		}

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

		// Some or all outputs may be delivered straight to station or faction
		// storage rather than ship cargo. Tally the per-item amounts that bypassed
		// cargo so we only credit the remainder to the ship.
		toStorage := make(map[string]float64)
		for _, key := range []string{"to_storage", "to_faction_storage"} {
			delivered, ok := result[key].([]any)
			if !ok {
				continue
			}
			for _, raw := range delivered {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				itemID, _ := item["item_id"].(string)
				qty, _ := item["quantity"].(float64)
				if itemID == "" || qty <= 0 {
					continue
				}
				toStorage[itemID] += qty
			}
		}

		// Add crafted outputs to cargo, minus anything routed to storage.
		for _, out := range outputs {
			if out.itemID == "" || out.quantity <= 0 {
				continue
			}
			cargoQty := out.quantity - toStorage[out.itemID]
			if cargoQty <= 0 {
				continue
			}
			found := false
			for i := range c.state.Ship.Cargo {
				if c.state.Ship.Cargo[i].ItemID == out.itemID {
					c.state.Ship.Cargo[i].Quantity += cargoQty
					found = true
					break
				}
			}
			if !found {
				c.state.Ship.Cargo = append(c.state.Ship.Cargo, CargoItem{
					ItemID:   out.itemID,
					Quantity: cargoQty,
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

		for _, out := range outputs {
			label := out.name
			if label == "" {
				label = out.itemID
			}
			stored := toStorage[out.itemID]
			switch {
			case stored >= out.quantity:
				c.debugLogger.Printf("Action result: crafted %.0f x %s -> storage (recipe: %s)", out.quantity, label, recipeName)
			case stored > 0:
				c.debugLogger.Printf("Action result: crafted %.0f x %s (%.0f to cargo, %.0f to storage) (recipe: %s)", out.quantity, label, out.quantity-stored, stored, recipeName)
			default:
				c.debugLogger.Printf("Action result: crafted %.0f x %s (recipe: %s)", out.quantity, label, recipeName)
			}
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
		case "survey_system":
			// survey_system does not mutate ship/player state; the REPL
			// formatter renders the result. Log cleanly instead of "unhandled".
			c.debugLogger.Printf("Action result: survey complete (%s)", command)
		case "cloak":
			// cloak's result omits the "action" field and carries enabled +
			// cloak_strength. Reflect the toggle in cached player state so
			// GetState() is accurate before the next full player sync.
			if enabled, ok := result["enabled"].(bool); ok {
				c.state.Player.IsCloaked = enabled
				if enabled {
					strength, _ := result["cloak_strength"].(float64)
					c.debugLogger.Printf("Action result: cloak engaged (strength %.0f)", strength)
				} else {
					c.debugLogger.Printf("Action result: cloak disengaged")
				}
			} else {
				c.debugLogger.Printf("Action result: cloak (no enabled flag)")
			}
		case "deploy_drone", "load_drone", "unload_drone", "recall_drone", "upload_drone_script":
			// Drone bay/bandwidth/roster state isn't cached in State; the REPL
			// formatter (and get_drones) renders the result. Log cleanly instead
			// of "unhandled".
			c.debugLogger.Printf("Action result: %s complete", command)
		case "scan":
			// scan reveals a target player's faction_id/username/ship_class.
			// Nothing in our own State changes, but the sighting feeds the
			// player observer (seen_players + faction backfill). currentSystemID
			// would re-lock c.mu (held here), so read CurrentSystem directly.
			scan := serverapi.ScanResponse{}
			scan.TargetID, _ = result["target_id"].(string)
			scan.Username, _ = result["username"].(string)
			scan.ShipClass, _ = result["ship_class"].(string)
			scan.FactionID, _ = result["faction_id"].(string)
			c.notifyPlayerFromScan(scan, c.state.CurrentSystem)
			c.debugLogger.Printf("Action result: scanned %s (%s)", scan.Username, scan.ShipClass)
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
	protocol.TypeTick:                    {},
	protocol.TypeStateUpdate:             {},
	protocol.TypeChatMessage:             {},
	protocol.TypeCombatUpdate:            {},
	protocol.TypeBattleAlert:             {},
	protocol.TypeBattleEnded:             {},
	protocol.TypePirateWarning:           {},
	protocol.TypePoliceWarning:           {},
	protocol.TypePlayerDied:              {},
	protocol.TypeScanDetected:            {},
	protocol.TypeTradeOfferReceived:      {},
	protocol.TypePilotlessShip:           {},
	protocol.TypeReconnected:             {},
	protocol.TypeSkillLevelUp:            {},
	protocol.TypeFactionPromote:          {},
	protocol.TypeFactionInvite:           {},
	protocol.TypeFactionAllianceProposal: {},
	protocol.TypeFactionAllianceFormed:   {},
	protocol.TypeFacilityRentWarning:     {},
	protocol.TypeAchievementUnlocked:     {},
	protocol.TypeCraftingUpdate:          {},
	protocol.TypeServerRestartWarning:    {},
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
			case "get_tax_estimate":
				storeKey = "tax_estimate"
				shouldStore = true
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
			case "list", "get", "accept", "deliver", "return", "cancel", "profile", "track", "pay_debt", "quote", "post":
				// /shipping is action-dispatched; the reply's action is the verb.
				// Namespace the key so it can't collide with other commands' keys.
				// (quote/post included so shipper-side reads land somewhere too, even
				// though Sub-project A ships no quote/post client method yet.)
				//
				// INVARIANT: these are BARE verbs, matched with no check that the
				// reply came from a /shipping request. It holds only because no
				// other command replies with a top-level action of "get", "accept",
				// "list", … today. A future server command that does would land its
				// body under shipping_<verb> and silently corrupt a carrier read —
				// so if one appears, this case must gate on the originating command
				// rather than the verb alone.
				storeKey = "shipping_" + action
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
		// browse_ships: {base_id, base_name, count, listings} with NO action
		// field (facility sub-actions carry the same shape but always include
		// "action"). Dedicated key so ship listings never collide with
		// market/facility "listings" payloads — the old "ships"-key drift
		// silently killed ship-listing capture from 2026-02-18 to 2026-07-04.
		if action, _ := resp.Payload["action"].(string); action == "" || action == "browse_ships" {
			_, hasBaseID := resp.Payload["base_id"]
			_, hasShipListings := resp.Payload["listings"]
			_, hasCount := resp.Payload["count"]
			if hasBaseID && hasShipListings && hasCount && storeKey == "" {
				storeKey = "ship_listings"
				shouldStore = true
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
			// An owned-fleet listing (list_ships) carries NO "action" field —
			// verified live 2026-08-01 — so the action-based switch above never
			// reaches its case "list_ships", and "owned_ships" was unreachable
			// from the day it was added. active_ship_id is the discriminator:
			// only a listing of ships you own names the active hull. Store
			// under BOTH keys, because cmd/auto-trader and
			// cmd/tools/daily-summary read the generic "ships".
			if _, hasActive := resp.Payload["active_ship_id"]; hasActive && storeKey == "" {
				storeKey = "owned_ships"
				extraKeys = append(extraKeys, "ships")
			} else if storeKey == "" {
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
		// Emit player sightings for get_system responses that include an
		// online_players roster. Additive notifier — no storeKey change.
		if _, hasOnline := resp.Payload["online_players"]; hasOnline {
			var players []serverapi.NearbyPlayer
			if unmarshalPayloadKey(resp.Payload, "online_players", &players) {
				c.notifyPlayers("get_system", players, "")
			}
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

				var players []serverapi.NearbyPlayer
				if unmarshalPayloadKey(resp.Payload, "agents", &players) {
					c.notifyPlayers("get_system_agents", players, "")
				}
			}
		}
		// Store commission_quote responses. The server's reply carries no
		// "action" field, so detect by the distinctive can_commission +
		// ship_class pair (commission_ship responses don't include
		// can_commission). Stored under "commission_quote" so the
		// play_as styled formatter can pick it up.
		if _, hasCanCommission := resp.Payload["can_commission"]; hasCanCommission {
			if _, hasShipClass := resp.Payload["ship_class"]; hasShipClass {
				if storeKey == "" {
					storeKey = "commission_quote"
				}
				shouldStore = true
			}
		}
		// Store commission_status responses. The server's reply carries no
		// "action" field; detect by the distinctive "commissions" list (the
		// roster of active/ready commissions). Stored under "commission_status"
		// so the play_as styled formatter can pick it up.
		if _, hasCommissions := resp.Payload["commissions"]; hasCommissions {
			if storeKey == "" {
				storeKey = "commission_status"
			}
			shouldStore = true
		}
		// Store passenger feature responses. None carry an "action" field, so
		// detect each by a distinctive payload key and store under the command
		// name so play_as's styled formatters (and other lookups) can find them:
		//   list_station_passengers → "waiting",  list_passengers → "passengers",
		//   load_passenger → "total_fare",        unload_passenger → "fare_collected".
		if storeKey == "" {
			if _, ok := resp.Payload["waiting"]; ok {
				storeKey = "list_station_passengers"
				shouldStore = true
			} else if _, ok := resp.Payload["passengers"]; ok {
				storeKey = "list_passengers"
				shouldStore = true
			} else if _, ok := resp.Payload["total_fare"]; ok {
				storeKey = "load_passenger"
				shouldStore = true
			} else if _, ok := resp.Payload["fare_collected"]; ok {
				storeKey = "unload_passenger"
				shouldStore = true
			}
		}
		// Emit passenger sightings into the catalog. Additive — independent of
		// storeKey. list_station_passengers (waiting) is the richest source
		// (carries empire + bio); the others fill gaps via COALESCE merge.
		c.notifyPassengersFromPayload(resp.Payload)
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
			case "browse_for_sale", "list_for_sale", "buy_listing", "cancel_listing":
				// These facility sub-actions can carry a "listings" field, so the
				// content-shape probe above keys them as "listings". Also store
				// under "facility" so play_as's facility-command lookup finds the
				// fresh response instead of a stale `facility list`.
				if storeKey == "" {
					storeKey = "facility"
				} else if storeKey != "facility" {
					extraKeys = append(extraKeys, "facility")
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
		// Store get_battle_status by SHAPE, not by action. A capture of the
		// real reply (2026-08-08) has NO "action" key at all, so the action
		// case further down never fires for this command and the key was
		// simply never written — the browse_ships/owned_ships failure again,
		// this time confirmed rather than suspected. A battle loop reading an
		// empty "battle_status" concludes the fight is over before it began.
		if isBattleStatusPayload(resp.Payload) {
			if storeKey == "" {
				storeKey = "battle_status"
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
		// Store single drone details (get_drone). The query reply has no
		// "drones" array; identify it by the drone-specific "loaded_at" field
		// paired with "id" so it doesn't collide with other id-bearing payloads.
		if storeKey == "" {
			_, hasID := resp.Payload["id"]
			_, hasLoadedAt := resp.Payload["loaded_at"]
			if hasID && hasLoadedAt {
				storeKey = "get_drone"
				shouldStore = true
			}
		}
		// Store get_location response. Payload wraps everything under a single
		// "location" object alongside a "message" string. Keyed under "location"
		// so the REPL's rawJSONKeyForCommand["get_location"] mapping finds it.
		if _, hasLocation := resp.Payload["location"]; hasLocation {
			if storeKey == "" {
				storeKey = "location"
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
		// When a faction_info payload describes the worker's OWN membership,
		// capture the readable tag into player state. The tag is not present in
		// any player payload (only the faction_id hash is), so this is the sole
		// source workers have for rendering "agent (TAG)" on the status page.
		if storeKey == "faction_info" {
			isMember, _ := resp.Payload["is_member"].(bool)
			tag, _ := resp.Payload["tag"].(string)
			// Tags are a fixed 4 chars, so short names arrive padded (" DB ").
			// Trimming is for display and loses the canonical form; never compare
			// a trimmed tag against a server value.
			tag = strings.TrimSpace(tag)
			if isMember || tag != "" {
				id, _ := resp.Payload["id"].(string)
				c.mu.Lock()
				if tag != "" {
					c.state.Player.FactionTag = tag
				}
				if id != "" && c.state.Player.FactionID == "" {
					c.state.Player.FactionID = id
				}
				c.mu.Unlock()
			}
		}
		// Store faction intel status (faction_intel_status). Live payload has
		// no "action" field; key on the distinctive pois_known stat so the
		// REPL formatter can find it (faction_trade_intel_status uses
		// unique_items/unique_stations instead).
		if storeKey == "" {
			if _, hasPOIsKnown := resp.Payload["pois_known"]; hasPOIsKnown {
				storeKey = "faction_intel_status"
				shouldStore = true
			}
		}
		// Store faction intel query results (faction_query_intel). No "action"
		// field; both query-intel commands carry an "entries" array, so key on
		// entries + the "count" field that only faction_query_intel returns
		// (faction_query_trade_intel uses "showing" instead). Stored under
		// "faction_intel" to match the MCP client's cacheResultAs key.
		if storeKey == "" {
			_, hasEntries := resp.Payload["entries"]
			_, hasCount := resp.Payload["count"]
			if hasEntries && hasCount {
				storeKey = "faction_intel"
				shouldStore = true
			}
		}
		// Store faction invites list (faction_get_invites). Distinctive
		// "invites" array; keyed by command name so play_as's lookup finds it.
		if _, hasInvites := resp.Payload["invites"]; hasInvites {
			if storeKey == "" {
				storeKey = "faction_get_invites"
			}
			shouldStore = true
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

			var players []serverapi.NearbyPlayer
			if unmarshalPayloadKey(resp.Payload, "nearby", &players) {
				poiID, _ := resp.Payload["poi_id"].(string)
				c.notifyPlayers("get_nearby", players, poiID)
			}
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
		// Store completed mission history (from completed_missions).
		// Content-based detection, because an action-keyed case alone is not
		// enough: the captured get_battle_status reply carries no "action" at
		// all, and a key that is only reachable through the action switch is
		// silently dead for any command whose reply omits it.
		//
		// The discriminator is a POSITIVE marker on the entries —
		// completion_time — and not the container's own fields. Container
		// fields cannot tell these lists apart: the active-missions reply
		// carries "missions" and "total_count" too, and its "max_missions" is
		// omitempty, so one omitted field would make it indistinguishable. A
		// consumer of this key grants a difficulty-cap exemption on what it
		// finds here, so a merely ACCEPTED mission must never reach it.
		//
		// Unverified against a real reply — no completed_missions capture
		// exists. An empty list is not stored at all, so a reader must treat
		// an empty result as "unknown", never as "nothing completed".
		if missions, ok := resp.Payload["missions"].([]any); ok && len(missions) > 0 {
			if first, isObj := missions[0].(map[string]any); isObj {
				if _, completed := first["completion_time"]; completed {
					if storeKey == "" {
						storeKey = "completed_missions"
					}
					shouldStore = true
				}
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
		// Store achievements data (from get_achievements response)
		// Content-based detection: has "achievements" array
		if _, hasAchievements := resp.Payload["achievements"]; hasAchievements {
			if storeKey == "" {
				storeKey = "achievements"
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
			case "completed_missions":
				if storeKey == "" {
					storeKey = "completed_missions"
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
		// Tick-deferred /shipping mutations (accept, deliver, return,
		// cancel, post, pay_debt) reply here shaped
		// {command:"shipping", tick, result:{action, ...}} with no
		// top-level "action", so the generic command-name store below
		// only preserves the wrapper under "shipping". Callers need the
		// INNER result body under "shipping_<action>" — the same key the
		// synchronous reads use — so they can decode serverapi structs
		// directly with no unwrapping. Decoding the wrapper instead
		// succeeds with every field zero, which is why this failed as an
		// empty contract rather than a decode error — the same trap craft
		// hit (see pkg/worker/craft_node.go unwrapActionResult).
		if cmd, _ := resp.Payload["command"].(string); cmd == "shipping" {
			if result, ok := resp.Payload["result"].(map[string]any); ok {
				if action, ok := result["action"].(string); ok && action != "" {
					if body, err := json.Marshal(result); err == nil {
						c.rawJSONMu.Lock()
						c.latestRawJSON["shipping_"+action] = body
						c.rawJSONMu.Unlock()
					} else {
						c.debugLogger.Printf("Failed to marshal raw JSON for shipping_%s: %v", action, err)
					}
				}
			}
		}
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
		// Deferred passenger terminals (load_passenger's "loaded", dock's
		// "passenger_arrivals") arrive as action_results — capture them too.
		c.notifyPassengersFromPayload(resp.Payload)
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
	case protocol.TypeError, protocol.TypeActionError:
		// Store the full error payload under a dedicated slot so callers can
		// render the actual error frame (code, message, tick) without falling
		// back to a stale prior payload. Both error types must update this slot:
		// otherwise a later `error` frame (e.g. a deferred "Resources depleted"
		// from mine) would leave _last_error holding the previous command's
		// `action_error`, and the REPL would print that stale error. Don't store
		// under success keys. Single slot is fine because REPL commands are
		// serialized.
		c.rawJSONMu.Lock()
		if jsonData, err := json.Marshal(resp.Payload); err == nil {
			c.latestRawJSON["_last_error"] = jsonData
		}
		c.rawJSONMu.Unlock()
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
		// Suppress the "Stored raw JSON" line for quieted types so silenced
		// pushes (e.g. mining_yield from drones) don't leak through this side
		// channel. Storage itself still happens — only the log line is hidden.
		quiet := c.isQuietEventType(resp.Type)
		if !quiet {
			c.debugLogger.Printf("Stored raw JSON for %s (%d bytes)", storeKey, len(jsonData))
		}

		// Also store under extra keys for cross-referenced data
		for _, key := range extraKeys {
			c.latestRawJSON[key] = jsonData
			if !quiet {
				c.debugLogger.Printf("Stored raw JSON for %s (extra key, %d bytes)", key, len(jsonData))
			}
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

// waitForInitialResponse waits for the first OK or error response from the server.
// Unlike Submit's terminator, it does NOT loop on pending/in-progress — it returns
// the first response and lets the caller decide what to do.
// isAutoDockTransition reports whether an OK payload is the server's automatic
// dock/undock side effect, emitted when a command (travel, jump, mine, buy, …)
// requires a different dock state than the ship is in. Per the API docs the
// server performs the transition automatically, costing one extra tick, and
// flags the frame with auto_docked / auto_undocked. Such a frame is a
// precursor to — not a substitute for — the issuing command's own response,
// so the travel/jump initial-response waiter must look past it.
func isAutoDockTransition(payload map[string]any) bool {
	if v, _ := payload["auto_undocked"].(bool); v {
		return true
	}
	if v, _ := payload["auto_docked"].(bool); v {
		return true
	}
	// Newer servers emit auto-dock notifications as their own OK frame, keyed
	// by a "type" discriminator in the payload (no boolean flag).
	switch t, _ := payload["type"].(string); t {
	case "auto_dock", "auto_undock":
		return true
	}
	// Older servers may omit the flag but still report the transition via the
	// action field. waitForInitialResponse only serves travel/jump, which never
	// legitimately terminate as a dock/undock, so this is safe to skip past.
	switch action, _ := payload["action"].(string); action {
	case "dock", "undock":
		return true
	}
	return false
}

func (c *Client) waitForInitialResponse(ctx context.Context, timeout time.Duration) (protocol.Response, error) {
	// Log the final response paired with the last sent request
	var finalResp *protocol.Response
	defer func() {
		if finalResp != nil {
			c.logCallResponse(*finalResp)
		}
	}()

	respCh := make(chan protocol.Response, 8)
	classifier := func(resp protocol.Response) bool {
		switch resp.Type {
		case protocol.TypeOK, protocol.TypeError, protocol.TypeActionError, protocol.TypeActionResult:
			return true
		}
		return false
	}
	cancel := c.subscribePush(classifier, func(resp protocol.Response) {
		select {
		case respCh <- resp:
		default:
			// buffer full; drop subsequent matches
		}
	})
	defer cancel()

	deadline := time.After(timeout)

	for {
		select {
		case resp := <-respCh:
			switch resp.Type {
			case protocol.TypeOK:
				// An automatic dock/undock side effect is NOT the issuing
				// command's own response. When a travel/jump is sent while
				// docked, the server first auto-undocks (a documented step that
				// costs one extra tick and carries an auto_undocked flag) and
				// only then confirms the travel/jump. Returning the auto-undock
				// frame here would let the caller's waitForStateChange(!Traveling)
				// observe Traveling still false and report a false-positive
				// completion while the real multi-tick action is still queued —
				// the next command then collides with "another action pending".
				// Skip it and keep waiting for the genuine confirmation.
				if isAutoDockTransition(resp.Payload) {
					c.debugLogger.Printf("Auto dock/undock side effect — waiting for the action's own response")
					deadline = time.After(timeout)
					continue
				}
				// If pending, keep waiting for the real initial response.
				if pending, ok := resp.Payload["pending"].(bool); ok && pending {
					c.debugLogger.Printf("Action queued by server — waiting for next-tick execution")
					deadline = time.After(timeout)
					continue
				}
				finalResp = &resp
				return resp, nil

			case protocol.TypeActionResult:
				// action_result arrives when the server processes a pending action
				// on the next tick. Treat it as the initial response.
				c.debugLogger.Printf("Received action_result as initial response")
				finalResp = &resp
				return resp, nil

			case protocol.TypeError:
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

			case protocol.TypeActionError:
				finalResp = &resp
				msg, _ := resp.Payload["message"].(string)
				if msg == "" {
					msg = "action error"
				}
				return resp, fmt.Errorf("%s", msg)
			}

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
