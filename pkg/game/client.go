package game

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/internal/protocol"
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
	client  *Client
	handler MessageHandler
	ctx     context.Context
	logger  *log.Logger
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

	// Attempt reconnection in background
	go r.attemptReconnection()
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
		return
	}

	r.logger.Printf("Failed to reconnect after %d attempts", maxAttempts)
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
				Connections: []string{},
			},
			Nearby:   []NearbyPlayer{},
			InCombat: false,
		},
		stopCh:         make(chan struct{}),
		readyChan:      make(chan struct{}),
		waiters:        make(map[string]chan protocol.Response),
		debugLogger:    debugLogger,
		latestListings: make([]MarketListing, 0),
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
		ws, _, err = websocket.Dial(ctx, c.url, nil)
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
	c.debugLogger.Printf("Full JSON being sent to WebSocket: %s", string(data))

	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		c.debugLogger.Printf("ERROR sending to WebSocket: %v", err)
		return fmt.Errorf("failed to send message: %w", err)
	}

	c.debugLogger.Printf("WebSocket write successful")
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

// GetSystem requests information about the current system
func (c *Client) GetSystem(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_system",
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
		Type:      "get_listings",
		Timestamp: time.Now().UnixMilli(),
	})
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
				time.Sleep(1 * time.Second) // Small delay between sells
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
		"ore_",       // All ores (ore_iron, ore_copper, etc.)
		"gas_",       // Gases
		"crystal_",   // Crystals
		"salvage_",   // Salvage materials
		"scrap_",     // Scrap materials
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
	return c.state
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

		_, data, err := c.conn.Read(ctx)
		if err != nil {
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()

			c.debugLogger.Printf("Connection error: %v", err)
			c.debugLogger.Printf("Hint: If 'read limited' error, the message exceeded the read limit. Current limit: 10MB")
			if c.handler != nil {
				c.handler.OnDisconnected(err)
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

				// Truncate state_update messages to reduce log clutter
				if resp.Type == "state_update" && len(payloadStr) > 200 {
					c.debugLogger.Printf("Response Payload: %s... [truncated]", payloadStr[:200])
				} else {
					c.debugLogger.Printf("Response Payload: %s", payloadStr)
				}
			}
			// Check for error message in payload
			if msg, ok := resp.Payload["message"]; ok {
				c.debugLogger.Printf("Response Message: '%v'", msg)
			}

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

			// Update state
			c.handleResponse(resp)

			// Notify handler for each decoded message
			if c.handler != nil {
				c.handler.OnMessage(resp)
			}
		}
	}
}

// handleResponse updates the game state based on server responses
func (c *Client) handleResponse(resp protocol.Response) {
	c.state.Mu.Lock()
	defer c.state.Mu.Unlock()

	switch resp.Type {
	case protocol.TypeWelcome:
		if tick, ok := resp.Payload["current_tick"].(float64); ok {
			c.state.CurrentTick = int64(tick)
		}
		if version, ok := resp.Payload["version"].(string); ok {
			c.state.ServerVersion = version
			c.debugLogger.Printf("Server version: %s", version)
		}

	case protocol.TypeRegistered:
		payloadJSON, _ := json.Marshal(resp.Payload)
		c.debugLogger.Printf("[RECVD] payload: %s", string(payloadJSON))
		// Support both 'password' (new API) and 'token' (legacy) for backward compatibility
		if password, ok := resp.Payload["password"].(string); ok {
			c.state.Password = password
			c.password = password
		} else if token, ok := resp.Payload["token"].(string); ok {
			// Legacy support: token field
			c.state.Password = token
			c.password = token
		}

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
		c.parseTravelAction(resp.Payload)
		// get_listings returns type "ok" with listings in payload
		if _, hasListings := resp.Payload["listings"]; hasListings {
			c.parseListingsData(resp.Payload)
		}

	case protocol.TypeDocked:
		c.state.Doc = true
		c.state.Traveling = false
		c.state.TravelProgress = nil

	case protocol.TypeUndocked:
		c.state.Doc = false

	case protocol.TypeStateUpdate:
		if tick, ok := resp.Payload["tick"].(float64); ok {
			c.state.CurrentTick = int64(tick)
		}
		c.parsePlayerData(resp.Payload)
		c.parseShipData(resp.Payload)
		c.parseTravelProgress(resp.Payload)
		c.parseNearbyPlayers(resp.Payload)

	case protocol.TypeTick:
		if tick, ok := resp.Payload["tick"].(float64); ok {
			c.state.CurrentTick = int64(tick)
		}

	case protocol.TypeListings:
		c.parseListingsData(resp.Payload)
	}
}

