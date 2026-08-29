package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
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
		// Coverage, not position, is what the walk is steering by now, so
		// report that instead of the anchor's coordinates.
		if surveyed, total, ok := surveyCoverage(ctx); ok {
			fmt.Printf("   Coverage: %d of %d systems surveyed (%.0f%%)\n",
				surveyed, total, 100*float64(surveyed)/float64(total))
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
		next, reason := pickNextSystem(ctx, state, visited)
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

// pickNextSystem chooses the adjacent system to jump to next.
//
// The goal of auto-explore is eventual coverage of the whole galaxy, so the
// target is the NEAREST ELIGIBLE system -- never surveyed, or surveyed longer
// ago than game.FreshnessSystem -- found by a breadth-first walk of the
// connection graph, and the return value is the first hop along the way.
//
// This replaces a picker that considered only the current system's immediate
// connections and held its visited set in memory. That produced four distinct
// failures, all of the same root:
//
//   - a server restart made it re-target the system it had just come from,
//     because the visited set died with the process;
//   - it re-surveyed systems it had finished minutes earlier, for the same
//     reason;
//   - it stopped dead with "all connections already visited" as soon as the
//     immediate neighbourhood was done, because nothing looked further;
//   - and it walled itself into pockets of the map, because a corridor it had
//     just crossed counted as visited and so could not be re-entered.
//
// The old "farthest from anchor" tiebreak is gone with it. That biased the walk
// away from gaps it had left behind, which is reasonable for a local wander and
// wrong when the objective is covering everything.
//
// Falling back to the old immediate-neighbour behaviour when the KB is
// unavailable is deliberate: an explorer with no graph should still explore.
func pickNextSystem(ctx context.Context, state *game.State, visited map[string]bool) (string, string) {
	connections := state.System.Connections
	if len(connections) == 0 {
		return "", "no connections from current system"
	}

	adjacency, elig, err := loadFrontier(ctx, visited)
	if err != nil {
		return pickNearestUnvisitedNeighbor(connections, visited)
	}

	// The live reply is more current than the KB's graph, so fold this
	// system's connections in. A system reached through a wormhole may have
	// links the stored map has never seen.
	from := state.System.ID
	if from == "" {
		from = state.CurrentSystem
	}
	for _, c := range connections {
		if c.SystemID != "" && !slices.Contains(adjacency[from], c.SystemID) {
			adjacency[from] = append(adjacency[from], c.SystemID)
		}
	}

	hop, target, jumps, ok := nextHopToward(adjacency, from, elig)
	if !ok {
		return pickNearestUnvisitedNeighbor(connections, visited)
	}
	if target == hop {
		return hop, "nearest unsurveyed system"
	}

	return hop, fmt.Sprintf("toward %s (%d jumps)", target, jumps)
}

// pickNearestUnvisitedNeighbor is the fallback for when the connection graph
// cannot be read: any neighbour not yet seen this run, by name for determinism.
func pickNearestUnvisitedNeighbor(connections []game.ConnectionInfo, visited map[string]bool) (string, string) {
	var names []string
	byName := make(map[string]string, len(connections))
	for _, c := range connections {
		if visited[c.SystemID] {
			continue
		}
		names = append(names, c.Name)
		byName[c.Name] = c.SystemID
	}
	if len(names) == 0 {
		return "", fmt.Sprintf("all %d connections already visited and no graph to widen into", len(connections))
	}
	slices.Sort(names)

	return byName[names[0]], "unvisited neighbor (no graph)"
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

// surveyCoverage reports how much of the known galaxy has been surveyed, for
// the opening line of an auto-explore run. Best-effort: a KB that cannot answer
// produces no line rather than an error.
func surveyCoverage(ctx context.Context) (surveyed, total int, ok bool) {
	if globalKB == nil {
		return 0, 0, false
	}
	systems, err := globalKB.GetSystems(ctx)
	if err != nil || len(systems) == 0 {
		return 0, 0, false
	}
	seen, err := knowledge.SystemsLastSurveyed(ctx, globalKB)
	if err != nil {
		return 0, 0, false
	}

	return len(seen), len(systems), true
}
