package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/user/spacemolt/internal/protocol"
	"github.com/user/spacemolt/pkg/game"
	"github.com/user/spacemolt/pkg/tui"
)

var (
	// Command-line flags
	debugMode = flag.Bool("debug", false, "Enable debug logging to stderr")
	logFile   = flag.String("log-file", "", "Write debug logs to file instead of stderr")
)

var (
	// debugLogger is the logger for debug messages (can be disabled)
	debugLogger *log.Logger = log.New(io.Discard, "", log.LstdFlags)
)

var credentialsFile = ".spacemolt-credentials.json"

func main() {
	flag.Parse()

	// Initialize debug logger
	if *debugMode || *logFile != "" {
		var writer io.Writer = os.Stderr
		if *logFile != "" {
			f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				log.Fatalf("Failed to open log file: %v", err)
			}
			defer func() { _ = f.Close() }()
			writer = f
		}
		debugLogger = log.New(writer, "[DEBUG] ", log.LstdFlags)
	}

	ctx := context.Background()

	// Check for existing credentials
	var username, token string
	if data, err := os.ReadFile(credentialsFile); err == nil {
		var creds map[string]string
		if err := json.Unmarshal(data, &creds); err == nil {
			username = creds["username"]
			token = creds["token"]
		}
	}

	// Connect to WebSocket
	url := "wss://game.spacemolt.com/ws"

	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "")
	}()

	// Game state
	state := &game.State{
		Doc:         true,
		MaxCargo:    10, // Default cargo capacity
		CurrentTick: 0,  // Will be updated from server
		System: game.SystemData{
			POIs:        []game.POI{},
			Connections: []string{},
		},
		Username: username,
		Token:    token,
	}

	// Create TUI model
	model := tui.NewWatcherModel(state)

	// Start Bubbletea program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen buffer
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	// Start WebSocket listener in goroutine
	go wsListener(ctx, ws, state, p)

	// Login or register
	time.Sleep(500 * time.Millisecond)

	if state.Username != "" && state.Token != "" {
		// Try to login first
		loginMsg := protocol.Message{
			Type: protocol.TypeLoggedIn,
			Payload: map[string]any{
				"username": state.Username,
				"token":    state.Token,
			},
			Timestamp: time.Now().UnixMilli(),
		}
		sendMessage(ctx, ws, loginMsg)
	} else {
		// Register new account
		state.Username = fmt.Sprintf("CosmicLobster_%d", time.Now().Unix())
		registerMsg := protocol.Message{
			Type: protocol.TypeRegistered,
			Payload: map[string]any{
				"username": state.Username,
				"empire":   "voidborn",
			},
			Timestamp: time.Now().UnixMilli(),
		}
		sendMessage(ctx, ws, registerMsg)
	}

	// Wait for login/register to complete
	time.Sleep(3 * time.Second)

	// Request system info after login
	sendMessage(ctx, ws, protocol.Message{Type: "get_system", Timestamp: time.Now().UnixMilli()})

	// Run the TUI
	if _, err := p.Run(); err != nil {
		log.Printf("Alas, there's been an error: %v", err)
	}
}

// wsListener handles incoming WebSocket messages and forwards them to the TUI
func wsListener(ctx context.Context, ws *websocket.Conn, state *game.State, p *tea.Program) {
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			p.Send(tui.WsMsg{Type: "error", Payload: map[string]any{"error": err.Error()}})
			return
		}

		var resp protocol.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}

		// Update state directly for tracking
		handleResponse(resp, state)

		// Send to TUI for display
		p.Send(tui.WsMsg{
			Type:    resp.Type,
			Payload: resp.Payload,
		})
	}
}

func sendMessage(ctx context.Context, ws *websocket.Conn, msg protocol.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		log.Printf("Failed to send message: %v", err)
	}
}

// updateShipState extracts ship data from a payload map and updates state.
// Must be called while state.Mu is held.
func updateShipState(state *game.State, ship map[string]any, logPrefix string) {
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
	// cargo_capacity may be int, int64, or float64 depending on JSON decoding
	if maxCargo, ok := ship["cargo_capacity"].(float64); ok {
		state.MaxCargo = int(maxCargo)
		debugLogger.Printf("%s cargo_capacity from float64: %d", logPrefix, state.MaxCargo)
	} else if maxCargo, ok := ship["cargo_capacity"].(int64); ok {
		state.MaxCargo = int(maxCargo)
		debugLogger.Printf("%s cargo_capacity from int64: %d", logPrefix, state.MaxCargo)
	} else if maxCargo, ok := ship["cargo_capacity"].(int); ok {
		state.MaxCargo = maxCargo
		debugLogger.Printf("%s cargo_capacity from int: %d", logPrefix, state.MaxCargo)
	} else if logPrefix != "" {
		var shipKeys []string
		for k := range ship {
			shipKeys = append(shipKeys, k)
		}
		debugLogger.Printf("%s cargo_capacity not found, ship keys: %v", logPrefix, shipKeys)
	}
	if cargo, ok := ship["cargo"].([]any); ok {
		state.Cargo = state.Cargo[:0] // Clear existing cargo
		for _, c := range cargo {
			if item, ok := c.(map[string]any); ok {
				state.Cargo = append(state.Cargo, item)
			}
		}
		debugLogger.Printf("%s cargo: %d items", logPrefix, len(state.Cargo))
	}
}

