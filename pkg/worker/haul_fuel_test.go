package worker

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// fakeFuelPrices is a stub FuelPriceSource.
type fakeFuelPrices struct {
	prices map[string]int // stationID -> all-in; absent => ok=false
	median int
	hasMed bool
}

func (f *fakeFuelPrices) GetStationFuelPrice(_ context.Context, id string) (int, time.Time, bool, error) {
	if v, ok := f.prices[id]; ok {
		return v, time.Time{}, true, nil
	}
	return 0, time.Time{}, false, nil
}
func (f *fakeFuelPrices) MedianStationFuelAllIn(_ context.Context) (int, bool, error) {
	return f.median, f.hasMed, nil
}

func TestBuildPriceOf(t *testing.T) {
	ctx := context.Background()
	src := &fakeFuelPrices{prices: map[string]int{"sol_central": 8}, median: 6, hasMed: true}
	priceOf := buildPriceOf(ctx, src)

	if got := priceOf("grand_exchange_station"); got != 0 {
		t.Errorf("free pump: want 0, got %v", got)
	}
	if got := priceOf("sol_central"); got != 8 {
		t.Errorf("captured: want 8, got %v", got)
	}
	if got := priceOf("uncaptured_station"); got != 6 {
		t.Errorf("uncaptured -> median: want 6, got %v", got)
	}

	// No median available -> uncaptured resolves to 0.
	noMed := buildPriceOf(ctx, &fakeFuelPrices{prices: map[string]int{}, hasMed: false})
	if got := noMed("anything"); got != 0 {
		t.Errorf("no median: want 0, got %v", got)
	}
	// Nil source -> always 0 (gross-only fallback).
	if got := buildPriceOf(ctx, nil)("sol_central"); got != 0 {
		t.Errorf("nil source: want 0, got %v", got)
	}
}

func TestHaulFuelLegCost(t *testing.T) {
	hf := haulFuel{perJump: 3, priceOf: func(string) float64 { return 5 }}
	if got := hf.legCost(4, "s"); got != 60 { // 4*3*5
		t.Errorf("legCost: want 60, got %v", got)
	}
	if got := hf.legCost(0, "s"); got != 0 {
		t.Errorf("zero jumps: want 0, got %v", got)
	}
	// Zero rate -> zero cost (gross-only fallback), price never consulted.
	zero := haulFuel{perJump: 0, priceOf: func(string) float64 { return 99 }}
	if got := zero.legCost(4, "s"); got != 0 {
		t.Errorf("zero rate: want 0, got %v", got)
	}
}

func TestHaulJumpsBetween(t *testing.T) {
	g, _ := graphFor([]string{"a", "b", "c"}, [2]string{"a", "b"}, [2]string{"b", "c"})
	hf := haulFuel{graph: g}
	if j, ok := hf.haulJumpsBetween("a", "c"); !ok || j != 2 {
		t.Errorf("a->c: want 2/true, got %d/%v", j, ok)
	}
	if _, ok := hf.haulJumpsBetween("a", ""); ok {
		t.Error("empty target: want ok=false")
	}
	if _, ok := hf.haulJumpsBetween("a", "unknown"); ok {
		t.Error("unreachable: want ok=false")
	}
}

func TestHaulFuelPerJumpServerThenFallback(t *testing.T) {
	ctx := context.Background()

	// Server value: find_route probe populates "_last" with fuel_per_jump.
	srv := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"_last": []byte(`{"fuel_per_jump":4}`)},
	}
	if got := haulFuelPerJump(ctx, srv, "b"); got != 4 {
		t.Errorf("server path: want 4, got %d", got)
	}

	// Fallback: no "_last", ship-class formula ceil(scale^1.5 * speed * 10 * 0.10).
	// scale=4, speed=2 -> ceil(8 * 2 * 1.0) = 16.
	fb := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"ship": []byte(`{"class":{"scale":4,"base_speed":2}}`)},
	}
	if got := haulFuelPerJump(ctx, fb, "b"); got != 16 {
		t.Errorf("fallback path: want 16, got %d", got)
	}

	// Neither -> 0.
	none := &fakeClient{state: &game.State{}, raw: map[string][]byte{}}
	if got := haulFuelPerJump(ctx, none, "b"); got != 0 {
		t.Errorf("no data: want 0, got %d", got)
	}
}