// parsePlayerData extracts player information from payload
func (c *Client) parsePlayerData(payload map[string]any) {
	if playerData, ok := payload["player"].(map[string]any); ok {
		// Parse basic fields
		if id, ok := playerData["id"].(string); ok {
			c.state.Player.ID = id
		}
		if username, ok := playerData["username"].(string); ok {
			c.state.Player.Username = username
			c.state.Username = username
		}
		if empire, ok := playerData["empire"].(string); ok {
			c.state.Player.Empire = empire
		}
		if credits, ok := playerData["credits"].(float64); ok {
			c.state.Player.Credits = credits
			c.state.Credits = credits
		}
		if currentSystem, ok := playerData["current_system"].(string); ok {
			c.state.Player.CurrentSystem = currentSystem
			c.state.CurrentSystem = currentSystem
		}
		if currentPoi, ok := playerData["current_poi"].(string); ok {
			c.state.Player.CurrentPOI = currentPoi
			c.state.CurrentPOI = currentPoi
			c.state.System.ShipPOI = currentPoi
		}
		if currentShipID, ok := playerData["current_ship_id"].(string); ok {
			c.state.Player.CurrentShipID = currentShipID
		}
		if homeBase, ok := playerData["home_base"].(string); ok {
			c.state.Player.HomeBase = homeBase
		}
		if dockedAtBase, ok := playerData["docked_at_base"].(string); ok {
			c.state.Player.DockedAtBase = dockedAtBase
		}
		if factionID, ok := playerData["faction_id"].(string); ok {
			c.state.Player.FactionID = factionID
		}
		if factionRank, ok := playerData["faction_rank"].(string); ok {
			c.state.Player.FactionRank = factionRank
		}
		if statusMessage, ok := playerData["status_message"].(string); ok {
			c.state.Player.StatusMessage = statusMessage
		}
		if clanTag, ok := playerData["clan_tag"].(string); ok {
			c.state.Player.ClanTag = clanTag
		}
		if primaryColor, ok := playerData["primary_color"].(string); ok {
			c.state.Player.PrimaryColor = primaryColor
		}
		if secondaryColor, ok := playerData["secondary_color"].(string); ok {
			c.state.Player.SecondaryColor = secondaryColor
		}
		if anonymous, ok := playerData["anonymous"].(bool); ok {
			c.state.Player.Anonymous = anonymous
		}

		// Parse skills
		if skills, ok := playerData["skills"].(map[string]any); ok {
			c.state.Player.Skills = make(map[string]Skill)
			for skillID, skillData := range skills {
				if skillMap, ok := skillData.(map[string]any); ok {
					skill := Skill{}
					if level, ok := skillMap["level"].(float64); ok {
						skill.Level = int(level)
					}
					if xp, ok := skillMap["xp"].(float64); ok {
						skill.XP = xp
					}
					c.state.Player.Skills[skillID] = skill
				}
			}
		}

		// Parse stats
		if stats, ok := playerData["stats"].(map[string]any); ok {
			c.parsePlayerStats(stats)
		}
	}
}

// parsePlayerStats extracts player statistics
func (c *Client) parsePlayerStats(stats map[string]any) {
	if shipsDestroyed, ok := stats["ships_destroyed"].(float64); ok {
		c.state.Player.Stats.ShipsDestroyed = int(shipsDestroyed)
	}
	if timesDestroyed, ok := stats["times_destroyed"].(float64); ok {
		c.state.Player.Stats.TimesDestroyed = int(timesDestroyed)
	}
	if oreMined, ok := stats["ore_mined"].(float64); ok {
		c.state.Player.Stats.OreMined = oreMined
	}
	if creditsEarned, ok := stats["credits_earned"].(float64); ok {
		c.state.Player.Stats.CreditsEarned = creditsEarned
	}
	if creditsSpent, ok := stats["credits_spent"].(float64); ok {
		c.state.Player.Stats.CreditsSpent = creditsSpent
	}
	if tradesCompleted, ok := stats["trades_completed"].(float64); ok {
		c.state.Player.Stats.TradesCompleted = int(tradesCompleted)
	}
	if systemsDiscovered, ok := stats["systems_discovered"].(float64); ok {
		c.state.Player.Stats.SystemsDiscovered = int(systemsDiscovered)
	}
	if itemsCrafted, ok := stats["items_crafted"].(float64); ok {
		c.state.Player.Stats.ItemsCrafted = int(itemsCrafted)
	}
	if missionsCompleted, ok := stats["missions_completed"].(float64); ok {
		c.state.Player.Stats.MissionsCompleted = int(missionsCompleted)
	}
}

