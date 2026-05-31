package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

const (
	autoExploreDefaultMaxHops = 10
	// Fuel threshold (percentage) under which we attempt to refuel from
	// cargo fuel_cells between system hops while NOT docked at a station.
	autoExploreLowFuelPct = 30.0
)

// autoExplore drives a tour across multiple systems, running a full explore
// in each before jumping to the next. Selection favors unvisited neighbors,
// preferring those farther from the starting system's position (so the
// tour drifts "outward" from the agent's home base when invoked from home).
//
// Flags:
//
//	--max-hops=N    maximum number of systems to visit (default 10)
//
// Stops when:
//   - --max-hops reached,
//   - no connections remain (all visited / isolated system),
//   - a jump fails,
//   - fuel is exhausted with no cargo fuel_cells to burn.
func autoExplore(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	maxHops := autoExploreDefaultMaxHops
	if len(parts) > 1 {
		flags, err := parseFlagArgs(parts[1:], "max_hops", "max-hops")
		if err != nil {
			return err
		}
		if v, ok := flags["max_hops"]; ok {
			if n, ok := v.(int); ok && n > 0 {
				maxHops = n
			}
		}
		if v, ok := flags["max-hops"]; ok {
			if n, ok := v.(int); ok && n > 0 {
				maxHops = n
			}
		}
		// Also accept a bare numeric positional arg for ergonomics:
		//   auto_explore 5
		if len(parts) == 2 && !strings.HasPrefix(parts[1], "--") {
			if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 {
				maxHops = n
			}
		}
	}

	// Refresh state so we have current system + position info.
	if err := client.GetSystem(ctx); err != nil {
		return fmt.Errorf("get_system failed: %w", err)
	}
	time.Sleep(game.SleepQuick)

	state := client.GetState()
	if state == nil || state.System.ID == "" {
		return fmt.Errorf("no system data available")
	}

	anchorSystemID := state.System.ID
	anchorPos, anchorHasPos := systemPosition(ctx, anchorSystemID)

	if format == formatStyled {
		fmt.Printf("\n🚀 Auto-explore starting from %s", state.System.Name)
		if anchorHasPos {
			fmt.Printf(" @ (%.1f, %.1f)", anchorPos.X, anchorPos.Y)
		}
		fmt.Printf("\n   Max hops: %d\n", maxHops)
		if !anchorHasPos {
			fmt.Printf("   Note: anchor position unknown (KB missing system data) — "+
				"destination selection will not prefer 'outward' direction%s\n", "")
		}
		fmt.Println()
	}

	visited := map[string]bool{anchorSystemID: true}
	systemsExplored := 0
	currentSystemName := state.System.Name

	// Collect raw responses for non-styled formats
	var allResponses []json.RawMessage

	for hop := 0; hop < maxHops; hop++ {
		if format == formatStyled {
			fmt.Printf("━━━ Hop %d/%d: exploring %s ━━━\n", hop+1, maxHops, currentSystemName)
		}

		if err := exploreSystem(client, ctx, true, format); err != nil {
			if format == formatStyled {
				fmt.Printf("  Explore reported: %v (continuing)\n", err)
			}
		}
		systemsExplored++

		// Opportunistic in-space refuel from cargo fuel_cells when low.
		refuelFromCargoIfLow(client, ctx, format)

		// Refresh state after explore; we may have landed at a POI or be in space.
		state = client.GetState()
		if state == nil {
			return fmt.Errorf("state unavailable after explore")
		}

		// Pick next system.
		next, reason := pickNextSystem(ctx, state, anchorSystemID, anchorPos, anchorHasPos, visited)
		if next == "" {
			if format == formatStyled {
				fmt.Printf("\n🛑 Stopping: %s\n", reason)
			}
			break
		}

		// Undock if still docked (needed for jump).
		if state.Doc {
			if format == formatStyled {
				fmt.Printf("  Undocking for jump...\n")
			}
			if err := client.Undock(ctx); err != nil {
				if format == formatStyled {
					fmt.Printf("  Undock warning: %v\n", err)
				}
			}
			time.Sleep(game.SleepTick)
			// Collect undock response
			if format != formatStyled {
				if raw := client.GetRawJSON("_last"); raw != nil {
					allResponses = append(allResponses, raw)
				}
			}
		}

		if format == formatStyled {
			fmt.Printf("\n🌌 Jumping to %s (%s)\n", next, reason)
		}
		result, err := client.Jump(ctx, next)
		if err != nil {
			return fmt.Errorf("jump to %s failed: %w", next, err)
		}
		if result != nil && result.Canceled {
			return fmt.Errorf("jump to %s canceled mid-travel", next)
		}
		if result != nil && result.SystemName != "" {
			currentSystemName = result.SystemName
		} else {
			currentSystemName = next
		}
		// Collect jump response
		if format != formatStyled {
			if raw := client.GetRawJSON("_last"); raw != nil {
				allResponses = append(allResponses, raw)
			}
		}
		visited[next] = true
		time.Sleep(game.SleepJump)
	}

	if format == formatStyled {
		fmt.Printf("\n✅ Auto-explore complete: %d systems visited\n", systemsExplored)
	} else {
		// Print all collected raw responses
		for i, raw := range allResponses {
			if len(allResponses) > 1 {
				fmt.Printf("\n--- Auto-explore response %d ---\n", i+1)
			}
			fmt.Printf("%s\n", string(raw))
		}
	}
	return nil
}

