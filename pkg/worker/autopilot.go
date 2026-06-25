package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureFunc records knowledge-base state at a waypoint. Autopilot calls it
// once after each jump arrival; it is best-effort — a returned error is logged
// to the autopilot's writer and never aborts the route. nil means no capture.
type CaptureFunc func(ctx context.Context) error

// AutopilotRefuelThreshold: when find_route fuel estimates are unavailable, the
// pre-route station refuel triggers if current fuel is below this fraction of capacity.
const AutopilotRefuelThreshold = 0.5

// needsRefuelForRoute reports whether to station-refuel before starting a route. It
// refuels when the route's jump estimate needs more fuel than is available, OR whenever
// the tank is below a fuel-fraction threshold. The threshold check matters because the
// jump estimate ignores in-system POI travel, which also burns fuel — a tank that
// clears the jumps can still strand on the final travel to the station. Returns false if
// capacity is unknown (maxFuel <= 0).
func needsRefuelForRoute(estimatedFuel, fuelAvailable int, fuel, maxFuel, threshold float64) bool {
	if maxFuel <= 0 {
		return false
	}
	if estimatedFuel > 0 && estimatedFuel > fuelAvailable {
		return true
	}
	return fuel/maxFuel < threshold
}

// ensureRouteFuel station-refuels before departing when fuel is short for the route: it
// docks if needed and pays for a full tank (Refuel has no amount). Best-effort — if the
// ship is not at a dockable station the dock fails, so it logs and returns, leaving
// autopilot to fall back to burning cargo fuel cells. Returns the (possibly increased)
// available fuel for display. Shared by every mobile role via Autopilot.
func ensureRouteFuel(ctx context.Context, client game.GameClient, out io.Writer, estimatedFuel, fuelAvailable int) int {
	state := client.GetState()
	if state == nil {
		return fuelAvailable
	}
	fuel, maxFuel := state.GetFuel()
	if !needsRefuelForRoute(estimatedFuel, fuelAvailable, fuel, maxFuel, AutopilotRefuelThreshold) {
		return fuelAvailable
	}
	if !state.IsDocked() {
		if err := client.Dock(ctx); err != nil {
			fmt.Fprintf(out, "  Fuel low and not at a station to refuel (%v); continuing\n", err) //nolint:errcheck
			return fuelAvailable
		}
	}
	if err := client.Refuel(ctx); err != nil {
		fmt.Fprintf(out, "  Station refuel failed: %v\n", err) //nolint:errcheck
		return fuelAvailable
	}
	_ = client.GetStatus(ctx)
	if s := client.GetState(); s != nil {
		f2, m2 := s.GetFuel()
		fmt.Fprintf(out, "  Refueled at station: %.0f/%.0f\n", f2, m2) //nolint:errcheck
		return int(f2)
	}
	return fuelAvailable
}

// AutopilotDeps are the injected dependencies for Autopilot.
type AutopilotDeps struct {
	Client     game.GameClient
	Out        io.Writer   // progress lines; nil -> io.Discard
	OnWaypoint CaptureFunc // per-arrival capture; nil -> no-op
}

// Autopilot executes a multi-jump route to targetSystem, then travels to
// targetPOI within the destination system when targetPOI != "". It uses
// FindRoute for the route, jumps each hop (attempting fuel-cell use / refuel
// when fuel runs short), invokes OnWaypoint after each arrival, and performs
// the final in-system Travel. Returns on FindRoute failure, a jump that fails
// after fuel attempts, a jump interruption, or ctx cancellation.
func Autopilot(ctx context.Context, deps AutopilotDeps, targetSystem, targetPOI string) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	client := deps.Client

	fmt.Fprintf(out, "Finding route to %s...\n", targetSystem) //nolint:errcheck
	route, err := client.FindRoute(ctx, targetSystem)
	if err != nil {
		return fmt.Errorf("find_route failed: %w", err)
	}
	// The first entry is the current system; a route of <=1 means we are
	// already in the target system.
	if len(route) <= 1 {
		fmt.Fprintln(out, "Already in target system (or no route found).") //nolint:errcheck
		if targetPOI != "" {
			return autopilotTravelToPOI(ctx, client, out, targetPOI)
		}
		return nil
	}
	route = route[1:]

	fuelPerJump, estimatedFuel, fuelAvailable := parseFuelEstimates(client)
	// Top up at the origin station if the route needs more fuel than we have. Without
	// this, a mobile worker carrying no fuel cells (e.g. a hauler) strands on jump 1.
	fuelAvailable = ensureRouteFuel(ctx, client, out, estimatedFuel, fuelAvailable)
	totalJumps := len(route)
	fmt.Fprintf(out, "\n Route: %d jump(s) to %s\n", totalJumps, targetSystem) //nolint:errcheck
	for i, step := range route {
		fmt.Fprintf(out, "   %d. %s\n", i+1, step.Name) //nolint:errcheck
	}
	if estimatedFuel > 0 {
		fmt.Fprintf(out, "   Fuel: %d per jump, ~%d total, %d available\n", fuelPerJump, estimatedFuel, fuelAvailable) //nolint:errcheck
		if estimatedFuel > fuelAvailable {
			fmt.Fprintf(out, "   WARNING: Not enough fuel! Need %d more.\n", estimatedFuel-fuelAvailable) //nolint:errcheck
		}
	}
	// Each jump ~2 ticks travel + ~1 tick update overhead.
	estTotalTicks := totalJumps * 3
	fmt.Fprintf(out, "   Est. time: ~%d ticks (~%s)\n\n", estTotalTicks, FormatDuration(estTotalTicks*10)) //nolint:errcheck

	startTime := time.Now()
	for i, step := range route {
		// Check fuel before each jump — use fuel cells if below 10%.
		autopilotRefuelIfNeeded(ctx, client, out)

		remaining := ""
		if i > 0 {
			perJump := time.Since(startTime) / time.Duration(i)
			left := perJump * time.Duration(totalJumps-i)
			remaining = fmt.Sprintf(" | ETA %s", FormatDuration(int(left.Seconds())))
		}
		fmt.Fprintf(out, "[Jump %d/%d] Jumping to %s...%s\n", i+1, totalJumps, step.Name, remaining) //nolint:errcheck

		result, err := client.Jump(ctx, step.SystemID)
		if err != nil {
			// Insufficient fuel: try fuel cells and retry once.
			if strings.Contains(err.Error(), "no_fuel") || strings.Contains(err.Error(), "nsufficient fuel") {
				if autopilotUseFuelCells(ctx, client, out) {
					fmt.Fprintf(out, "  Retrying jump to %s...\n", step.Name) //nolint:errcheck
					result, err = client.Jump(ctx, step.SystemID)
				}
			}
			if err != nil {
				return fmt.Errorf("jump %d/%d to %s failed: %w", i+1, totalJumps, step.Name, err)
			}
		}
		if result.Canceled {
			name := targetSystem
			if state := client.GetState(); state != nil {
				name = state.System.Name
			}
			fmt.Fprintf(out, "  Jump interrupted! Stopped in %s.\n", name) //nolint:errcheck
			return fmt.Errorf("autopilot interrupted at jump %d/%d (combat?)", i+1, totalJumps)
		}
		fmt.Fprintf(out, "  Arrived in %s\n", step.Name) //nolint:errcheck

		if deps.OnWaypoint != nil {
			if err := deps.OnWaypoint(ctx); err != nil {
				fmt.Fprintf(out, "  (waypoint capture failed: %v)\n", err) //nolint:errcheck
			}
		}
	}

	// Refresh full state so the caller's statusline shows the correct location.
	_ = client.GetStatus(ctx)
	fmt.Fprintf(out, "\n Arrived at %s in %s (%d jumps)\n", //nolint:errcheck
		targetSystem, FormatDuration(int(time.Since(startTime).Seconds())), totalJumps)

	if targetPOI != "" {
		return autopilotTravelToPOI(ctx, client, out, targetPOI)
	}
	return nil
}

