package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// getCapitalDisplayName returns the display name for a capital system ID
func getCapitalDisplayName(systemID string) string {
	switch systemID {
	case "sol":
		return "Sol"
	case "frontier":
		return "Frontier"
	case "nexus_prime":
		return "Nexus Prime"
	case "haven":
		return "Haven"
	case "krynn":
		return "Krynn"
	default:
		return systemID
	}
}

const (
	// TickInterval is how often to check state updates
	TickInterval = 5 * time.Second
	// JumpTimeout is maximum time to wait for a jump to complete
	JumpTimeout = 35 * time.Second
	// TravelTimeout is maximum time to wait for in-system travel to complete
	TravelTimeout = 15 * time.Second
	// UndockTimeout is maximum time to wait for undock to complete
	// Game server processes 1 action every 10 seconds, so we need:
	// - 10 seconds for the action to be processed
	// - Additional time for state update to arrive
	// - Buffer for any delays
	UndockTimeout = 30 * time.Second
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-recall [flags] <agent-id>")
		fmt.Println("")
		fmt.Println("Arguments:")
		fmt.Println("  agent-id   Agent identifier (e.g., miner-1, trader-1)")
		fmt.Println("")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Description:")
		fmt.Println("  Automatically returns the agent to their Empire's capital base.")
		fmt.Println("  - Determines agent's current location")
		fmt.Println("  - Finds route to Empire capital (Solarian->Sol, Crimson->Krynn, etc.)")
		fmt.Println("  - Performs jump sequence to reach capital system")
		fmt.Println("  - Travels to and docks at the capital base")
		fmt.Println("")
		fmt.Println("Example:")
		fmt.Println("  auto-recall miner-1    # Returns miner-1 to Sol base")
		os.Exit(1)
	}

	agentID := flag.Args()[0]
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	// Create context for lifecycle management
	ctx := context.Background()

	// Initialize game client using shared library function
	client, creds, err := game.InitializeAgent(agentID, logger, ctx, *debug)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("🚀 Starting auto-recall to home base...")
	logger.Printf("Agent: %s | Empire: %s | Current System: %s | Credits: %.2f",
		creds.Username, creds.Empire, state.CurrentSystem, state.Credits)

	// Determine capital system
	capitalSystem := agent.EmpireCapitalSystem(state.Player.Empire)
	if capitalSystem == "" {
		log.Fatalf("Unknown empire: %s (cannot determine capital system)", state.Player.Empire)
	}
	// Get display name for capital system
	capitalName := getCapitalDisplayName(capitalSystem)
	logger.Printf("🏠 Capital System: %s (%s)", capitalName, capitalSystem)

	// Execute recall sequence
	if err := executeRecall(ctx, client, logger, capitalSystem, capitalName); err != nil {
		log.Fatalf("Recall failed: %v", err)
	}

	logger.Printf("✅ Recall complete! Agent is now docked at %s base", capitalName)
}

// executeRecall performs the full recall sequence
func executeRecall(ctx context.Context, client *game.Client, logger *log.Logger, capitalSystem, capitalName string) error {
	state := client.GetState()

	// Early check: If already docked at capital base or station, we're done
	if state.CurrentSystem == capitalSystem && state.Doc {
		// Verify we're at a base or station POI
		var atBaseOrStation bool
		for _, poi := range state.System.POIs {
			if poi.ID == state.CurrentPOI && (poi.Type == "base" || poi.Type == "station" || poi.HasBase) {
				atBaseOrStation = true
				break
			}
		}

		if atBaseOrStation {
			logger.Printf("✓ Already docked at %s base/station - nothing to do!", capitalName)
			return nil
		}
	}

	// Step 1: Undock if currently docked
	if state.Doc {
		logger.Printf("📤 Undocking from current location...")
		if err := client.Undock(ctx); err != nil {
			return fmt.Errorf("failed to undock: %w", err)
		}
		// Wait a moment for any state updates, then verify we're undocked
		time.Sleep(1 * time.Second)
		state = client.GetState()
		if state.Doc {
			// Still showing as docked, wait for actual undock
			if err := waitForUndock(ctx, client, logger); err != nil {
				return fmt.Errorf("error waiting for undock to complete: %w", err)
			}
		} else {
			logger.Printf("   ✓ Already undocked")
		}
		logger.Printf("✓ Undocked successfully")
	}

	// Step 2: If already in capital system, travel to base
	state = client.GetState()
	if state.CurrentSystem == capitalSystem {
		logger.Printf("✓ Already in capital system %s", capitalName)
		return travelToBase(ctx, client, logger)
	}

	// Step 3: Find route to capital
	logger.Printf("🗺️  Finding route to %s...", capitalName)
	route, err := client.FindRoute(ctx, capitalSystem)
	if err != nil {
		return fmt.Errorf("failed to find route to %s: %w", capitalSystem, err)
	}

	if len(route) == 0 {
		return fmt.Errorf("no route found to %s", capitalSystem)
	}

	// Filter out any route steps with 0 jumps (already at target)
	var filteredRoute []game.RouteStep
	for _, step := range route {
		if step.Jumps > 0 {
			filteredRoute = append(filteredRoute, step)
		}
	}

	// If all steps were filtered out, we're already there
	if len(filteredRoute) == 0 {
		logger.Printf("✓ Already in capital system %s", capitalName)
		return travelToBase(ctx, client, logger)
	}

	logger.Printf("✓ Route found: %d jumps", len(filteredRoute))
	for i, step := range filteredRoute {
		logger.Printf("   %d. %s (%s)", i+1, step.Name, step.SystemID)
	}

	// Step 4: Execute jump sequence
	for i, step := range filteredRoute {
		state = client.GetState()

		// Check if we're already at the target
		if state.CurrentSystem == step.SystemID {
			logger.Printf("✓ Already at %s", step.Name)
			continue
		}

		logger.Printf("🌟 Jump %d/%d: %s -> %s",
			i+1, len(filteredRoute), state.CurrentSystem, step.Name)

		if err := performJump(ctx, client, logger, step.SystemID, step.Name); err != nil {
			return fmt.Errorf("jump to %s failed: %w", step.Name, err)
		}

		logger.Printf("✓ Arrived at %s", step.Name)

		// Refresh system data to get updated POI list
		logger.Printf("   Refreshing system data...")
		if err := client.GetSystem(ctx); err != nil {
			logger.Printf("   Warning: Failed to refresh system data: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Step 5: Final check and travel to base
	state = client.GetState()
	if state.CurrentSystem != capitalSystem {
		return fmt.Errorf("navigation error: expected to be in %s but in %s",
			capitalSystem, state.CurrentSystem)
	}

	logger.Printf("✓ Successfully reached capital system %s", capitalName)
	return travelToBase(ctx, client, logger)
}

// performJump executes a single jump and waits for completion
func performJump(ctx context.Context, client *game.Client, logger *log.Logger, targetSystem, _ string) error {
	// Execute jump - let the server validate if it's possible
	logger.Printf("   Initiating jump...")
	if _, err := client.Jump(ctx, targetSystem); err != nil {
		return fmt.Errorf("jump command failed: %w", err)
	}

	return nil
}

// waitForUndock waits for undock to complete by monitoring state
// Note: Game server processes actions every 10 seconds, so undock takes:
// - 10s for action to be processed
// - Additional ticks for state update to arrive
// - Total can be 20-30s depending on server load
func waitForUndock(ctx context.Context, client *game.Client, logger *log.Logger) error {
	deadline := time.Now().Add(UndockTimeout)
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()

	// Give the state a moment to update
	time.Sleep(500 * time.Millisecond)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for undock to complete")
			}

			state := client.GetState()
			if !state.Doc {
				logger.Printf("   ✓ Undock complete")
				return nil
			}
		}
	}
}

