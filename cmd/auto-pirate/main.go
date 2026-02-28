package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

const (
	// Pirating thresholds and priorities
	TARGET_SHIP_CLASS   = "fighter" // Preferred target ship class
	MAX_COMBAT_DISTANCE = 15        // Maximum distance to engage targets (AU)
	MIN_COMBAT_TICKS    = 20        // Minimum ticks in combat before disengaging
	SAFE_HULL_PERCENT   = 0.3       // Disengage if hull drops below 30%
	RESERVE_CREDITS     = 100.0     // Reserve for repairs and refueling
)

type PirateAgent struct {
	logger *log.Logger
}

func (p *PirateAgent) OnConnected(state *game.State) {
	p.logger.Printf("Connected! Credits: %.2f", state.Credits)
}

func (p *PirateAgent) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			p.logger.Printf("OK: %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			p.logger.Printf("ERROR: %s", msg)
		}
	}
}

func (p *PirateAgent) OnDisconnected(err error) {
	p.logger.Printf("Disconnected: %v", err)
}

func updateCaptainsLog(agentID string, client *game.Client, combatEncounters int) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Combat encounters: %d", combatEncounters))
	notes = append(notes, fmt.Sprintf("Credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	if len(state.Ship.Cargo) > 0 {
		notes = append(notes, fmt.Sprintf("Cargo: %d items (%.0f/%.0f)", len(state.Ship.Cargo), state.Ship.CargoUsed, state.Ship.CargoCapacity))
	}

	// Count weapons
	weaponCount := 0
	for _, module := range state.Ship.Modules {
		if len(module) >= 7 && module[:7] == "weapon_" {
			weaponCount++
		}
	}
	notes = append(notes, fmt.Sprintf("Weapons: %d", weaponCount))

	currentGoal := "Awaiting implementation of pirating logic - currently monitoring for targets"
	if state.Doc {
		currentGoal = "Docked at station, selling plundered loot and upgrading weapons"
	} else if state.InCombat {
		currentGoal = "Engaged in combat with hostile target"
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

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-pirate [flags] <agent-id>")
		fmt.Println("This tool controls a pirate agent that hunts NPCs and players")
		fmt.Println("NOTE: This agent is currently simplified and needs combat logic implemented")
		fmt.Println("")
		fmt.Println("Flags:")
		flag.PrintDefaults()
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

	ctx := context.Background()

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

	state := client.GetState()
	logger.Printf("Ready! Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Empire, state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	logger.Printf("Pirate agent started - awaiting implementation of combat logic")
	logger.Printf("Currently in simple monitoring mode")

	combatEncounters := 0

	// Update captain's log on startup
	updateCaptainsLog(agentID, client, combatEncounters)

	// Periodic captain's log updates
	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	statusTicker := time.NewTicker(10 * time.Second)
	defer statusTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-logTicker.C:
			updateCaptainsLog(agentID, client, combatEncounters)
		case <-statusTicker.C:
			state := client.GetState()
			logger.Printf("Status: Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Docked: %v | Location: %s",
				state.Credits, state.Fuel, state.MaxFuel, state.Hull,
				state.MaxHull, state.Doc, state.System.Name)
		}
	}
}
