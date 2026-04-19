package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// autopilot executes a multi-jump route to a target system, updating the KB
// at each waypoint. Usage: autopilot <system> [poi]
func autopilot(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	if len(parts) < 2 {
		return fmt.Errorf("usage: autopilot <system-name> [poi-name]")
	}

	targetSystem := parts[1]
	targetPOI := ""
	if len(parts) >= 3 {
		targetPOI = strings.Join(parts[2:], " ")
	}

	// Step 1: Find route
	if format == formatStyled {
		fmt.Printf("Finding route to %s...\n", targetSystem)
	}
	route, err := client.FindRoute(ctx, targetSystem)
	if err != nil {
		return fmt.Errorf("find_route failed: %w", err)
	}
	if len(route) == 0 {
		if format == formatStyled {
			fmt.Println("Already in target system (or no route found).")
		}
		if targetPOI != "" {
			return autopilotTravelToPOI(client, ctx, targetPOI, format)
		}
		return nil
	}

	// The first entry in the route is the current system — skip it.
	if len(route) > 1 {
		route = route[1:]
	} else {
		if format == formatStyled {
			fmt.Println("Already in target system.")
		}
		if targetPOI != "" {
			return autopilotTravelToPOI(client, ctx, targetPOI, format)
		}
		return nil
	}

	// Parse fuel estimates from the raw find_route response.
	fuelPerJump, estimatedFuel, fuelAvailable := parseFuelEstimates(client)

	totalJumps := len(route)
	if format == formatStyled {
		fmt.Printf("\n Route: %d jump(s) to %s\n", totalJumps, targetSystem)
		for i, step := range route {
			fmt.Printf("   %d. %s\n", i+1, step.Name)
		}
		if estimatedFuel > 0 {
			fmt.Printf("   Fuel: %d per jump, ~%d total, %d available\n", fuelPerJump, estimatedFuel, fuelAvailable)
			if estimatedFuel > fuelAvailable {
				fmt.Printf("   WARNING: Not enough fuel! Need %d more.\n", estimatedFuel-fuelAvailable)
			}
		}

		// Estimate total time: each jump ~2 ticks (jump travel) + ~1 tick (update_system overhead)
		estTicksPerJump := 3
		estTotalTicks := totalJumps * estTicksPerJump
		estWallSecs := estTotalTicks * 10
		fmt.Printf("   Est. time: ~%d ticks (~%s)\n\n", estTotalTicks, formatDuration(estWallSecs))
	}

	startTime := time.Now()

	// Collect raw responses for non-styled formats
	var allResponses []json.RawMessage

	// Step 2: Execute jumps
	for i, step := range route {
		// Check fuel before each jump — use fuel cells if below 10%
		autopilotRefuelIfNeeded(client, ctx, format)

		elapsed := time.Since(startTime)
		remaining := ""
		if i > 0 && format == formatStyled {
			perJump := elapsed / time.Duration(i)
			left := perJump * time.Duration(totalJumps-i)
			remaining = fmt.Sprintf(" | ETA %s", formatDuration(int(left.Seconds())))
		}

		if format == formatStyled {
			fmt.Printf("[Jump %d/%d] Jumping to %s...%s\n", i+1, totalJumps, step.Name, remaining)
		}

		result, err := client.Jump(ctx, step.SystemID)
		if err != nil {
			// If jump failed due to insufficient fuel, try using fuel cells and retry once.
			if strings.Contains(err.Error(), "no_fuel") || strings.Contains(err.Error(), "nsufficient fuel") {
				if autopilotUseFuelCells(client, ctx, format) {
					if format == formatStyled {
						fmt.Printf("  Retrying jump to %s...\n", step.Name)
					}
					result, err = client.Jump(ctx, step.SystemID)
				}
			}
			if err != nil {
				return fmt.Errorf("jump %d/%d to %s failed: %w", i+1, totalJumps, step.Name, err)
			}
		}

		if result.Canceled {
			state := client.GetState()
			if format == formatStyled {
				fmt.Printf("  Jump interrupted! Stopped in %s.\n", state.System.Name)
			}
			return fmt.Errorf("autopilot interrupted at jump %d/%d (combat?)", i+1, totalJumps)
		}

		if format == formatStyled {
			fmt.Printf("  Arrived in %s\n", step.Name)
		}

		// Collect jump response
		if format != formatStyled {
			if raw := client.GetRawJSON("_last"); raw != nil {
				allResponses = append(allResponses, raw)
			}
		}

		// Update KB with new system data
		if globalKB != nil {
			if err := kbUpdateSystem(client, ctx); err != nil {
				if format == formatStyled {
					fmt.Printf("  (KB update failed: %v)\n", err)
				}
			}
			// Always refresh the POI the agent arrived at so the KB records the
			// current location (gate, star, or resource POI) with fresh data.
			if err := kbUpdatePOI(client, ctx); err != nil {
				if format == formatStyled {
					fmt.Printf("  (POI update failed: %v)\n", err)
				}
			}
		}
	}

	// Refresh full state so statusline shows correct location.
	_ = client.GetStatus(ctx)

	totalElapsed := time.Since(startTime)
	if format == formatStyled {
		fmt.Printf("\n Arrived at %s in %s (%d jumps)\n", targetSystem, formatDuration(int(totalElapsed.Seconds())), totalJumps)
	} else {
		// Print all collected raw responses
		for i, raw := range allResponses {
			if len(allResponses) > 1 {
				fmt.Printf("\n--- Autopilot jump %d ---\n", i+1)
			}
			fmt.Printf("%s\n", string(raw))
		}
	}

	// Step 3: Travel to POI if specified
	if targetPOI != "" {
		return autopilotTravelToPOI(client, ctx, targetPOI, format)
	}

	return nil
}

