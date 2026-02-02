package game

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/user/spacemolt/internal/protocol"
)

// Client represents a WebSocket client for the Spacemolt game
type Client struct {
	conn        *websocket.Conn
	url         string
	username    string
	token       string
	state       *State
	mu          sync.RWMutex
	handler     MessageHandler
	stopCh      chan struct{}
	connected   bool
	debugLogger *log.Logger
}

// MessageHandler handles incoming game messages
type MessageHandler interface {
	OnConnected(state *State)
	OnMessage(resp protocol.Response)
	OnDisconnected(err error)
}

// NewClient creates a new game client
func NewClient(url, username, token string, debugLogger *log.Logger) *Client {
	if debugLogger == nil {
		debugLogger = log.New(log.Writer(), "[GAME] ", log.LstdFlags)
	}

	return &Client{
		url:         url,
		username:    username,
		token:       token,
		state: &State{
			Doc:         true,
			MaxCargo:    10,
			CurrentTick: 0,
			System: SystemData{
				POIs:        []POI{},
				Connections: []string{},
			},
		},
		stopCh:      make(chan struct{}),
		debugLogger: debugLogger,
	}
}

// Connect establishes a WebSocket connection to the game server
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	ws, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = ws
	c.connected = true
	c.state.Username = c.username
	c.state.Token = c.token

	c.debugLogger.Printf("Connected to %s", c.url)

	// Start message listener
	go c.listen(ctx)

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

	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	c.debugLogger.Printf("[SENT] %s", msg.Type)
	return nil
}

// Login authenticates with the server using stored credentials
func (c *Client) Login(ctx context.Context) error {
	if c.token == "" {
		return fmt.Errorf("no token available")
	}

	msg := protocol.Message{
		Type: protocol.TypeLoggedIn,
		Payload: map[string]any{
			"username": c.username,
			"token":    c.token,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	return c.Send(ctx, msg)
}

// Register creates a new account
func (c *Client) Register(ctx context.Context, empire string) error {
	msg := protocol.Message{
		Type: protocol.TypeRegistered,
		Payload: map[string]any{
			"username": c.username,
			"empire":   empire,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	return c.Send(ctx, msg)
}

// Undock undocks from the current station
func (c *Client) Undock(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "undock",
		Timestamp: time.Now().UnixMilli(),
	})
}

// Dock docks at a station in the current system
func (c *Client) Dock(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "dock",
		Timestamp: time.Now().UnixMilli(),
	})
}

// Travel travels to a POI within the current system
func (c *Client) Travel(ctx context.Context, targetPOI string) error {
	return c.Send(ctx, protocol.Message{
		Type:     "travel",
		Payload:  map[string]any{"target_poi": targetPOI},
		Timestamp: time.Now().UnixMilli(),
	})
}

// Jump jumps to another system
func (c *Client) Jump(ctx context.Context, targetSystem string) error {
	return c.Send(ctx, protocol.Message{
		Type:     "jump",
		Payload:  map[string]any{"target_system": targetSystem},
		Timestamp: time.Now().UnixMilli(),
	})
}

// Mine mines resources at the current location
func (c *Client) Mine(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "mine",
		Timestamp: time.Now().UnixMilli(),
	})
}

// Scan scans the current area
func (c *Client) Scan(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:     "scan",
		Payload:  map[string]any{"target_id": "area"},
		Timestamp: time.Now().UnixMilli(),
	})
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
			if c.handler != nil {
				c.handler.OnDisconnected(err)
			}
			return
		}

		var resp protocol.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			c.debugLogger.Printf("Failed to parse message: %v", err)
			continue
		}

		c.debugLogger.Printf("[RECV] %s", resp.Type)

		// Update state
		c.handleResponse(resp)

		// Notify handler
		if c.handler != nil {
			c.handler.OnMessage(resp)
		}
	}
}

