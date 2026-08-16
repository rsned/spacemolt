package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// TestAutopilotTravelToPOIRefuelsAndRetriesOnInsufficientFuel covers the in-system sell-leg
// fuel bug: when the ship is already in the target system and the server rejects the POI hop
// for insufficient fuel, autopilot must station-refuel and retry the hop once — instead of
// failing out (which stranded haulers in a "leaving claimed" loop). The multi-jump path
// already refuels via ensureRouteFuel; the intra-system path previously did not.
func TestAutopilotTravelToPOIRefuelsAndRetriesOnInsufficientFuel(t *testing.T) {
	f := &fakeClient{
		state:   &game.State{Fuel: 5, MaxFuel: 100, Doc: true}, // docked at a station, low fuel
		route:   []game.RouteStep{{SystemID: "a", Name: "A"}},  // already in target system -> intra-system hop
		fuelLow: true,                                          // Travel fails until a refuel clears it
	}
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard}, "a", "a-stn")
	if err != nil {
		t.Fatalf("Autopilot should recover via refuel+retry on an in-system hop, got: %v", err)
	}
	travels := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "travel:") {
			travels++
		}
	}
	if travels != 2 {
		t.Errorf("want 2 travel attempts (fail -> refuel -> retry), got %d in %v", travels, f.calls)
	}
	if !slices.Contains(f.calls, "refuel") {
		t.Errorf("want a refuel between the two travel attempts, got %v", f.calls)
	}
}

func autopilotFake() *fakeClient {
	return &fakeClient{
		// Fuel full so autopilotRefuelIfNeeded no-ops; empty cargo.
		state: &game.State{Fuel: 100, MaxFuel: 100},
		route: []game.RouteStep{
			{SystemID: "sys_a", Name: "Alpha"}, // current system, skipped
			{SystemID: "sys_b", Name: "Bravo"},
			{SystemID: "sys_c", Name: "Charlie"},
		},
	}
}

func TestAutopilotJumpsEachHopAndCaptures(t *testing.T) {
	f := autopilotFake()
	var captures int
	err := Autopilot(context.Background(), AutopilotDeps{
		Client:     f,
		Out:        io.Discard,
		OnWaypoint: func(ctx context.Context) error { captures++; return nil },
	}, "sys_c", "")
	if err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	// First route entry (current system) is skipped; two jumps follow.
	if !slices.Contains(f.calls, "jump:sys_b") || !slices.Contains(f.calls, "jump:sys_c") {
		t.Fatalf("expected jumps to sys_b and sys_c, got %v", f.calls)
	}
	if captures != 2 {
		t.Errorf("OnWaypoint called %d times, want 2 (one per arrival)", captures)
	}
	// No POI -> no final travel.
	for _, c := range f.calls {
		if len(c) >= 7 && c[:7] == "travel:" {
			t.Errorf("unexpected travel call %q with empty POI", c)
		}
	}
}

func TestAutopilotWaypointCheckStopsEarly(t *testing.T) {
	f := autopilotFake() // route: current -> sys_b -> sys_c (2 jumps)
	calls := 0
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f,
		Out:    io.Discard,
		WaypointCheck: func(_ context.Context) (bool, error) {
			calls++
			return true, nil // stop after the first arrival (sys_b)
		},
	}, "sys_c", "")
	if !errors.Is(err, errAutopilotStopped) {
		t.Fatalf("want errAutopilotStopped, got %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_b") {
		t.Fatalf("want the first jump to sys_b, got %v", f.calls)
	}
	if slices.Contains(f.calls, "jump:sys_c") {
		t.Fatalf("must stop before jumping to sys_c, got %v", f.calls)
	}
	if calls != 1 {
		t.Errorf("WaypointCheck called %d times, want 1 (stopped after first arrival)", calls)
	}
}

func TestAutopilotWaypointCheckErrorIsNonFatal(t *testing.T) {
	f := autopilotFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard,
		WaypointCheck: func(_ context.Context) (bool, error) { return false, errors.New("boom") },
	}, "sys_c", "")
	if err != nil {
		t.Fatalf("a WaypointCheck error must not abort the route, got %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_c") {
		t.Fatalf("route should complete despite check errors, got %v", f.calls)
	}
}