// parseShipData extracts ship information from payload
func (c *Client) parseShipData(payload map[string]any) {
	if shipData, ok := payload["ship"].(map[string]any); ok {
		// Parse basic fields
		if id, ok := shipData["id"].(string); ok {
			c.state.Ship.ID = id
		}
		if ownerID, ok := shipData["owner_id"].(string); ok {
			c.state.Ship.OwnerID = ownerID
		}
		if classID, ok := shipData["class_id"].(string); ok {
			c.state.Ship.ClassID = classID
		}
		if name, ok := shipData["name"].(string); ok {
			c.state.Ship.Name = name
		}

		// Parse hull
		if hull, ok := shipData["hull"].(float64); ok {
			c.state.Ship.Hull = hull
			c.state.Hull = hull
		}
		if maxHull, ok := shipData["max_hull"].(float64); ok {
			c.state.Ship.MaxHull = maxHull
			c.state.MaxHull = maxHull
		}

		// Parse shield
		if shield, ok := shipData["shield"].(float64); ok {
			c.state.Ship.Shield = shield
		}
		if maxShield, ok := shipData["max_shield"].(float64); ok {
			c.state.Ship.MaxShield = maxShield
		}
		if shieldRecharge, ok := shipData["shield_recharge"].(float64); ok {
			c.state.Ship.ShieldRecharge = shieldRecharge
		}

		// Parse other stats
		if armor, ok := shipData["armor"].(float64); ok {
			c.state.Ship.Armor = armor
		}
		if speed, ok := shipData["speed"].(float64); ok {
			c.state.Ship.Speed = speed
		}

		// Parse fuel
		if fuel, ok := shipData["fuel"].(float64); ok {
			c.state.Ship.Fuel = fuel
			c.state.Fuel = fuel
		}
		if maxFuel, ok := shipData["max_fuel"].(float64); ok {
			c.state.Ship.MaxFuel = maxFuel
			c.state.MaxFuel = maxFuel
		}

		// Parse cargo
		if cargoUsed, ok := shipData["cargo_used"].(float64); ok {
			c.state.Ship.CargoUsed = cargoUsed
		}
		if cargoCapacity, ok := shipData["cargo_capacity"].(float64); ok {
			c.state.Ship.CargoCapacity = cargoCapacity
			c.state.MaxCargo = int(cargoCapacity)
		}

		// Parse CPU and power
		if cpuUsed, ok := shipData["cpu_used"].(float64); ok {
			c.state.Ship.CPUUsed = cpuUsed
		}
		if cpuCapacity, ok := shipData["cpu_capacity"].(float64); ok {
			c.state.Ship.CPUCapacity = cpuCapacity
		}
		if powerUsed, ok := shipData["power_used"].(float64); ok {
			c.state.Ship.PowerUsed = powerUsed
		}
		if powerCapacity, ok := shipData["power_capacity"].(float64); ok {
			c.state.Ship.PowerCapacity = powerCapacity
		}

		// Parse modules
		if modules, ok := shipData["modules"].([]any); ok {
			c.state.Ship.Modules = make([]string, 0, len(modules))
			for _, m := range modules {
				if moduleID, ok := m.(string); ok {
					c.state.Ship.Modules = append(c.state.Ship.Modules, moduleID)
				}
			}
		}

		// Parse cargo items
		if cargo, ok := shipData["cargo"].([]any); ok {
			c.state.Ship.Cargo = make([]CargoItem, 0, len(cargo))
			c.state.Cargo = c.state.Cargo[:0]
			for _, item := range cargo {
				if itemMap, ok := item.(map[string]any); ok {
					cargoItem := CargoItem{}
					if itemID, ok := itemMap["item_id"].(string); ok {
						cargoItem.ItemID = itemID
					}
					if quantity, ok := itemMap["quantity"].(float64); ok {
						cargoItem.Quantity = quantity
					}
					c.state.Ship.Cargo = append(c.state.Ship.Cargo, cargoItem)
					c.state.Cargo = append(c.state.Cargo, itemMap)
				}
			}
		}
	}
}