// refuelFromCargoIfLow burns fuel_cell items from cargo if current fuel is
// below autoExploreLowFuelPct. Only acts when the agent is NOT docked
// (at stations, explore() already refueled via refuelAtStations).
func refuelFromCargoIfLow(client game.GameClient, ctx context.Context, format outputFormat) {
	state := client.GetState()
	if state == nil || state.MaxFuel == 0 || state.Doc {
		return
	}
	fuelPct := (state.Fuel / state.MaxFuel) * 100
	if fuelPct >= autoExploreLowFuelPct {
		return
	}

	// Look for any fuel_cell variant in cargo.
	var found bool
	for _, item := range state.Ship.Cargo {
		if strings.Contains(strings.ToLower(item.ItemID), "fuel_cell") && item.Quantity >= 1 {
			found = true
			break
		}
	}
	if !found {
		if format == formatStyled {
			fmt.Printf("  Fuel low (%.0f%%) but no fuel_cell in cargo — continuing\n", fuelPct)
		}
		return
	}

	if format == formatStyled {
		fmt.Printf("  Fuel low (%.0f%%) — burning cargo fuel_cell via refuel command\n", fuelPct)
	}
	if err := client.Refuel(ctx); err != nil {
		if !isTankFullError(err) {
			if format == formatStyled {
				fmt.Printf("  Refuel warning: %v\n", err)
			}
		}
		return
	}
	time.Sleep(game.SleepQuick)
	_ = client.GetStatus(ctx)
	time.Sleep(game.SleepQuick)
	if state = client.GetState(); state != nil && state.MaxFuel > 0 {
		if format == formatStyled {
			fmt.Printf("  Fuel now: %.0f/%.0f (%.0f%%)\n",
				state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100)
		}
	}
}

// pickNextSystem chooses the next system to jump to from the current
// system's connections. Unvisited neighbors win; among those, if we have
// anchor position data, prefer systems farther from the anchor. Returns
// the chosen system ID and a human-readable reason string, or ("", reason)
// to signal no valid candidate.
func pickNextSystem(
	ctx context.Context,
	state *game.State,
	anchorSystemID string,
	anchorPos game.Position,
	anchorHasPos bool,
	visited map[string]bool,
) (string, string) {
	connections := state.System.Connections
	if len(connections) == 0 {
		return "", "no connections from current system"
	}

	type candidate struct {
		id       string
		name     string
		distFrom float64 // distance from anchor (0 if unknown)
		hasPos   bool
	}
	var unvisited []candidate
	for _, c := range connections {
		if visited[c.SystemID] {
			continue
		}
		cand := candidate{id: c.SystemID, name: c.Name}
		if anchorHasPos {
			if pos, ok := systemPosition(ctx, c.SystemID); ok {
				cand.distFrom = math.Hypot(pos.X-anchorPos.X, pos.Y-anchorPos.Y)
				cand.hasPos = true
			}
		}
		unvisited = append(unvisited, cand)
	}

	if len(unvisited) == 0 {
		if visited[anchorSystemID] && len(visited) == 1 {
			return "", "starting system has no reachable neighbors"
		}
		return "", fmt.Sprintf("all %d connections already visited", len(connections))
	}

	// Sort: candidates with known position and farther from anchor come first.
	// Stable fallback by name for deterministic ordering on ties / no-data.
	sort.SliceStable(unvisited, func(i, j int) bool {
		a, b := unvisited[i], unvisited[j]
		if a.hasPos && b.hasPos {
			return a.distFrom > b.distFrom
		}
		if a.hasPos != b.hasPos {
			return a.hasPos // prefer ones with known position
		}
		return a.name < b.name
	})

	pick := unvisited[0]
	reason := "unvisited neighbor"
	if pick.hasPos && anchorHasPos {
		reason = fmt.Sprintf("farthest unvisited neighbor (%.1f from anchor)", pick.distFrom)
	}
	return pick.id, reason
}

// systemPosition returns the position of a system from the knowledge base,
// if available. Returns (zero, false) when the KB is not loaded or the
// system isn't known.
func systemPosition(ctx context.Context, systemID string) (game.Position, bool) {
	if globalKB == nil || systemID == "" {
		return game.Position{}, false
	}
	sys, err := globalKB.GetSystem(ctx, systemID)
	if err != nil || sys == nil {
		return game.Position{}, false
	}
	// Treat all-zero positions as unknown so tie-breaking falls back cleanly.
	if sys.Position.X == 0 && sys.Position.Y == 0 {
		return game.Position{}, false
	}
	return sys.Position, true
}
