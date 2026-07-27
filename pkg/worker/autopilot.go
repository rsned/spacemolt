package worker

import (
	"context"
	"encoding/json"
	"errors"
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

// WaypointCheckFunc is an optional per-arrival decision hook (distinct from the
// capture-only CaptureFunc). Autopilot calls it after each jump arrival; returning
// stop=true makes Autopilot halt at the current system and return errAutopilotStopped
// so the caller can re-plan. A returned error is logged and treated as "do not stop"
// (best-effort — the watchdog never aborts a route on its own failure). nil means no check.
type WaypointCheckFunc func(ctx context.Context) (stop bool, err error)

// errAutopilotStopped is returned when a WaypointCheckFunc requests an early stop.
// Callers detect it with errors.Is to distinguish a deliberate re-plan from a failure.
var errAutopilotStopped = errors.New("autopilot stopped early by waypoint check")

// AutopilotRefuelThreshold: when find_route fuel estimates are unavailable, the
// pre-route station refuel triggers if current fuel is below this fraction of capacity.
const AutopilotRefuelThreshold = 0.5

// AutopilotDeferReserveJumps is the fuel headroom, expressed in jump-equivalents, that
// must remain beyond a route's jump estimate before a refuel may be DEFERRED to a
// cheaper destination. It exists because estimatedFuel covers jumps ONLY -- the
// in-system POI travel at both ends also burns fuel -- so a tank that merely clears the
// estimate can still strand on the final hop. Stranding costs a rescue, which dwarfs
// any endpoint price spread, so this is deliberately generous.
const AutopilotDeferReserveJumps = 2

// fuelTiming carries the endpoint fuel prices for a refuel-timing decision. The zero
// value is Comparable=false, which reproduces the legacy price-blind behavior exactly --
// so every caller that does not opt in is unaffected.
type fuelTiming struct {
	OriginPrice float64
	DestPrice   float64
	// Comparable is true only when BOTH endpoint prices are known. One unknown end
	// makes the comparison meaningless, and guessing is how a worker strands.
	Comparable bool
	// Reserve is the fuel headroom required beyond the route estimate before a
	// refuel may be deferred. Zero disables deferral.
	Reserve int
}

// needsRefuelForRoute reports whether to station-refuel before starting a route. It
// refuels when the route's jump estimate needs more fuel than is available, OR whenever
// the tank is below a fuel-fraction threshold. The threshold check matters because the
// jump estimate ignores in-system POI travel, which also burns fuel — a tank that
// clears the jumps can still strand on the final travel to the station. Returns false if
// capacity is unknown (maxFuel <= 0).
func needsRefuelForRoute(estimatedFuel, fuelAvailable int, fuel, maxFuel, threshold float64) bool {
	return needsRefuelForRouteAt(estimatedFuel, fuelAvailable, fuel, maxFuel, threshold, fuelTiming{})
}

// needsRefuelForRouteAt is needsRefuelForRoute with endpoint price comparison. Given an
// incomparable timing it is exactly needsRefuelForRoute.
//
// A station refuel always fills the tank to full and charges only for the gap (the
// server ignores quantity for credit refuels), so the purchase is all-or-nothing per
// visit and its cost scales with how empty the tank is. Total units burned over a
// journey is fixed by the route, so the only free variable is the price paid per unit:
// buy at the cheaper of the two stations you were going to visit anyway.
func needsRefuelForRouteAt(estimatedFuel, fuelAvailable int, fuel, maxFuel, threshold float64, ft fuelTiming) bool {
	if maxFuel <= 0 {
		return false
	}
	// Forced: the route needs more fuel than we hold. No price makes this optional.
	if estimatedFuel > 0 && estimatedFuel > fuelAvailable {
		return true
	}
	if !ft.Comparable {
		return fuel/maxFuel < threshold
	}
	// No dearer here than at the destination: buy now and fill completely, even above
	// the threshold. Since the refuel fills to full either way, a cheap stop also
	// pre-pays the next leg. Equal prices buy here too — waiting gains nothing and
	// risks the destination's price having drifted (it tracks bunker reserve level).
	if ft.OriginPrice <= ft.DestPrice {
		return fuel < maxFuel
	}
	// Dearer here than there: defer the purchase, but only with headroom to spare.
	if estimatedFuel > 0 && ft.Reserve > 0 && fuelAvailable >= estimatedFuel+ft.Reserve {
		return false
	}
	return fuel/maxFuel < threshold
}

// ensureRouteFuel station-refuels before departing when fuel is short for the route: it
// docks if needed and pays for a full tank (Refuel has no amount). Best-effort — if the
// ship is not at a dockable station the dock fails, so it logs and returns, leaving
// autopilot to fall back to burning cargo fuel cells. Returns the (possibly increased)
// available fuel for display. Shared by every mobile role via Autopilot.
func ensureRouteFuel(ctx context.Context, client game.GameClient, out io.Writer, estimatedFuel, fuelAvailable int, ft fuelTiming) int {
	state := client.GetState()
	if state == nil {
		return fuelAvailable
	}
	fuel, maxFuel := state.GetFuel()
	if !needsRefuelForRouteAt(estimatedFuel, fuelAvailable, fuel, maxFuel, AutopilotRefuelThreshold, ft) {
		// Make a deliberate deferral visible: otherwise "departed on a half tank"
		// is indistinguishable in the log from the old threshold behavior.
		if ft.Comparable && ft.OriginPrice > ft.DestPrice && fuel < maxFuel {
			fmt.Fprintf(out, "  Fuel %.0f/%.0f: deferring refuel (%.0f/unit here vs %.0f at destination)\n", //nolint:errcheck
				fuel, maxFuel, ft.OriginPrice, ft.DestPrice)
		}
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
	Out        io.Writer         // progress lines; nil -> io.Discard
	OnWaypoint CaptureFunc       // per-arrival capture; nil -> no-op
	// WaypointCheck is an optional per-arrival early-stop decision (re-route hook);
	// nil -> never stops. It runs after OnWaypoint on each jump arrival.
	WaypointCheck WaypointCheckFunc
	// FuelPriceAt opts this route into price-aware refuel timing: buy fuel at the
	// cheaper of the origin and destination stations rather than topping off blindly
	// at AutopilotRefuelThreshold. nil (the default for every role that has not opted
	// in) leaves the legacy price-blind behavior untouched.
	FuelPriceAt FuelPriceAt
}

// fuelTimingFor resolves the endpoint fuel prices for a route into a refuel-timing
// decision input. It returns the zero (incomparable) value — i.e. exactly the legacy
// price-blind behavior — when the role has not opted in, when there is no distinct
// destination station, or when EITHER endpoint's price is unknown. Roughly half the
// galaxy's stations have never been price-captured, so "unknown" is the common case and
// must stay safe.
func (d AutopilotDeps) fuelTimingFor(destStation string, fuelPerJump int) fuelTiming {
	if d.FuelPriceAt == nil || destStation == "" || fuelPerJump <= 0 {
		return fuelTiming{}
	}
	state := d.Client.GetState()
	if state == nil || state.CurrentPOI == "" || state.CurrentPOI == destStation {
		return fuelTiming{}
	}
	originPrice, originOK := d.FuelPriceAt(state.CurrentPOI)
	destPrice, destOK := d.FuelPriceAt(destStation)
	if !originOK || !destOK {
		return fuelTiming{}
	}
	return fuelTiming{
		OriginPrice: originPrice,
		DestPrice:   destPrice,
		Comparable:  true,
		Reserve:     AutopilotDeferReserveJumps * fuelPerJump,
	}
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
	// With FuelPriceAt configured this also defers a merely-optional top-off when the
	// destination sells fuel cheaper.
	fuelAvailable = ensureRouteFuel(ctx, client, out, estimatedFuel, fuelAvailable, deps.fuelTimingFor(targetPOI, fuelPerJump))
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

		// Optional per-arrival early-stop: lets a re-route watchdog halt the route at
		// this system so the caller can re-plan. Check errors are non-fatal (do not stop).
		if deps.WaypointCheck != nil {
			if stop, err := deps.WaypointCheck(ctx); err != nil {
				fmt.Fprintf(out, "  (waypoint check failed: %v)\n", err) //nolint:errcheck
			} else if stop {
				fmt.Fprintf(out, "  Waypoint check requested early stop at %s\n", step.Name) //nolint:errcheck
				return errAutopilotStopped
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

// autopilotTravelToPOI travels to a named POI in the current system. If the server rejects
// the hop for insufficient fuel, it tops up in place (station refuel, then cargo fuel cells)
// and retries once. Without this, a hauler that arrives in the sell system low on fuel loops
// forever on the in-system sell hop — the multi-jump path refuels via ensureRouteFuel, but
// this intra-system path previously went straight to Travel with whatever fuel was left.
func autopilotTravelToPOI(ctx context.Context, client game.GameClient, out io.Writer, targetPOI string) error {
	fmt.Fprintf(out, "Traveling to POI: %s...\n", targetPOI) //nolint:errcheck
	result, err := client.Travel(ctx, targetPOI)
	if err != nil && isInsufficientFuelErr(err) && refuelInPlace(ctx, client, out) {
		fmt.Fprintf(out, "  Refueled; retrying travel to %s...\n", targetPOI) //nolint:errcheck
		result, err = client.Travel(ctx, targetPOI)
	}
	if err != nil {
		return fmt.Errorf("travel to %s failed: %w", targetPOI, err)
	}
	if result.Canceled {
		return fmt.Errorf("travel to %s was interrupted", targetPOI)
	}
	fmt.Fprintf(out, "  Arrived at %s\n", result.POI) //nolint:errcheck
	return nil
}

// isInsufficientFuelErr reports whether a travel/jump error is the server's out-of-fuel
// rejection. Matches the same substrings the jump path keys on.
func isInsufficientFuelErr(err error) bool {
	s := err.Error()
	return strings.Contains(s, "no_fuel") || strings.Contains(s, "nsufficient fuel")
}

// refuelInPlace forces a fuel top-up for an in-system hop the server rejected for insufficient
// fuel: it docks at the current station if needed, station-refuels, and falls back to burning
// cargo fuel cells when no station refuel is available. Returns true if a refuel action
// succeeded, so the caller should retry the hop.
func refuelInPlace(ctx context.Context, client game.GameClient, out io.Writer) bool {
	if state := client.GetState(); state != nil {
		if !state.IsDocked() {
			if err := client.Dock(ctx); err != nil {
				fmt.Fprintf(out, "  Fuel low and cannot dock to refuel (%v)\n", err) //nolint:errcheck
			}
		}
		if s := client.GetState(); s != nil && s.IsDocked() {
			if err := client.Refuel(ctx); err != nil {
				fmt.Fprintf(out, "  Station refuel failed: %v\n", err) //nolint:errcheck
			} else {
				_ = client.GetStatus(ctx)
				if s2 := client.GetState(); s2 != nil {
					fNow, mNow := s2.GetFuel()
					fmt.Fprintf(out, "  Refueled at station: %.0f/%.0f\n", fNow, mNow) //nolint:errcheck
				}
				return true
			}
		}
	}
	// Station refuel unavailable or failed — fall back to cargo fuel cells.
	return autopilotUseFuelCells(ctx, client, out)
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