// autopilotTravelToPOI travels to a named POI in the current system.
func autopilotTravelToPOI(client game.GameClient, ctx context.Context, targetPOI string, format outputFormat) error {
	if format == formatStyled {
		fmt.Printf("Traveling to POI: %s...\n", targetPOI)
	}

	result, err := client.Travel(ctx, targetPOI)
	if err != nil {
		return fmt.Errorf("travel to %s failed: %w", targetPOI, err)
	}
	if result.Canceled {
		return fmt.Errorf("travel to %s was interrupted", targetPOI)
	}

	if format == formatStyled {
		fmt.Printf("  Arrived at %s\n", result.POI)
	} else {
		// Print raw travel response
		if raw := client.GetRawJSON("_last"); raw != nil {
			fmt.Printf("%s\n", string(raw))
		}
	}
	return nil
}

// parseFuelEstimates extracts fuel info from the cached find_route response.
func parseFuelEstimates(client game.GameClient) (fuelPerJump, estimatedFuel, fuelAvailable int) {
	raw := client.GetRawJSON("_last")
	if raw == nil {
		return 0, 0, 0
	}
	var resp struct {
		FuelPerJump   int `json:"fuel_per_jump"`
		EstimatedFuel int `json:"estimated_fuel"`
		FuelAvailable int `json:"fuel_available"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, 0, 0
	}
	return resp.FuelPerJump, resp.EstimatedFuel, resp.FuelAvailable
}

// autopilotUseFuelCells uses all fuel_cell items in cargo. Returns true if any were used.
func autopilotUseFuelCells(client game.GameClient, ctx context.Context, format outputFormat) bool {
	state := client.GetState()
	if state == nil {
		return false
	}

	used := false
	for _, item := range state.Ship.Cargo {
		if !strings.Contains(strings.ToLower(item.ItemID), "fuel_cell") || item.Quantity < 1 {
			continue
		}

		qty := int(item.Quantity)
		if format == formatStyled {
			fmt.Printf("  Fuel low — using %d %s from cargo...\n", qty, item.ItemID)
		}
		if err := client.RawCommand(ctx, "use_item", map[string]any{
			"item_id":  item.ItemID,
			"quantity": qty,
		}); err != nil {
			if format == formatStyled {
				fmt.Printf("  Warning: use_item %s failed: %v\n", item.ItemID, err)
			}
			continue
		}
		time.Sleep(game.SleepQuick)
		used = true
	}

	if used {
		// Refresh state — RawCommand doesn't update internal fuel/cargo state.
		_ = client.GetStatus(ctx)
		time.Sleep(game.SleepQuick)
		state = client.GetState()
		if state != nil && state.MaxFuel > 0 {
			if format == formatStyled {
				fmt.Printf("  Fuel now: %.0f/%.0f (%.0f%%)\n", state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100)
			}
		}
	}
	return used
}

// autopilotRefuelIfNeeded checks if fuel is below 10% and uses fuel_cell items
// from cargo to refuel in space.
func autopilotRefuelIfNeeded(client game.GameClient, ctx context.Context, format outputFormat) {
	state := client.GetState()
	if state == nil || state.MaxFuel == 0 {
		return
	}

	fuelPct := (state.Fuel / state.MaxFuel) * 100
	if fuelPct >= 10 {
		return
	}

	// Look for fuel cells in cargo
	for _, item := range state.Ship.Cargo {
		if !strings.Contains(strings.ToLower(item.ItemID), "fuel_cell") {
			continue
		}
		if item.Quantity < 1 {
			continue
		}

		if format == formatStyled {
			fmt.Printf("  Fuel low (%.0f%%) — using %s from cargo...\n", fuelPct, item.ItemID)
		}
		if err := client.RawCommand(ctx, "use_item", map[string]any{
			"item_id": item.ItemID,
		}); err != nil {
			if format == formatStyled {
				fmt.Printf("  Warning: use_item %s failed: %v\n", item.ItemID, err)
			}
			return
		}
		time.Sleep(game.SleepQuick)

		// Refresh state — RawCommand doesn't update internal fuel/cargo state.
		_ = client.GetStatus(ctx)
		time.Sleep(game.SleepQuick)
		state = client.GetState()
		if state != nil && state.MaxFuel > 0 {
			if format == formatStyled {
				fmt.Printf("  Fuel now: %.0f/%.0f (%.0f%%)\n", state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100)
			}
		}
		return
	}

	fmt.Printf("  WARNING: Fuel low (%.0f%%) and no fuel cells in cargo!\n", fuelPct)
}

// formatDuration formats seconds as "Xm Ys" or "Xs".
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	m := seconds / 60
	s := seconds % 60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}
