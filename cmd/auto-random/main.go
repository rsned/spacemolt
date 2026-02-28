package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

const (
	// Random agent behavior thresholds
	MAX_IDLE_TICKS   = 20  // Switch to new action after this many idle ticks
	ACTION_COOLDOWN  = 3   // Seconds to wait between actions
	CHAT_PROBABILITY = 0.1 // 10% chance to send a random chat message
)

type RandomAgent struct {
	client *game.Client
	logger *log.Logger
	ctx    context.Context
}

func (a *RandomAgent) OnConnected(state *game.State) {
	a.logger.Printf("Connected! Credits: %.2f", state.Credits)
}

func (a *RandomAgent) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			a.logger.Printf("OK: %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			a.logger.Printf("ERROR: %s", msg)
		}
	}
}

func (a *RandomAgent) OnDisconnected(err error) {
	a.logger.Printf("Disconnected: %v", err)
}

func updateCaptainsLog(agentID string, client *game.Client, tickCounter int) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Ticks executed: %d", tickCounter))
	notes = append(notes, fmt.Sprintf("Credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	if len(state.Ship.Cargo) > 0 {
		notes = append(notes, fmt.Sprintf("Cargo: %d items (%.0f/%.0f)", len(state.Ship.Cargo), state.Ship.CargoUsed, state.Ship.CargoCapacity))
	}

	currentGoal := "Autonomous random behavior - exploring galaxy and simulating NPC activity"
	if state.Doc {
		currentGoal = "Docked at station, performing random station activities"
	} else if state.Traveling {
		currentGoal = fmt.Sprintf("Traveling to %s", state.TravelProgress.Destination)
	} else if state.InCombat {
		currentGoal = "Engaged in unexpected combat"
	}

	entry := &game.AgentLog{
		AgentName:   state.Player.Username,
		CurrentGoal: currentGoal,
		Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
		Notes:       notes,
	}

	if err := game.WriteCaptainsLog(agentID, entry); err != nil {
		// Log error but don't fail - captain's log is not critical
		_ = err
	}
}

// randomLoop implements the main autonomous loop for random agent
// Logic: Perform random actions (dock/undock/travel/scan/jump/etc.) to simulate NPC behavior
func randomLoop(agentID string, client *game.Client, logger *log.Logger, ctx context.Context) error {
	a := &RandomAgent{
		client: client,
		logger: logger,
		ctx:    ctx,
	}

	tickCounter := 0
	idleTicks := 0

	logger.Printf("Starting autonomous random NPC agent...")
	logger.Printf("Will randomly explore galaxy and perform actions")

	// Captain's log ticker - update every 2 minutes
	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	// Initial captain's log entry
	updateCaptainsLog(agentID, client, tickCounter)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-logTicker.C:
			updateCaptainsLog(agentID, client, tickCounter)
		default:
		}

		state := client.GetState()
		tickCounter++
		logger.Printf("=== Tick %d ===", tickCounter)
		logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Docked: %v | Traveling: %v | Location: %s",
			state.Credits, state.Fuel, state.MaxFuel, state.Hull,
			state.MaxHull, state.Doc, state.Traveling, state.System.Name)

		// Count idle ticks
		if !state.Traveling && !state.InCombat {
			idleTicks++
		} else {
			idleTicks = 0
		}

		// Switch to new behavior after too many idle ticks
		if idleTicks > MAX_IDLE_TICKS {
			action := a.pickRandomAction()
			a.performAction(client, logger, ctx, action)
			idleTicks = 0
			continue
		}

		// Small chance to send a random chat message
		if rand.Float64() < CHAT_PROBABILITY {
			chatMessages := []string{
				"Exploration is key to discovery!",
				"Has anyone seen the Nebula Collective?",
				"The stars are beautiful tonight.",
				"Trading at Haven Exchange is profitable today.",
				"I found a rich asteroid field!",
				"Warning: Pirates spotted in sector 7.",
				"Random jump gate activated!",
			}
			msg := chatMessages[rand.Intn(len(chatMessages))]
			if err := client.Send(ctx, protocol.Message{
				Type: "chat",
				Payload: map[string]any{
					"channel": "system",
					"content": msg,
				},
				Timestamp: time.Now().UnixMilli(),
			}); err != nil {
				logger.Printf("Warning: Failed to send chat: %v", err)
			}
			logger.Printf("Chat: %s", msg)
			time.Sleep(time.Duration(ACTION_COOLDOWN) * time.Second)
		}

		// Wait before next tick
		time.Sleep(10 * time.Second)
	}
}

// pickRandomAction selects a random game action based on current state
func (a *RandomAgent) pickRandomAction() string {
	actions := []string{
		"get_status", "get_system", "get_poi", "scan", "travel", "dock", "undock",
		"jump", "mine", "refuel", "repair",
	}

	// Weight towards actions that make sense for an NPC
	return actions[rand.Intn(len(actions))]
}