func handleResponse(resp protocol.Response, state *game.State) {
	state.Mu.Lock()
	defer state.Mu.Unlock()

	// Log all message types and their payload keys for debugging
	var keys []string
	for k := range resp.Payload {
		keys = append(keys, k)
	}
	debugLogger.Printf("[%s] payload keys: %v", resp.Type, keys)

	switch resp.Type {
	case protocol.TypeWelcome:
		// Welcome message - no special handling needed

	case protocol.TypeRegistered:
		if token, ok := resp.Payload["token"].(string); ok {
			state.Token = token
			saveCredentials(state.Username, token)
		}

	case protocol.TypeLoggedIn:
		// Log all payload keys to see what's available
		var keys []string
		for k := range resp.Payload {
			keys = append(keys, k)
		}
		debugLogger.Printf("logged_in payload keys: %v", keys)

		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if pUsername, ok := player["username"].(string); ok {
				state.Username = pUsername
			}
			if currentPoi, ok := player["current_poi"].(string); ok {
				state.CurrentPOI = currentPoi
				state.System.ShipPOI = currentPoi
			}
		}
		// Check for ship data in logged_in response
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			debugLogger.Printf("logged_in contains ship data")
			updateShipState(state, ship, "logged_in")
		}
		if system, ok := resp.Payload["system"].(map[string]any); ok {
			if sysName, ok := system["name"].(string); ok {
				state.CurrentSystem = sysName
			}
		}
		// Save credentials on successful login
		saveCredentials(state.Username, state.Token)

	case protocol.TypeError:
		// Parse error message to detect implied state changes
		if errMsg, ok := resp.Payload["message"].(string); ok {
			// Check for errors implying undocked state
			if containsIgnoreCase(errMsg, []string{"already undocked", "not docked", "ship is not docked"}) {
				if state.Doc {
					state.Doc = false
				}
			}
			// Check for errors implying docked state
			if containsIgnoreCase(errMsg, []string{"already docked", "already at station"}) {
				if !state.Doc {
					state.Doc = true
				}
			}
		}

	case protocol.TypeOK:
		// Handle player data (credits) in ok responses
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if credits, ok := player["credits"].(float64); ok {
				state.Credits = credits
				debugLogger.Printf("ok credits updated: %.0f", state.Credits)
			}
			if currentPoi, ok := player["current_poi"].(string); ok {
				state.CurrentPOI = currentPoi
				state.System.ShipPOI = currentPoi
			}
		}

		// Parse system data if present
		if poisData, ok := resp.Payload["pois"].([]any); ok {
			state.System.POIs = state.System.POIs[:0]
			for _, p := range poisData {
				if poi, ok := p.(map[string]any); ok {
					poiObj := game.POI{}
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
					state.System.POIs = append(state.System.POIs, poiObj)
				}
			}
			state.LastMapUpdate = time.Now()
		}

		if sysData, ok := resp.Payload["system"].(map[string]any); ok {
			if name, ok := sysData["name"].(string); ok {
				state.System.Name = name
			}
			if desc, ok := sysData["description"].(string); ok {
				state.System.Description = desc
			}
			if connections, ok := sysData["connections"].([]any); ok {
				state.System.Connections = state.System.Connections[:0]
				for _, c := range connections {
					if connStr, ok := c.(string); ok {
						state.System.Connections = append(state.System.Connections, connStr)
					}
				}
			}
		}

		// Handle ship data in ok responses (e.g., after mining, trading, etc.)
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			debugLogger.Printf("ok response contains ship data")
			updateShipState(state, ship, "ok")
		}

		if action, ok := resp.Payload["action"].(string); ok {
			switch action {
			case "undock":
				state.Doc = false
			case "dock":
				state.Doc = true
			case "travel":
				state.Traveling = false
			}
		}

	case protocol.TypeDocked:
		state.Doc = true
		state.Traveling = false

	case protocol.TypeUndocked:
		state.Doc = false

	case protocol.TypeStateUpdate:
		// Update tick count
		if tick, ok := resp.Payload["tick"].(float64); ok {
			state.CurrentTick = int64(tick)
		}

		// Update state from payload
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if currentPoi, ok := player["current_poi"].(string); ok {
				state.CurrentPOI = currentPoi
				state.System.ShipPOI = currentPoi
			}
			if credits, ok := player["credits"].(float64); ok {
				state.Credits = credits
			}
		}
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(state, ship, "state_update")
		}

	case protocol.TypeChatMessage:
		// Chat messages are handled by TUI

	case protocol.TypePOI:
		// POI data handled by TUI

	case protocol.TypeSystem:
		// System data handled by TUI

	case protocol.TypeMining:
		// Mining updates - handle ship data (cargo, fuel, etc.)
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			debugLogger.Printf("mining response contains ship data")
			updateShipState(state, ship, "mining")
		}
		// Also check for player data (credits may change when selling)
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if credits, ok := player["credits"].(float64); ok {
				state.Credits = credits
				debugLogger.Printf("mining credits updated: %.0f", state.Credits)
			}
		}
	}
}

// containsIgnoreCase checks if any of the substrings exist in the text (case-insensitive)
func containsIgnoreCase(text string, substrings []string) bool {
	lower := strings.ToLower(text)
	for _, s := range substrings {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func saveCredentials(username, token string) {
	creds := map[string]string{
		"username": username,
		"token":    token,
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal credentials: %v", err)
		return
	}
	if err := os.WriteFile(credentialsFile, data, 0600); err != nil {
		log.Printf("Failed to save credentials: %v", err)
	}
}