// autopilotTravelToPOI travels to a named POI in the current system.
func autopilotTravelToPOI(ctx context.Context, client game.GameClient, out io.Writer, targetPOI string) error {
	fmt.Fprintf(out, "Traveling to POI: %s...\n", targetPOI) //nolint:errcheck
	result, err := client.Travel(ctx, targetPOI)
	if err != nil {
		return fmt.Errorf("travel to %s failed: %w", targetPOI, err)
	}
	if result.Canceled {
		return fmt.Errorf("travel to %s was interrupted", targetPOI)
	}
	fmt.Fprintf(out, "  Arrived at %s\n", result.POI) //nolint:errcheck
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

// autopilotUseFuelCells uses all fuel_cell items in cargo. Returns true if any
// were used.
func autopilotUseFuelCells(ctx context.Context, client game.GameClient, out io.Writer) bool {
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
		fmt.Fprintf(out, "  Fuel low — using %d %s from cargo...\n", qty, item.ItemID) //nolint:errcheck
		if err := client.RawCommand(ctx, "use_item", map[string]any{
			"item_id":  item.ItemID,
			"quantity": qty,
		}); err != nil {
			fmt.Fprintf(out, "  Warning: use_item %s failed: %v\n", item.ItemID, err) //nolint:errcheck
			continue
		}
		time.Sleep(game.SleepQuick)
		used = true
	}
	if used {
		// Refresh state — RawCommand doesn't update internal fuel/cargo state.
		_ = client.GetStatus(ctx)
		time.Sleep(game.SleepQuick)
		if state = client.GetState(); state != nil && state.MaxFuel > 0 {
			fmt.Fprintf(out, "  Fuel now: %.0f/%.0f (%.0f%%)\n", state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100) //nolint:errcheck
		}
	}
	return used
}

// autopilotRefuelIfNeeded uses fuel_cell items from cargo when fuel is below 10%.
func autopilotRefuelIfNeeded(ctx context.Context, client game.GameClient, out io.Writer) {
	state := client.GetState()
	if state == nil || state.MaxFuel == 0 {
		return
	}
	fuelPct := (state.Fuel / state.MaxFuel) * 100
	if fuelPct >= 10 {
		return
	}
	for _, item := range state.Ship.Cargo {
		if !strings.Contains(strings.ToLower(item.ItemID), "fuel_cell") || item.Quantity < 1 {
			continue
		}
		fmt.Fprintf(out, "  Fuel low (%.0f%%) — using %s from cargo...\n", fuelPct, item.ItemID) //nolint:errcheck
		if err := client.RawCommand(ctx, "use_item", map[string]any{"item_id": item.ItemID}); err != nil {
			fmt.Fprintf(out, "  Warning: use_item %s failed: %v\n", item.ItemID, err) //nolint:errcheck
			return
		}
		time.Sleep(game.SleepQuick)
		_ = client.GetStatus(ctx)
		time.Sleep(game.SleepQuick)
		if state = client.GetState(); state != nil && state.MaxFuel > 0 {
			fmt.Fprintf(out, "  Fuel now: %.0f/%.0f (%.0f%%)\n", state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100) //nolint:errcheck
		}
		return
	}
	fmt.Fprintf(out, "  WARNING: Fuel low (%.0f%%) and no fuel cells in cargo!\n", fuelPct) //nolint:errcheck
}

// FormatDuration formats seconds as "Xm Ys", "Xm", or "Xs".
func FormatDuration(seconds int) string {
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
