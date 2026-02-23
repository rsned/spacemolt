package game

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// NavigateToSystem uses FindRoute + sequential Jump calls to travel
// from the current system to the target system. Handles multi-hop routes.
// The agent must be undocked before calling this function.
func NavigateToSystem(client *Client, ctx context.Context, targetSystem string, logger *log.Logger) error {
	state := client.GetState()

	if state.CurrentSystem == targetSystem {
		logger.Printf("Already at target system %s", targetSystem)
		return nil
	}

	// Ensure we have system data.
	if len(state.System.Connections) == 0 {
		if err := client.GetSystem(ctx); err != nil {
			return fmt.Errorf("get system: %w", err)
		}
		time.Sleep(2 * time.Second)
		state = client.GetState()
	}

	// Check if the target is a direct neighbor first.
	for _, conn := range state.System.Connections {
		if conn.SystemID == targetSystem {
			return jumpAndWait(client, ctx, targetSystem, logger)
		}
	}

	// Multi-hop: use server pathfinding.
	logger.Printf("Finding route from %s to %s...", state.CurrentSystem, targetSystem)
	steps, err := client.FindRoute(ctx, targetSystem)
	if err != nil {
		return fmt.Errorf("find route to %s: %w", targetSystem, err)
	}

	// The route includes the current system as the first entry — skip past it.
	// Also skip any systems we've already passed through (in case of resume).
	currentSystem := strings.ToLower(state.CurrentSystem)
	startIdx := 0
	for i, step := range steps {
		if strings.ToLower(step.SystemID) == currentSystem || strings.ToLower(step.Name) == currentSystem {
			startIdx = i + 1
			break
		}
	}
	hops := steps[startIdx:]

	logger.Printf("Route found: %d total step(s), %d hop(s) remaining", len(steps), len(hops))
	for i, step := range hops {
		logger.Printf("  Hop %d: %s (%s)", i+1, step.Name, step.SystemID)
	}

	// Execute each hop.
	for i, step := range hops {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		logger.Printf("Jumping %d/%d → %s...", i+1, len(hops), step.Name)
		if err := jumpAndWait(client, ctx, step.SystemID, logger); err != nil {
			return fmt.Errorf("jump hop %d to %s: %w", i+1, step.SystemID, err)
		}
	}

	// Verify we arrived.
	state = client.GetState()
	if state.CurrentSystem != targetSystem {
		return fmt.Errorf("navigation ended at %s, expected %s", state.CurrentSystem, targetSystem)
	}

	logger.Printf("Arrived at %s", targetSystem)
	return nil
}

// NavigateAndDock finds the station in the current system, travels to it, and docks.
func NavigateAndDock(client *Client, ctx context.Context, logger *log.Logger) error {
	state := client.GetState()

	if state.Doc {
		logger.Printf("Already docked")
		return nil
	}

	// Ensure we have system data.
	if len(state.System.POIs) == 0 {
		if err := client.GetSystem(ctx); err != nil {
			return fmt.Errorf("get system: %w", err)
		}
		time.Sleep(2 * time.Second)
		state = client.GetState()
	}

	// Find the station POI.
	var stationPOI string
	for _, poi := range state.System.POIs {
		if poi.Type == "station" {
			stationPOI = poi.ID
			break
		}
	}
	if stationPOI == "" {
		return fmt.Errorf("no station found in system %s", state.System.Name)
	}

	// Travel to station if not already there.
	if state.CurrentPOI != stationPOI && !state.Traveling {
		logger.Printf("Traveling to station %s...", stationPOI)
		if err := client.Travel(ctx, stationPOI); err != nil {
			return fmt.Errorf("travel to station: %w", err)
		}
		// Wait for travel to complete.
		if err := waitForArrival(client, ctx, 60*time.Second); err != nil {
			return fmt.Errorf("waiting for travel: %w", err)
		}
	}

	// Dock.
	logger.Printf("Docking at station...")
	if err := client.Dock(ctx); err != nil {
		// "Already docked" is not a real error.
		if err.Error() == "Already docked (success)" {
			return nil
		}
		return fmt.Errorf("dock: %w", err)
	}
	time.Sleep(SleepDock)
	return nil
}

// jumpAndWait performs a single jump and waits for the travel to complete.
func jumpAndWait(client *Client, ctx context.Context, targetSystem string, logger *log.Logger) error {
	// Undock if docked (can't jump while docked).
	state := client.GetState()
	if state.Doc {
		logger.Printf("Undocking before jump...")
		if err := client.Undock(ctx); err != nil {
			return fmt.Errorf("undock: %w", err)
		}
		time.Sleep(SleepUndock)
	}

	if err := client.Jump(ctx, targetSystem); err != nil {
		return fmt.Errorf("jump to %s: %w", targetSystem, err)
	}

	// Wait for the jump action to execute on the server (it's pending until next tick).
	time.Sleep(SleepJump)

	// Then poll until we're no longer traveling (for variable-length jumps).
	if err := waitForArrival(client, ctx, 120*time.Second); err != nil {
		return fmt.Errorf("waiting after jump to %s: %w", targetSystem, err)
	}

	// Refresh system data at new location.
	if err := client.GetSystem(ctx); err != nil {
		logger.Printf("Warning: failed to get system data after jump: %v", err)
	}
	time.Sleep(SleepQuick)

	return nil
}

// waitForArrival polls the state until the agent is no longer traveling.
func waitForArrival(client *Client, ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		state := client.GetState()
		if !state.Traveling {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("travel timeout after %v", timeout)
		}

		time.Sleep(5 * time.Second)
	}
}
