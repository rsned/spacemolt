package worker

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// reserve16 is the deferral headroom for a hull burning 8 fuel/jump, which is what a
// live mission hauler measured on 2026-07-27 (AutopilotDeferReserveJumps * 8).
const reserve16 = AutopilotDeferReserveJumps * 8

func TestNeedsRefuelForRouteAt(t *testing.T) {
	// dear = origin dearer than destination (defer candidate); cheap = the reverse.
	dear := fuelTiming{OriginPrice: 26, DestPrice: 2, Comparable: true, Reserve: reserve16}
	cheap := fuelTiming{OriginPrice: 2, DestPrice: 26, Comparable: true, Reserve: reserve16}

	tests := []struct {
		name                     string
		estimatedFuel, available int
		fuel, maxFuel            float64
		ft                       fuelTiming
		want                     bool
	}{
		// Legacy behavior must be bit-identical without an opt-in.
		{"incomparable, below threshold: refuel", 0, 0, 40, 100, fuelTiming{}, true},
		{"incomparable, above threshold: no refuel", 0, 0, 60, 100, fuelTiming{}, false},
		{"unknown capacity: never refuel", 50, 0, 0, 0, dear, false},

		// A route that does not fit is forced regardless of how cheap the far end is.
		{"route shortfall beats a cheap destination", 50, 10, 100, 100, dear, true},

		// Cheap here: buy now even well above the threshold, because the refuel fills
		// to full either way and a cheap tank pre-pays the next leg.
		{"cheap here: fills above threshold", 10, 90, 90, 100, cheap, true},
		{"cheap here but tank already full: nothing to buy", 10, 100, 100, 100, cheap, false},
		{"equal prices: buy here rather than gamble on drift", 10, 90, 90, 100,
			fuelTiming{OriginPrice: 7, DestPrice: 7, Comparable: true, Reserve: reserve16}, true},
		{"free pump here: fills above threshold", 10, 90, 90, 100,
			fuelTiming{OriginPrice: 0, DestPrice: 4, Comparable: true, Reserve: reserve16}, true},

		// Dear here: defer only with headroom beyond the jump estimate.
		{"dear here with headroom: defer", 20, 60, 60, 100, dear, false},
		{"dear here, headroom short, below threshold: refuel", 30, 40, 40, 100, dear, true},
		{"dear here, headroom exactly met: defer", 20, 36, 40, 100, dear, false},
		{"dear here, one unit short of headroom: falls back to threshold", 20, 35, 40, 100, dear, true},

		// Deferral needs both a reserve and an estimate; without either it must not
		// gamble, and falls back to the threshold.
		{"zero reserve disables deferral", 20, 60, 40, 100,
			fuelTiming{OriginPrice: 26, DestPrice: 2, Comparable: true}, true},
		{"no route estimate cannot defer", 0, 60, 40, 100, dear, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsRefuelForRouteAt(tt.estimatedFuel, tt.available, tt.fuel, tt.maxFuel,
				AutopilotRefuelThreshold, tt.ft)
			if got != tt.want {
				t.Errorf("needsRefuelForRouteAt = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNeedsRefuelForRouteMatchesLegacy pins that the price-blind wrapper delegates with
// an incomparable timing, so no un-opted-in role can change behavior.
func TestNeedsRefuelForRouteMatchesLegacy(t *testing.T) {
	for _, fuel := range []float64{0, 10, 49, 50, 51, 100} {
		for _, est := range []int{0, 10, 200} {
			want := needsRefuelForRouteAt(est, 20, fuel, 100, AutopilotRefuelThreshold, fuelTiming{})
			if got := needsRefuelForRoute(est, 20, fuel, 100, AutopilotRefuelThreshold); got != want {
				t.Errorf("fuel=%v est=%d: wrapper %v != delegate %v", fuel, est, got, want)
			}
		}
	}
}

func TestBuildFuelPriceAt(t *testing.T) {
	ctx := context.Background()
	src := &fakeFuelPrices{prices: map[string]int{"sol_central": 26}, median: 6, hasMed: true}
	at := buildFuelPriceAt(ctx, src)

	if got, ok := at("sol_central"); got != 26 || !ok {
		t.Errorf("captured: want (26,true), got (%v,%v)", got, ok)
	}
	// A free faction pump is a KNOWN zero: it is the cheapest place to buy, not a
	// missing reading.
	if got, ok := at("grand_exchange_station"); got != 0 || !ok {
		t.Errorf("free pump: want (0,true), got (%v,%v)", got, ok)
	}
	// The critical difference from buildPriceOf: an uncaptured station must report
	// UNKNOWN, never the galaxy median. ~half the galaxy's stations are uncaptured, and
	// a median is not a safe basis for running a tank down.
	if got, ok := at("uncaptured_station"); ok {
		t.Errorf("uncaptured: want ok=false (not the median), got (%v,%v)", got, ok)
	}
	if _, ok := at(""); ok {
		t.Error("empty station id: want ok=false")
	}
	if buildFuelPriceAt(ctx, nil) != nil {
		t.Error("nil source: want a nil resolver so timing stays price-blind")
	}
}

func TestFuelTimingFor(t *testing.T) {
	at := buildFuelPriceAt(context.Background(),
		&fakeFuelPrices{prices: map[string]int{"sol_central": 26, "ramens_rest": 3}, median: 6, hasMed: true})
	clientAt := func(poi string) *fakeClient {
		return &fakeClient{state: &game.State{Fuel: 50, MaxFuel: 100, CurrentPOI: poi}}
	}

	t.Run("both endpoints known: comparable with a hull-scaled reserve", func(t *testing.T) {
		d := AutopilotDeps{Client: clientAt("sol_central"), FuelPriceAt: at}
		ft := d.fuelTimingFor("ramens_rest", 8)
		if !ft.Comparable || ft.OriginPrice != 26 || ft.DestPrice != 3 {
			t.Fatalf("want comparable 26 -> 3, got %+v", ft)
		}
		if ft.Reserve != reserve16 {
			t.Errorf("reserve: want %d, got %d", reserve16, ft.Reserve)
		}
	})

	// Every one of these must degrade to the legacy price-blind path.
	for _, tt := range []struct {
		name string
		deps AutopilotDeps
		dest string
		perJ int
	}{
		{"no opt-in", AutopilotDeps{Client: clientAt("sol_central")}, "ramens_rest", 8},
		{"no destination station", AutopilotDeps{Client: clientAt("sol_central"), FuelPriceAt: at}, "", 8},
		{"no fuel-per-jump probe", AutopilotDeps{Client: clientAt("sol_central"), FuelPriceAt: at}, "ramens_rest", 0},
		{"origin is the destination", AutopilotDeps{Client: clientAt("ramens_rest"), FuelPriceAt: at}, "ramens_rest", 8},
		{"destination price unknown", AutopilotDeps{Client: clientAt("sol_central"), FuelPriceAt: at}, "uncaptured", 8},
		{"origin price unknown", AutopilotDeps{Client: clientAt("uncaptured"), FuelPriceAt: at}, "ramens_rest", 8},
		{"no state", AutopilotDeps{Client: &fakeClient{}, FuelPriceAt: at}, "ramens_rest", 8},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if ft := tt.deps.fuelTimingFor(tt.dest, tt.perJ); ft.Comparable {
				t.Errorf("want incomparable (legacy behavior), got %+v", ft)
			}
		})
	}
}

func TestEnsureRouteFuelPriceAware(t *testing.T) {
	dear := fuelTiming{OriginPrice: 26, DestPrice: 2, Comparable: true, Reserve: reserve16}
	cheap := fuelTiming{OriginPrice: 2, DestPrice: 26, Comparable: true, Reserve: reserve16}

	t.Run("dear origin with headroom: skips the refuel and says why", func(t *testing.T) {
		f := &fakeClient{state: &game.State{Fuel: 60, MaxFuel: 100, Doc: true}}
		var log bytes.Buffer
		ensureRouteFuel(context.Background(), f, &log, 20, 60, dear)
		if slices.Contains(f.calls, "refuel") {
			t.Errorf("want no refuel at the dear end, got %v", f.calls)
		}
		if !strings.Contains(log.String(), "deferring refuel") {
			t.Errorf("deferral must be visible in the log, got %q", log.String())
		}
	})

	t.Run("cheap origin: buys above the threshold that would have skipped it", func(t *testing.T) {
		f := &fakeClient{state: &game.State{Fuel: 90, MaxFuel: 100, Doc: true}}
		// 90/100 is well above AutopilotRefuelThreshold, so the legacy path would
		// have bought nothing here.
		if needsRefuelForRoute(10, 90, 90, 100, AutopilotRefuelThreshold) {
			t.Fatal("precondition: legacy path should not refuel at 90/100")
		}
		ensureRouteFuel(context.Background(), f, io.Discard, 10, 90, cheap)
		if !slices.Contains(f.calls, "refuel") {
			t.Errorf("want an opportunistic fill at the cheap end, got %v", f.calls)
		}
	})

	t.Run("dear origin but route does not fit: still refuels, no deferral message", func(t *testing.T) {
		f := &fakeClient{state: &game.State{Fuel: 20, MaxFuel: 100, Doc: true}}
		var log bytes.Buffer
		ensureRouteFuel(context.Background(), f, &log, 80, 20, dear)
		if !slices.Contains(f.calls, "refuel") {
			t.Errorf("want a forced refuel on route shortfall, got %v", f.calls)
		}
		if strings.Contains(log.String(), "deferring") {
			t.Errorf("must not claim a deferral when it refueled: %q", log.String())
		}
	})
}