func TestNeedsRefuelForRoute(t *testing.T) {
	tests := []struct {
		name                         string
		estimatedFuel, fuelAvailable int
		fuel, maxFuel, threshold     float64
		want                         bool
	}{
		{"route needs more than available", 50, 10, 80, 100, 0.5, true},
		{"route within available", 10, 50, 80, 100, 0.5, false},
		{"jumps fine but tank below threshold", 10, 50, 20, 100, 0.5, true},
		{"no estimate, below threshold", 0, 0, 20, 100, 0.5, true},
		{"no estimate, above threshold", 0, 0, 80, 100, 0.5, false},
		{"unknown capacity", 50, 0, 0, 0, 0.5, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsRefuelForRoute(tc.estimatedFuel, tc.fuelAvailable, tc.fuel, tc.maxFuel, tc.threshold); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEnsureRouteFuel(t *testing.T) {
	t.Run("full tank: no dock/refuel", func(t *testing.T) {
		f := &fakeClient{state: &game.State{Fuel: 100, MaxFuel: 100}}
		ensureRouteFuel(context.Background(), f, io.Discard, 0, 0, fuelTiming{})
		if slices.Contains(f.calls, "refuel") || slices.Contains(f.calls, "dock") {
			t.Errorf("unexpected dock/refuel with full tank: %v", f.calls)
		}
	})
	t.Run("low fuel, undocked: docks then refuels", func(t *testing.T) {
		f := &fakeClient{state: &game.State{Fuel: 5, MaxFuel: 100}}
		ensureRouteFuel(context.Background(), f, io.Discard, 0, 0, fuelTiming{})
		d, r := slices.Index(f.calls, "dock"), slices.Index(f.calls, "refuel")
		if d < 0 || r < 0 || d > r {
			t.Errorf("want dock before refuel, got %v", f.calls)
		}
	})
	t.Run("low fuel, docked: refuels without docking", func(t *testing.T) {
		f := &fakeClient{state: &game.State{Fuel: 5, MaxFuel: 100, Doc: true}}
		ensureRouteFuel(context.Background(), f, io.Discard, 0, 0, fuelTiming{})
		if slices.Contains(f.calls, "dock") {
			t.Errorf("should not dock when already docked: %v", f.calls)
		}
		if !slices.Contains(f.calls, "refuel") {
			t.Errorf("want refuel, got %v", f.calls)
		}
	})
	t.Run("route shortfall refuels even with full tank reading", func(t *testing.T) {
		f := &fakeClient{state: &game.State{Fuel: 100, MaxFuel: 100, Doc: true}}
		ensureRouteFuel(context.Background(), f, io.Discard, 50, 10, fuelTiming{})
		if !slices.Contains(f.calls, "refuel") {
			t.Errorf("want refuel on route shortfall, got %v", f.calls)
		}
	})
	t.Run("not at a station: dock fails, no refuel", func(t *testing.T) {
		f := &fakeClient{state: &game.State{Fuel: 5, MaxFuel: 100}, dockErr: errors.New("must be docked at a base")}
		ensureRouteFuel(context.Background(), f, io.Discard, 0, 0, fuelTiming{})
		if !slices.Contains(f.calls, "dock") {
			t.Errorf("want dock attempt, got %v", f.calls)
		}
		if slices.Contains(f.calls, "refuel") {
			t.Errorf("must not refuel when not at a station, got %v", f.calls)
		}
	})
}

func TestAutopilotTravelsToPOIAfterJumps(t *testing.T) {
	f := autopilotFake()
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard}, "sys_c", "trade_hub")
	if err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	if !slices.Contains(f.calls, "travel:trade_hub") {
		t.Errorf("expected final travel:trade_hub, got %v", f.calls)
	}
}

func TestAutopilotCaptureErrorIsNonFatal(t *testing.T) {
	f := autopilotFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client:     f,
		Out:        io.Discard,
		OnWaypoint: func(ctx context.Context) error { return errors.New("kb down") },
	}, "sys_c", "")
	if err != nil {
		t.Fatalf("capture errors must be non-fatal, got %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_c") {
		t.Errorf("route should still complete despite capture errors, got %v", f.calls)
	}
}

func TestAutopilotUsesFuelCellsWhenLow(t *testing.T) {
	f := &fakeClient{
		// Fuel at 5% — below the 10% threshold in autopilotRefuelIfNeeded.
		state: &game.State{
			Fuel:    5,
			MaxFuel: 100,
			Ship: game.Ship{
				Cargo: []game.CargoItem{
					{ItemID: "fuel_cell", Quantity: 3},
				},
			},
		},
		route: []game.RouteStep{
			{SystemID: "sys_a", Name: "Alpha"}, // current system, skipped
			{SystemID: "sys_b", Name: "Bravo"},
		},
	}
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard}, "sys_b", "")
	if err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	if !slices.Contains(f.calls, "raw:use_item") {
		t.Errorf("expected raw:use_item call when fuel is low, got %v", f.calls)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int]string{5: "5s", 60: "1m", 90: "1m 30s", 125: "2m 5s"}
	for secs, want := range cases {
		if got := FormatDuration(secs); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", secs, got, want)
		}
	}
}

// TestAutopilotRefusesToDepartWhenShortOnFuel is the regression for the
// 2026-08-15 strandings: the planner printed "Not enough fuel! Need 56 more"
// and jumped anyway, killing craftsman-1 at westmark_star with 4/400. A route
// we have already computed we cannot finish must not be started — the agent
// stays docked and recoverable instead of fuel-dead in deep space.
func TestAutopilotRefusesToDepartWhenShortOnFuel(t *testing.T) {
	f := autopilotFake()
	// find_route reports the shape craftsman-1 saw: 15 per jump, 225 needed,
	// 169 aboard. Fuel is full in state, so ensureRouteFuel has nothing to add.
	f.raw = map[string][]byte{
		"_last": []byte(`{"fuel_per_jump":15,"estimated_fuel":225,"fuel_available":169}`),
	}

	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard}, "sys_c", "")

	if !errors.Is(err, ErrInsufficientRouteFuel) {
		t.Fatalf("want ErrInsufficientRouteFuel, got %v", err)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "jump:") {
			t.Fatalf("must not jump when short on fuel, calls=%v", f.calls)
		}
	}
}

// TestAutopilotDepartsWhenFuelSuffices guards the other side: an adequate tank
// must still fly the whole route, so the gate cannot deadlock normal hauling.
func TestAutopilotDepartsWhenFuelSuffices(t *testing.T) {
	f := autopilotFake()
	f.raw = map[string][]byte{
		"_last": []byte(`{"fuel_per_jump":15,"estimated_fuel":30,"fuel_available":169}`),
	}

	if err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard}, "sys_c", ""); err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_b") || !slices.Contains(f.calls, "jump:sys_c") {
		t.Fatalf("sufficient fuel must fly the route, calls=%v", f.calls)
	}
}
