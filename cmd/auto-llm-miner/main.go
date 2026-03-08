package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	llmpkg "github.com/rsned/spacemolt/pkg/llm"
)

const (
	ROLE = "miner"
)

func updateCaptainsLog(agentID string, client *game.Client, runCount int) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("LLM-guided runs completed: %d", runCount))
	notes = append(notes, fmt.Sprintf("Credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	notes = append(notes, fmt.Sprintf("Cargo: %d items (%.0f/%.0f)", len(state.Ship.Cargo), state.Ship.CargoUsed, state.Ship.CargoCapacity))

	// Count mining lasers
	miningLasers := 0
	for _, module := range state.Ship.Modules {
		if len(module) >= 13 && module[:13] == "mining_laser_" {
			miningLasers++
		}
	}
	notes = append(notes, fmt.Sprintf("Mining lasers: %d", miningLasers))

	currentGoal := "LLM-powered autonomous mining - AI decision making for optimal resource gathering"
	if state.Doc {
		currentGoal = "Docked at station - AI analyzing best actions (selling, refueling, upgrades)"
	} else if state.Traveling && state.TravelProgress != nil {
		currentGoal = fmt.Sprintf("AI-guided travel to %s", state.TravelProgress.Destination)
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
		fmt.Println("Usage: auto-llm-miner [flags] <agent-id>")
		fmt.Println("Example: auto-llm-miner miner-1")
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

	// Step 1: Initialize game client with shared library
	client, creds, err := game.InitializeAgent(agentID, logger, ctx, *debug)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	logger.Printf("🤖 LLM-Powered autonomous miner starting...")
	logger.Printf("Agent: %s | Empire: %s",
		creds.Username, creds.Empire)

	// Step 2: Initialize LLM client
	llmClient, err := llmpkg.New(llmpkg.Config{
		BaseURL: "http://localhost:11434",
		Model:   "llama3.2",
		Timeout: 30 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to initialize LLM client: %v", err)
	}

	// Step 3: Main decision loop
	runCount := 0

	// Captain's log ticker - update every 2 minutes
	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	// Initial captain's log entry
	updateCaptainsLog(agentID, client, runCount)

	for {
		select {
		case <-ctx.Done():
			return
		case <-logTicker.C:
			updateCaptainsLog(agentID, client, runCount)
		default:
		}

		runCount++
		logger.Printf("═══ Run #%d ═══", runCount)

		state := client.GetState()

		// Step 4: Build prompt for LLM
		prompt := buildMiningPrompt(state, creds.Username, agentID)

		// Step 5: Query LLM for decision
		response, err := llmClient.Decide(ctx, prompt)
		if err != nil {
			logger.Printf("❌ LLM query failed: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		logger.Printf("🤖 LLM Decision: action=%s target=%s (confidence: %.2f)", response.Action, response.Target, response.Confidence)
		logger.Printf("   Reasoning: %s", response.Reasoning)

		// Step 6: Parse and execute action
		// Construct action string from DecisionResponse fields
		actionStr := response.Action
		if response.Target != "" {
			actionStr = fmt.Sprintf("%s target=%s", response.Action, response.Target)
		}
		action, args, err := parseAction(actionStr)
		if err != nil {
			logger.Printf("❌ Failed to parse LLM action: %v", err)
			logger.Printf("   Action string: %s", actionStr)
			time.Sleep(5 * time.Second)
			continue
		}

		logger.Printf("🎯 Action: %s %v", action, args)

		// Step 7: Execute action
		var result protocol.Response
		execErr := executeAction(client, ctx, action, args, &result)
		if execErr != nil {
			logger.Printf("❌ Action execution failed: %v", execErr)
		} else {
			logger.Printf("✅ Action completed successfully")
			if msg, ok := result.Payload["message"].(string); ok {
				logger.Printf("   Result: %s", msg)
			}
		}

		// Update captain's log every 10 runs
		if runCount%10 == 0 {
			updateCaptainsLog(agentID, client, runCount)
		}

		// Wait for rate limiting (10s per game action)
		logger.Printf("⏱ Waiting 10 seconds for next action...")
		time.Sleep(10 * time.Second)
	}
}

// buildMiningPrompt creates a prompt for the LLM based on current game state
func buildMiningPrompt(state *game.State, username, agentID string) string {
	fuelPct := (state.Fuel / state.MaxFuel) * 100
	hullPct := (state.Hull / state.MaxHull) * 100
	cargoPct := (float64(len(state.Ship.Cargo)) / float64(state.Ship.CargoCapacity)) * 100

	prompt := fmt.Sprintf(`You are %s, an autonomous miner in SpaceMolt. Session ID: %s

## CURRENT SITUATION
**Location:** %s (%s)
**Ship:** %s (%s)
**Fuel:** %.0f/%.0f (%.1f%% full)
**Hull:** %.0f/%.0f (%.1f%% full)
**Cargo:** %d/%.0f items (%.1f%% full)
**Credits:** %.0f

## YOUR ROLE
You are a mining specialist. Your goals:
1. Keep your ship fueled and repaired
2. Mine resources when at asteroid belts/fields
3. Return to station when cargo is full or fuel is low
4. Sell all cargo at station
5. Upgrade equipment when profitable

## CRITICAL INSTRUCTION: BE ACTIONABLE

You must respond with EXACTLY ONE action. Not analysis, not plans, not "I will" - ONE concrete command.

## AVAILABLE ACTIONS

Use this format: action_name arg1=value1 arg2=value2

**Movement:**
- travel poi_id=<poi_id>
- dock
- undock

**Mining:**
- mine

**Station Actions:**
- sell_all
- refuel
- repair

**Upgrades:**
- buy item_id=<item_id> quantity=<quantity>
- install module_id=<module_id>

## GOOD EXAMPLES
travel poi_id=asteroid_belt_01
mine
sell_all
refuel
buy item_id=mining_laser_1 quantity=1

## BAD EXAMPLES
"I should travel to the asteroid belt" - Too verbose
"Traveling to mine some resources" - Not actionable
"What should I do next?" - Not a concrete action

Choose ONE concrete action that advances your goals.`,
		username,
		agentID,
		state.System.Name,
		state.CurrentSystem,
		state.Ship.Name,
		state.Ship.ClassID,
		state.Fuel,
		state.MaxFuel,
		fuelPct,
		state.Hull,
		state.MaxHull,
		hullPct,
		len(state.Ship.Cargo),
		state.Ship.CargoCapacity,
		cargoPct,
		state.Credits,
	)

	return prompt
}

// parseAction extracts action name and args from LLM response
func parseAction(content string) (action string, args map[string]string, err error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", nil, fmt.Errorf("empty LLM response")
	}

	// Split into tokens
	tokens := strings.Fields(content)
	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("no tokens in LLM response")
	}

	action = strings.ToLower(tokens[0])

	// Parse args (key=value or key="value" format)
	args = make(map[string]string)
	for _, token := range tokens[1:] {
		token = strings.TrimSpace(token)

		// Check for key=value or key="value" format
		if strings.Contains(token, "=") {
			parts := strings.SplitN(token, "=", 2)
			if len(parts) == 2 {
				key := strings.ToLower(strings.TrimSpace(parts[0]))
				value := strings.Trim(strings.TrimSpace(parts[1]), `"`)
				args[key] = value
			}
		}
	}

	return action, args, nil
}

// executeAction executes an action using the game client
func executeAction(client *game.Client, ctx context.Context, action string, args map[string]string, result *protocol.Response) error {
	switch action {
	case "travel":
		poiID, ok := args["poi_id"]
		if !ok {
			return fmt.Errorf("travel requires poi_id argument")
		}
		_, err := client.Travel(ctx, poiID)
		return err

	case "dock":
		return client.Dock(ctx)

	case "undock":
		return client.Undock(ctx)

	case "mine":
		return client.Mine(ctx)

	case "sell_all":
		return client.SellAll(ctx)

	case "refuel":
		return client.Refuel(ctx)

	case "repair":
		return client.Repair(ctx)

	case "buy":
		itemID, ok := args["item_id"]
		if !ok {
			return fmt.Errorf("buy requires item_id argument")
		}
		quantity := 1.0
		if qty, ok := args["quantity"]; ok {
			if _, err := fmt.Sscanf(qty, "%f", &quantity); err != nil {
				return fmt.Errorf("invalid quantity value: %w", err)
			}
		}
		return client.Buy(ctx, itemID, quantity)

	case "install":
		moduleID, ok := args["module_id"]
		if !ok {
			return fmt.Errorf("install requires module_id argument")
		}
		return client.Install(ctx, moduleID)

	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}