// parseSystemData extracts system information from payload
func (c *Client) parseSystemData(payload map[string]any) {
	// Check for direct system object
	if systemData, ok := payload["system"].(map[string]any); ok {
		c.parseSystemObject(systemData)
	}

	// Check for POIs array
	if poisData, ok := payload["pois"].([]any); ok {
		c.state.System.POIs = c.state.System.POIs[:0]
		for _, p := range poisData {
			if poi, ok := p.(map[string]any); ok {
				poiObj := POI{}
				if id, ok := poi["id"].(string); ok {
					poiObj.ID = id
				}
				if name, ok := poi["name"].(string); ok {
					poiObj.Name = name
				}
				if poiType, ok := poi["type"].(string); ok {
					poiObj.Type = poiType
				}
				if desc, ok := poi["description"].(string); ok {
					poiObj.Description = desc
				}
				if sysID, ok := poi["system_id"].(string); ok {
					poiObj.SystemID = sysID
				}
				if baseID, ok := poi["base_id"].(string); ok {
					poiObj.BaseID = baseID
				}
				if pos, ok := poi["position"].(map[string]any); ok {
					if x, ok := pos["x"].(float64); ok {
						poiObj.Position.X = x
					}
					if y, ok := pos["y"].(float64); ok {
						poiObj.Position.Y = y
					}
				}
				if resources, ok := poi["resources"].([]any); ok {
					for _, r := range resources {
						if res, ok := r.(map[string]any); ok {
							resObj := POIResource{}
							if resourceID, ok := res["resource_id"].(string); ok {
								resObj.ResourceID = resourceID
							}
							if richness, ok := res["richness"].(float64); ok {
								resObj.Richness = richness
							}
							if remaining, ok := res["remaining"].(float64); ok {
								resObj.Remaining = remaining
							}
							poiObj.Resources = append(poiObj.Resources, resObj)
						}
					}
				}
				c.state.System.POIs = append(c.state.System.POIs, poiObj)
			}
		}
		c.state.LastMapUpdate = time.Now()
	}
}

// parseSystemObject parses a system object
func (c *Client) parseSystemObject(systemData map[string]any) {
	if id, ok := systemData["id"].(string); ok {
		c.state.System.ID = id
	}
	if name, ok := systemData["name"].(string); ok {
		c.state.System.Name = name
		c.state.CurrentSystem = name
	}
	if desc, ok := systemData["description"].(string); ok {
		c.state.System.Description = desc
	}
	if empire, ok := systemData["empire"].(string); ok {
		c.state.System.Empire = empire
	}
	if policeLevel, ok := systemData["police_level"].(float64); ok {
		c.state.System.PoliceLevel = int(policeLevel)
	}
	if discovered, ok := systemData["discovered"].(bool); ok {
		c.state.System.Discovered = discovered
	}
	if discoveredBy, ok := systemData["discovered_by"].(string); ok {
		c.state.System.DiscoveredBy = discoveredBy
	}
	if position, ok := systemData["position"].(map[string]any); ok {
		if x, ok := position["x"].(float64); ok {
			c.state.System.Position.X = x
		}
		if y, ok := position["y"].(float64); ok {
			c.state.System.Position.Y = y
		}
	}
	if connections, ok := systemData["connections"].([]any); ok {
		c.state.System.Connections = c.state.System.Connections[:0]
		for _, conn := range connections {
			if connStr, ok := conn.(string); ok {
				c.state.System.Connections = append(c.state.System.Connections, connStr)
			}
		}
	}
}

