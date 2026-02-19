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
	reconnecting atomic.Bool // Prevents multiple concurrent reconnections
	mu           sync.Mutex  // Protects reconnecting state
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
		r.mu.Lock()
		if !r.reconnecting.Load() {
			r.reconnecting.Store(true)
			go r.attemptReconnection()
		}
		r.mu.Unlock()
	}
}

func (r *ReconnectingHandler) attemptReconnection() {
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
		r.reconnecting.Store(false)
		return
	}

	r.logger.Printf("Failed to reconnect after %d attempts", maxAttempts)
	r.reconnecting.Store(false)
}

// NewClient creates a new game client
func NewClient(url, username, password string, debugLogger *log.Logger) *Client {
	if debugLogger == nil {
		debugLogger = log.New(log.Writer(), "[GAME] ", log.LstdFlags)
	}

	return &Client{
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
		stopCh:         make(chan struct{}),
		readyChan:      make(chan struct{}),
		waiters:        make(map[string]chan protocol.Response),
		debugLogger:    debugLogger,
		latestListings: make([]MarketListing, 0),
		latestShips:    make(map[string]any),
		latestRawJSON:  make(map[string][]byte),
		lastError:      make(map[string]any),
	}
}

// Connect establishes a WebSocket connection to the game server
// Implements retry logic with exponential backoff for rate limiting (429 errors)
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	maxRetries := 5
	baseDelay := 1 * time.Second

	var ws *websocket.Conn
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var resp *http.Response
		ws, resp, err = websocket.Dial(ctx, c.url, nil)
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

	c.debugLogger.Printf("Connected to %s (read limit: 10MB)", c.url)

	// Start message listener
	go c.listen(ctx)

	return nil
}

// Disconnect closes the WebSocket connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.connected = false
		err := c.conn.Close(websocket.StatusNormalClosure, "client disconnect")
		c.conn = nil
		c.debugLogger.Printf("Disconnected from server")
		return err
	}
	return nil
}

// Reconnect disconnects and reconnects to the server
func (c *Client) Reconnect(ctx context.Context) error {
	c.debugLogger.Printf("Attempting to reconnect...")

	// Close existing connection if any
	_ = c.Disconnect()

	// Wait a moment before reconnecting
	time.Sleep(2 * time.Second)

	// Reconnect
	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("reconnect failed: %w", err)
	}

	// Re-authenticate
	if err := c.Login(ctx); err != nil {
		return fmt.Errorf("login after reconnect failed: %w", err)
	}

	c.debugLogger.Printf("Reconnected and logged in successfully")
	return nil
}

// SetHandler sets the message handler
func (c *Client) SetHandler(handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}

// Send sends a message to the game server
func (c *Client) Send(ctx context.Context, msg protocol.Message) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

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

	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
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
func (c *Client) Register(ctx context.Context, empire string) error {
	msg := protocol.Message{
		Type: "register",
		Payload: map[string]any{
			"username": c.username,
			"empire":   empire,
		},
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
	return c.waitForActionResponse(ctx, 5*time.Second)
}

// Dock docks at a station in the current system
func (c *Client) Dock(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "dock",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, 5*time.Second)
}

// Travel travels to a POI within the current system
func (c *Client) Travel(ctx context.Context, targetPOI string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "travel",
		Payload:   map[string]any{"target_poi": targetPOI},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, 5*time.Second)
}

// Jump jumps to another system
func (c *Client) Jump(ctx context.Context, targetSystem string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "jump",
		Payload:   map[string]any{"target_system": targetSystem},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, 5*time.Second)
}

// Mine mines resources at the current location
func (c *Client) Mine(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "mine",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, 5*time.Second)
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
	return c.waitForActionResponse(ctx, 5*time.Second)
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
	return c.waitForActionResponse(ctx, 5*time.Second)
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
	return c.waitForActionResponse(ctx, 5*time.Second)
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

	resp, err := c.waitForAuthResponse(ctx, protocol.TypeOK, 5*time.Second)
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