// performAction executes a single action and handles the result
func (a *RandomAgent) performAction(client *game.Client, logger *log.Logger, ctx context.Context, action string) {
	state := client.GetState()

	logger.Printf("Action: %s", action)

	switch action {
	case "get_status":
		_ = client.GetStatus(ctx)
		logger.Printf("Status retrieved")

	case "get_system":
		_ = client.GetSystem(ctx)
		logger.Printf("System data retrieved")

	case "get_poi":
		_ = client.GetPOI(ctx)
		logger.Printf("POI data retrieved")

	case "scan":
		_ = client.Scan(ctx)
		logger.Printf("Scanned area")

	case "travel":
		if state.Doc && !state.Traveling {
			// Travel to a random POI in current system
			pois := []string{}
			poiNames := map[string]string{} // Track POI names for logging
			for _, p := range state.System.POIs {
				if p.Type == "station" || p.Type == "planet" {
					pois = append(pois, p.ID)
					poiNames[p.ID] = p.Name
				}
			}
			if len(pois) > 0 {
				target := pois[rand.Intn(len(pois))]
				logger.Printf("Traveling to POI: %s (%s)", target, poiNames[target])
				_ = client.Travel(ctx, target)
				logger.Printf("Travel initiated")
			} else {
				logger.Printf("No valid travel targets in system")
			}
		} else {
			logger.Printf("Cannot travel (docked=%v, traveling=%v)", state.Doc, state.Traveling)
		}

	case "dock":
		if !state.Doc && state.CurrentPOI != "" {
			// Find nearest station
			var stationPOI string
			for _, p := range state.System.POIs {
				if p.Type == "station" {
					stationPOI = p.ID
					break
				}
			}
			if stationPOI != "" {
				logger.Printf("Docking at station: %s", stationPOI)
				_ = client.Dock(ctx)
				logger.Printf("Docking initiated")
			} else {
				logger.Printf("No station found at current POI")
			}
		} else {
			logger.Printf("Cannot dock (already docked or no POI)")
		}

	case "undock":
		if state.Doc {
			logger.Printf("Undocking from station")
			_ = client.Undock(ctx)
			logger.Printf("Undock initiated")
		} else {
			logger.Printf("Cannot undock (not docked)")
		}

	case "jump":
		if state.Doc && !state.Traveling {
			// Jump to a random connected system
			systems := state.System.Connections
			if len(systems) > 0 {
				target := systems[rand.Intn(len(systems))]
				logger.Printf("Jumping to system: %s", target.Name)
				_ = client.Jump(ctx, target.SystemID)
				logger.Printf("Jump initiated")
			} else {
				logger.Printf("No jump gates available")
			}
		} else {
			logger.Printf("Cannot jump (docked=%v, traveling=%v)", state.Doc, state.Traveling)
		}

	case "mine":
		if !state.Doc && state.CurrentPOI != "" && !state.Traveling {
			// Try to mine if at asteroid belt
			isAsteroidField := false
			for _, p := range state.System.POIs {
				if p.ID == state.CurrentPOI && p.Type == "asteroid_belt" {
					isAsteroidField = true
					break
				}
			}
			if isAsteroidField {
				logger.Printf("Mining resources")
				_ = client.Mine(ctx)
				logger.Printf("Mining initiated")
			} else {
				logger.Printf("Not at asteroid field")
			}
		} else {
			logger.Printf("Cannot mine (docked=%v, no POI=%v, traveling=%v)",
				state.Doc, state.CurrentPOI == "", state.Traveling)
		}

	case "refuel":
		if state.Doc && state.Fuel < state.MaxFuel*0.8 {
			logger.Printf("Refueling")
			_ = client.Refuel(ctx)
			logger.Printf("Refuel initiated")
		} else {
			logger.Printf("Cannot refuel (not docked or fuel sufficient)")
		}

	case "repair":
		if state.Doc && state.Hull < state.MaxHull*0.9 {
			logger.Printf("Repairing hull")
			_ = client.Repair(ctx)
			logger.Printf("Repair initiated")
		} else {
			logger.Printf("Cannot repair (not docked or hull sufficient)")
		}

	default:
		logger.Printf("Unknown action: %s", action)
	}

	// Wait before next action
	time.Sleep(time.Duration(ACTION_COOLDOWN) * time.Second)
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-random [flags] <agent-id>")
		fmt.Println("Example: auto-random random-1")
		fmt.Println()
		fmt.Println("Flags:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("This tool controls a random NPC agent that performs")
		fmt.Println("various actions to simulate autonomous behavior:")
		fmt.Println("  - Explores systems and travels")
		fmt.Println("  - Mines at asteroid fields")
		fmt.Println("  - Docks/undocks at stations")
		fmt.Println("  - Jumps between systems")
		fmt.Println("  - Randomly chats and acts like an NPC")
		fmt.Println()
		os.Exit(1)
	}

	agentID := flag.Args()[0]

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	// Check captain's log for previous mission
	previousLog, err := game.ReadLatestCaptainsLog(agentID)
	if err != nil {
		logger.Printf("Failed to read captain's log: %v", err)
	} else if previousLog != nil {
		logger.Printf("📖 Captain's Log - Last Entry:")
		logger.Printf("   Mission: %s", previousLog.CurrentGoal)
		logger.Printf("   Location: %s", previousLog.Location)
		logger.Printf("   Time: %s", previousLog.Timestamp.Format("2006-01-02 15:04"))
	}

	// Create context for lifecycle management
	ctx := context.Background()

	// Initialize game client using shared library function
	// This handles: credential loading, client creation, connection, and login
	client, creds, err := game.InitializeAgent(agentID, logger, ctx, *debug)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("Ready! Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Empire, state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Start autonomous random loop
	logger.Printf("Starting autonomous random NPC agent...")
	logger.Printf("Will randomly explore galaxy, perform actions, and chat")

	if err := randomLoop(agentID, client, logger, ctx); err != nil {
		log.Fatalf("Random loop error: %v", err)
	}
}