// parseErrorState extracts state changes from error messages
func (c *Client) parseErrorState(payload map[string]any) {
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

// parseNearbyPlayers extracts nearby player list from state_update
func (c *Client) parseNearbyPlayers(payload map[string]any) {
	if inCombat, ok := payload["in_combat"].(bool); ok {
		c.state.InCombat = inCombat
	}

	if nearbyData, ok := payload["nearby"].([]any); ok {
		c.state.Nearby = c.state.Nearby[:0]
		for _, n := range nearbyData {
			if nearbyMap, ok := n.(map[string]any); ok {
				nearby := NearbyPlayer{}
				if playerID, ok := nearbyMap["player_id"].(string); ok {
					nearby.PlayerID = playerID
				}
				if username, ok := nearbyMap["username"].(string); ok {
					nearby.Username = username
				}
				if shipClass, ok := nearbyMap["ship_class"].(string); ok {
					nearby.ShipClass = shipClass
				}
				if factionID, ok := nearbyMap["faction_id"].(string); ok {
					nearby.FactionID = factionID
				}
				if factionTag, ok := nearbyMap["faction_tag"].(string); ok {
					nearby.FactionTag = factionTag
				}
				if statusMessage, ok := nearbyMap["status_message"].(string); ok {
					nearby.StatusMessage = statusMessage
				}
				if clanTag, ok := nearbyMap["clan_tag"].(string); ok {
					nearby.ClanTag = clanTag
				}
				if primaryColor, ok := nearbyMap["primary_color"].(string); ok {
					nearby.PrimaryColor = primaryColor
				}
				if secondaryColor, ok := nearbyMap["secondary_color"].(string); ok {
					nearby.SecondaryColor = secondaryColor
				}
				if anonymous, ok := nearbyMap["anonymous"].(bool); ok {
					nearby.Anonymous = anonymous
				}
				if inCombat, ok := nearbyMap["in_combat"].(bool); ok {
					nearby.InCombat = inCombat
				}
				c.state.Nearby = append(c.state.Nearby, nearby)
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

// parseListingsData extracts market listings from a listings response
func (c *Client) parseListingsData(payload map[string]any) {
	// Clear previous listings
	c.listingsMu.Lock()
	c.latestListings = c.latestListings[:0]
	c.listingsMu.Unlock()

	// Parse listings array
	if listingsData, ok := payload["listings"].([]any); ok {
		c.listingsMu.Lock()
		for _, l := range listingsData {
			if listingMap, ok := l.(map[string]any); ok {
				listing := MarketListing{}

				if itemID, ok := listingMap["item_id"].(string); ok {
					listing.ItemID = itemID
				}
				// Server response has item_type in some cases, otherwise infer from item_id
				if itemType, ok := listingMap["item_type"].(string); ok {
					listing.ItemType = itemType
				} else if itemID, ok := listingMap["item_id"].(string); ok {
					// Infer type from item_id prefix (ore_, weapon_, mining_, etc)
					if len(itemID) > 0 {
						if itemID[:4] == "ore_" {
							listing.ItemType = "ore"
						} else if len(itemID) > 7 && itemID[:7] == "weapon_" {
							listing.ItemType = "weapon"
						} else if len(itemID) > 7 && itemID[:7] == "mining_" {
							listing.ItemType = "module"
						} else if len(itemID) > 7 && itemID[:7] == "shield_" {
							listing.ItemType = "shield"
						} else if len(itemID) > 6 && itemID[:6] == "cargo_" {
							listing.ItemType = "cargo"
						} else {
							listing.ItemType = "unknown"
						}
					}
				}
				if quantity, ok := listingMap["quantity"].(float64); ok {
					listing.Quantity = quantity
				}
				// Server uses price_each instead of price_per_unit
				if pricePerUnit, ok := listingMap["price_per_unit"].(float64); ok {
					listing.PricePerUnit = pricePerUnit
				} else if priceEach, ok := listingMap["price_each"].(float64); ok {
					listing.PricePerUnit = priceEach
				}
				// Server uses total instead of total_price
				if totalPrice, ok := listingMap["total_price"].(float64); ok {
					listing.TotalPrice = totalPrice
				} else if total, ok := listingMap["total"].(float64); ok {
					listing.TotalPrice = total
				}
				// Listings from NPCs are always "sell" type
				if listingType, ok := listingMap["type"].(string); ok {
					listing.Type = listingType
				} else {
					listing.Type = "sell" // Default to sell for NPC listings
				}
				// Server uses seller instead of listed_by
				if listedBy, ok := listingMap["listed_by"].(string); ok {
					listing.ListedBy = listedBy
				} else if seller, ok := listingMap["seller"].(string); ok {
					listing.ListedBy = seller
				}

				c.latestListings = append(c.latestListings, listing)
			}
		}
		c.listingsMu.Unlock()

		c.debugLogger.Printf("Parsed %d market listings", len(c.latestListings))
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