// handleResponse updates the game state based on server responses
func (c *Client) handleResponse(resp protocol.Response) {
	c.state.Mu.Lock()
	defer c.state.Mu.Unlock()

	// Log all message types and their payload keys for debugging
	var keys []string
	for k := range resp.Payload {
		keys = append(keys, k)
	}
	c.debugLogger.Printf("[%s] payload keys: %v", resp.Type, keys)

	switch resp.Type {
	case protocol.TypeWelcome:
		if tick, ok := resp.Payload["current_tick"].(float64); ok {
			c.state.CurrentTick = int64(tick)
		}

	case protocol.TypeRegistered:
		if token, ok := resp.Payload["token"].(string); ok {
			c.state.Token = token
			c.token = token // Save token
		}

	case protocol.TypeLoggedIn:
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if pUsername, ok := player["username"].(string); ok {
				c.state.Username = pUsername
			}
			if currentPoi, ok := player["current_poi"].(string); ok {
				c.state.CurrentPOI = currentPoi
				c.state.System.ShipPOI = currentPoi
			}
		}
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(c.state, ship, "logged_in")
		}
		if system, ok := resp.Payload["system"].(map[string]any); ok {
			if sysName, ok := system["name"].(string); ok {
				c.state.CurrentSystem = sysName
			}
		}

	case protocol.TypeError:
		// Parse error message to detect implied state changes
		if errMsg, ok := resp.Payload["message"].(string); ok {
			if containsIgnoreCase(errMsg, []string{"already undocked", "not docked", "ship is not docked"}) {
				c.state.Doc = false
			}
			if containsIgnoreCase(errMsg, []string{"already docked", "already at station"}) {
				c.state.Doc = true
			}
		}

	case protocol.TypeOK:
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if credits, ok := player["credits"].(float64); ok {
				c.state.Credits = credits
			}
			if currentPoi, ok := player["current_poi"].(string); ok {
				c.state.CurrentPOI = currentPoi
				c.state.System.ShipPOI = currentPoi
			}
		}

		// Parse system data
		if poisData, ok := resp.Payload["pois"].([]any); ok {
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
					if pos, ok := poi["position"].(map[string]any); ok {
						if x, ok := pos["x"].(float64); ok {
							poiObj.X = x
						}
						if y, ok := pos["y"].(float64); ok {
							poiObj.Y = y
						}
					}
					if resources, ok := poi["resources"].([]any); ok {
						for _, r := range resources {
							if res, ok := r.(map[string]any); ok {
								poiObj.Resources = append(poiObj.Resources, res)
							}
						}
					}
					c.state.System.POIs = append(c.state.System.POIs, poiObj)
				}
			}
			c.state.LastMapUpdate = time.Now()
		}

		if sysData, ok := resp.Payload["system"].(map[string]any); ok {
			if name, ok := sysData["name"].(string); ok {
				c.state.System.Name = name
			}
			if desc, ok := sysData["description"].(string); ok {
				c.state.System.Description = desc
			}
			if connections, ok := sysData["connections"].([]any); ok {
				c.state.System.Connections = c.state.System.Connections[:0]
				for _, conn := range connections {
					if connStr, ok := conn.(string); ok {
						c.state.System.Connections = append(c.state.System.Connections, connStr)
					}
				}
			}
		}

		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(c.state, ship, "ok")
		}

		if action, ok := resp.Payload["action"].(string); ok {
			switch action {
			case "undock":
				c.state.Doc = false
			case "dock":
				c.state.Doc = true
			case "travel":
				c.state.Traveling = false
			}
		}

	case protocol.TypeDocked:
		c.state.Doc = true
		c.state.Traveling = false

	case protocol.TypeUndocked:
		c.state.Doc = false

	case protocol.TypeStateUpdate:
		if tick, ok := resp.Payload["tick"].(float64); ok {
			c.state.CurrentTick = int64(tick)
		}

		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if currentPoi, ok := player["current_poi"].(string); ok {
				c.state.CurrentPOI = currentPoi
				c.state.System.ShipPOI = currentPoi
			}
			if credits, ok := player["credits"].(float64); ok {
				c.state.Credits = credits
			}
		}

		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(c.state, ship, "state_update")
		}

	case protocol.TypeMining:
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(c.state, ship, "mining")
		}
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if credits, ok := player["credits"].(float64); ok {
				c.state.Credits = credits
			}
		}
	}
}

// updateShipState extracts ship data from a payload map and updates state
func updateShipState(state *State, ship map[string]any, logPrefix string) {
	if fuel, ok := ship["fuel"].(float64); ok {
		state.Fuel = fuel
	}
	if maxFuel, ok := ship["max_fuel"].(float64); ok {
		state.MaxFuel = maxFuel
	}
	if hull, ok := ship["hull"].(float64); ok {
		state.Hull = hull
	}
	if maxHull, ok := ship["max_hull"].(float64); ok {
		state.MaxHull = maxHull
	}
	if maxCargo, ok := ship["cargo_capacity"].(float64); ok {
		state.MaxCargo = int(maxCargo)
	} else if maxCargo, ok := ship["cargo_capacity"].(int64); ok {
		state.MaxCargo = int(maxCargo)
	} else if maxCargo, ok := ship["cargo_capacity"].(int); ok {
		state.MaxCargo = maxCargo
	}
	if cargo, ok := ship["cargo"].([]any); ok {
		state.Cargo = state.Cargo[:0]
		for _, c := range cargo {
			if item, ok := c.(map[string]any); ok {
				state.Cargo = append(state.Cargo, item)
			}
		}
	}
}

// containsIgnoreCase checks if any of the substrings exist in the text (case-insensitive)
func containsIgnoreCase(text string, substrings []string) bool {
	for _, substr := range substrings {
		if len(text) >= len(substr) {
			for i := 0; i <= len(text)-len(substr); i++ {
				match := true
				for j := 0; j < len(substr); j++ {
					c1 := text[i+j]
					c2 := substr[j]
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