// waitForPOIArrival waits for the ship to arrive at a specific POI
func waitForPOIArrival(ctx context.Context, client *game.Client, logger *log.Logger, targetPOI string) error {
	deadline := time.Now().Add(TravelTimeout)
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for arrival at POI")
			}

			state := client.GetState()
			if state.CurrentPOI == targetPOI {
				logger.Printf("   ✓ Arrived at target POI")
				return nil
			}
		}
	}
}

// travelToBase travels within the current system to the base/station and docks
func travelToBase(ctx context.Context, client *game.Client, logger *log.Logger) error {
	state := client.GetState()

	// Find a base or station POI (prefer base, fall back to station)
	var basePOI *game.POI
	var stationPOI *game.POI
	for i := range state.System.POIs {
		poi := &state.System.POIs[i]
		if poi.Type == "base" || poi.HasBase {
			basePOI = poi
			break
		}
		if poi.Type == "station" {
			stationPOI = poi
		}
	}

	// Use base if found, otherwise use station
	targetPOI := basePOI
	if targetPOI == nil {
		targetPOI = stationPOI
	}

	if targetPOI == nil {
		return fmt.Errorf("no base or station found in system %s", state.CurrentSystem)
	}

	poiType := "base"
	if targetPOI.Type == "station" {
		poiType = "station"
	}
	logger.Printf("🏢 %s found: %s", strings.ToTitle(poiType), targetPOI.Name)

	// Check if already at the base
	if state.CurrentPOI == basePOI.ID {
		logger.Printf("✓ Already at base location")
	} else {
		// Travel to base
		logger.Printf("🛤️  Traveling to base...")
		if _, err := client.Travel(ctx, basePOI.ID); err != nil {
			// Check if already traveling - if so, wait for arrival
			if err.Error() == "already in transit - wait for arrival" {
				logger.Printf("   Already in transit, waiting for arrival...")
			} else {
				return fmt.Errorf("failed to travel to base: %w", err)
			}
		}

		if err := waitForPOIArrival(ctx, client, logger, basePOI.ID); err != nil {
			return fmt.Errorf("error waiting for travel to base: %w", err)
		}
		logger.Printf("✓ Arrived at base location")
	}

	// Dock at the base
	state = client.GetState()
	if state.Doc && state.CurrentPOI == basePOI.ID {
		// Already docked at the base
		logger.Printf("✓ Already docked at %s", basePOI.Name)
		return nil
	}

	if !state.Doc {
		logger.Printf("🔄 Docking at base...")
		if err := client.Dock(ctx); err != nil {
			// Already docked is not an error
			if err.Error() != "Already docked (success)" {
				return fmt.Errorf("failed to dock: %w", err)
			}
		}
		logger.Printf("✓ Docked successfully")
	} else {
		// Docked but at a different POI - need to travel to base first
		logger.Printf("⚠️  Docked at different location, traveling to base...")
		if _, err := client.Travel(ctx, basePOI.ID); err != nil {
			return fmt.Errorf("failed to travel to base: %w", err)
		}
		if err := waitForPOIArrival(ctx, client, logger, basePOI.ID); err != nil {
			return fmt.Errorf("error waiting for travel to base: %w", err)
		}
		// Now dock
		logger.Printf("🔄 Docking at base...")
		if err := client.Dock(ctx); err != nil {
			return fmt.Errorf("failed to dock: %w", err)
		}
		logger.Printf("✓ Docked successfully")
	}

	return nil
}