// GetSystem requests information about the current system
func (c *Client) GetSystem(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_system",
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetMap requests all systems with coordinates and connections
func (c *Client) GetMap(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_map",
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetPOI requests information about the current POI
func (c *Client) GetPOI(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_poi",
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetStatus requests player status
func (c *Client) GetStatus(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_status",
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetListings requests market listings for the current station
func (c *Client) GetListings(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "view_market",
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetShips requests ship listings from the current station
func (c *Client) GetShips(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_ships",
		Timestamp: time.Now().UnixMilli(),
	})
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
	return c.waitForActionResponse(ctx, 5*time.Second)
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
	return c.waitForActionResponse(ctx, 5*time.Second)
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
//	err := client.DepositItems(ctx, "ore_iron", 100.0)
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

	return c.waitForActionResponse(ctx, 5*time.Second)
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
		if item.Quantity > 0 {
			if err := c.DepositItems(ctx, item.ItemID, item.Quantity); err != nil {
				c.debugLogger.Printf("Failed to deposit %s: %v", item.ItemID, err)
				depositErrors++
				// Continue depositing other items even if one fails
			} else {
				c.debugLogger.Printf("Deposited %s x%.0f", item.ItemID, item.Quantity)
				time.Sleep(1 * time.Second) // Brief delay between deposits
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
	return c.waitForActionResponse(ctx, 5*time.Second)
}

// Repair repairs the ship's hull at the current station
func (c *Client) Repair(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "repair",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, 5*time.Second)
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
	return c.waitForActionResponse(ctx, 5*time.Second)
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
	return c.waitForActionResponse(ctx, 5*time.Second)
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
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
		}

		// Get connection reference with mutex protection
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			// Connection closed, exit listener
			return
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()

			c.debugLogger.Printf("Connection error: %v", err)
			c.debugLogger.Printf("Hint: If 'read limited' error, the message exceeded the read limit. Current limit: 10MB")

			// Get handler reference with mutex protection
			c.mu.RLock()
			handler := c.handler
			c.mu.RUnlock()

			if handler != nil {
				handler.OnDisconnected(err)
			}
			return
		}

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

			// Update state before notifying waiters, so state is current
			// when waitForResponse/waitForAuthResponse returns.
			c.handleResponse(resp)

			// Notify any waiters for this response type
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

	case protocol.TypeError:
		c.parseErrorState(resp.Payload)

	case protocol.TypeOK:
		c.parsePlayerData(resp.Payload)
		c.parseShipData(resp.Payload)
		c.parseSystemData(resp.Payload)
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
		// get_ships returns type "ok" with ships in payload
		if _, hasShips := resp.Payload["ships"]; hasShips {
			c.parseShipsData(resp.Payload)
		}
		// get_skills returns type "ok" with player_skills and skills in payload
		if _, hasSkills := resp.Payload["skills"]; hasSkills {
			c.parseSkillsData(resp.Payload)
		}

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

	case protocol.TypePoliceCombat:
		c.debugLogger.Printf("⚔️  POLICE COMBAT: %v", resp.Payload)
	}
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

	// Update docked status: server sends docked_at_base as a string field;
	// present but empty means undocked, non-empty means docked.
	// The json.Unmarshal always includes this field, so we check the raw payload.
	if playerMap, ok := payload["player"].(map[string]any); ok {
		if _, hasDockedField := playerMap["docked_at_base"]; hasDockedField {
			c.state.Doc = player.DockedAtBase != ""
		}
	}

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
		} else {
			c.debugLogger.Printf("No system data found in payload (hasID=%v, hasName=%v, hasPOIs=%v)", hasID, hasName, hasPOIs)
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

	if errMsg, ok := payload["message"].(string); ok {
		if containsIgnoreCase(errMsg, []string{"already undocked", "not docked", "ship is not docked"}) {
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

// Close closes the connection
func (c *Client) Close() error {
	close(c.stopCh)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Close(websocket.StatusNormalClosure, "")
		c.connected = false
		return err
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
			}
		}

		// Fall through to content-based detection for responses without "action"
		// or to add extra storage keys for responses that contain nested data.

		// Store full status response (Player, Ship, System, POI, etc.)
		if storeKey == "" {
			if _, hasPlayer := resp.Payload["Player"]; hasPlayer {
				storeKey = "status"
				shouldStore = true
			} else if _, hasUsername := resp.Payload["Username"]; hasUsername {
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
		// Store map data (from get_map response)
		if _, hasSystems := resp.Payload["systems"]; hasSystems {
			if storeKey == "" {
				storeKey = "systems"
			}
			shouldStore = true
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

	c.waiterMu.Lock()
	c.waiters[protocol.TypeOK] = okChan
	c.waiters[protocol.TypeError] = errorChan
	c.waiterMu.Unlock()

	defer func() {
		c.waiterMu.Lock()
		delete(c.waiters, protocol.TypeOK)
		delete(c.waiters, protocol.TypeError)
		c.waiterMu.Unlock()
	}()

	select {
	case <-okChan:
		return nil
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
				c.debugLogger.Printf("Insufficient crafting skill")
				return fmt.Errorf("insufficient skill level for this recipe")

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
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for action response")
	case <-ctx.Done():
		return ctx.Err()
	}
}
