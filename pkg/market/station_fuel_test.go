package market

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStationFuelRoundTrip(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close() //nolint:errcheck
	ctx := context.Background()

	// Unknown station -> ok=false, no error.
	if _, _, ok, err := c.GetStationFuelPrice(ctx, "sol_central"); err != nil || ok {
		t.Fatalf("unknown station: want ok=false err=nil, got ok=%v err=%v", ok, err)
	}

	// Insert.
	if err := c.UpsertStationFuel(ctx, StationFuel{
		StationID: "sol_central", FuelPrice: 2, FuelTaxPerUnit: 5, FuelPriceAllIn: 7,
		CapturedAt: "2026-07-15T00:00:00Z", CapturedBy: "marketbot_sol",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	allIn, at, ok, err := c.GetStationFuelPrice(ctx, "sol_central")
	if err != nil || !ok || allIn != 7 {
		t.Fatalf("after insert: allIn=%d ok=%v err=%v", allIn, ok, err)
	}
	if at.IsZero() {
		t.Fatalf("expected a parsed captured_at, got zero time")
	}

	// Re-upsert the same station -> still ONE row, values replaced.
	if err := c.UpsertStationFuel(ctx, StationFuel{
		StationID: "sol_central", FuelPrice: 3, FuelTaxPerUnit: 9, FuelPriceAllIn: 12,
		CapturedAt: "2026-07-15T01:00:00Z", CapturedBy: "marketbot_sol",
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	var n int
	if err := c.db.QueryRow(`SELECT count(*) FROM station_fuel_prices`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row after re-upsert, got %d", n)
	}
	if allIn, _, _, _ := c.GetStationFuelPrice(ctx, "sol_central"); allIn != 12 {
		t.Fatalf("expected updated allIn=12, got %d", allIn)
	}
}
