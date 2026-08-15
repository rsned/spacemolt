package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/navigation"
)

// The get_base shape observed live 2026-08-15 (grand_exchange_station):
// base.fuel/max_fuel are the public desk, faction_fuel_* the faction bunker.
const getBaseWithReserves = `{
  "base": {"id": "grand_exchange_station", "poi_id": "grand_exchange",
           "faction_id": "crft-id", "fuel": 499724, "max_fuel": 500000},
  "fuel_price": 2, "fuel_tax_per_unit": 5, "fuel_price_all_in": 7,
  "faction_fuel_capacity": 50000, "faction_fuel_reserve": 26872
}`

// A dry desk still quotes a price — fuel 0 must survive as a MEASURED zero.
const getBaseDryDesk = `{
  "base": {"id": "frontier_station", "fuel": 0, "max_fuel": 200000},
  "fuel_price": 20, "fuel_tax_per_unit": 1, "fuel_price_all_in": 21
}`

// Pre-reserve payload: no fuel field on base at all.
const getBaseLegacy = `{
  "base": {"id": "old_station"},
  "fuel_price": 3, "fuel_tax_per_unit": 1, "fuel_price_all_in": 4
}`

func TestParseGetBaseFuelReserves(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	sf, ok, err := parseGetBaseFuel([]byte(getBaseWithReserves), "grand_exchange", "mb", now)
	if err != nil || !ok {
		t.Fatalf("parse: ok=%v err=%v", ok, err)
	}
	if sf.FuelReserve != 499724 || sf.FuelCapacity != 500000 {
		t.Errorf("desk reserve = %d/%d, want 499724/500000", sf.FuelReserve, sf.FuelCapacity)
	}
	if sf.FactionFuelReserve != 26872 || sf.FactionID != "crft-id" {
		t.Errorf("bunker = %d faction=%q, want 26872/crft-id", sf.FactionFuelReserve, sf.FactionID)
	}
	if sf.ReserveObservedAt == "" {
		t.Errorf("measured capture must stamp ReserveObservedAt")
	}

	dry, ok, err := parseGetBaseFuel([]byte(getBaseDryDesk), "frontier_station", "mb", now)
	if err != nil || !ok {
		t.Fatalf("dry parse: ok=%v err=%v", ok, err)
	}
	if dry.FuelReserve != 0 || dry.ReserveObservedAt == "" {
		t.Errorf("dry desk must be a measured zero: %+v", dry)
	}

	old, ok, err := parseGetBaseFuel([]byte(getBaseLegacy), "old_station", "mb", now)
	if err != nil || !ok {
		t.Fatalf("legacy parse: ok=%v err=%v", ok, err)
	}
	if old.FuelReserve != -1 || old.ReserveObservedAt != "" {
		t.Errorf("absent fuel field must stay unknown (-1, no stamp): %+v", old)
	}
}

func TestUpsertPreservesReserveAgainstPriceOnlyWrites(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close() //nolint:errcheck
	ctx := context.Background()

	measured := StationFuel{StationID: "a", FuelPriceAllIn: 7,
		CapturedAt: "2026-08-15T10:00:00Z", FuelReserve: 1234, FuelCapacity: 5000,
		FactionFuelReserve: -1, FactionFuelCapacity: -1,
		ReserveObservedAt: "2026-08-15T10:00:00Z"}
	if err := c.UpsertStationFuel(ctx, measured); err != nil {
		t.Fatalf("measured upsert: %v", err)
	}
	// A zero-valued price-only write (no ReserveObservedAt) must not clobber
	// the reading — neither to 0 ("measured dry") nor to -1.
	if err := c.UpsertStationFuel(ctx, StationFuel{StationID: "a", FuelPriceAllIn: 9,
		CapturedAt: "2026-08-15T11:00:00Z"}); err != nil {
		t.Fatalf("price-only upsert: %v", err)
	}
	var reserve int
	var obs string
	if err := c.db.QueryRow(`SELECT fuel_reserve, reserve_observed_at FROM station_fuel_prices WHERE station_id='a'`).
		Scan(&reserve, &obs); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if reserve != 1234 || obs != "2026-08-15T10:00:00Z" {
		t.Errorf("price-only write clobbered reserve: %d @ %s", reserve, obs)
	}

	// MarkDeskDry overrides with a fresh measured zero, and inserts rows for
	// never-captured stations.
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if err := c.MarkDeskDry(ctx, "a", "assist-1", now); err != nil {
		t.Fatalf("mark dry: %v", err)
	}
	if err := c.db.QueryRow(`SELECT fuel_reserve FROM station_fuel_prices WHERE station_id='a'`).Scan(&reserve); err != nil {
		t.Fatalf("read after dry: %v", err)
	}
	if reserve != 0 {
		t.Errorf("after MarkDeskDry reserve = %d, want 0", reserve)
	}
	if err := c.MarkDeskDry(ctx, "never-seen", "assist-1", now); err != nil {
		t.Fatalf("mark dry new row: %v", err)
	}
}

func TestNearestFuelRanking(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close() //nolint:errcheck
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	stale := now.Add(-24 * time.Hour).Format(time.RFC3339)

	// Station -> system topology: home - s1 - s2 - s3, plus far (unreachable)
	// and hold (stronghold).
	for _, s := range []struct{ station, system string }{
		{"wet_near", "s1"}, {"dry_near", "s1"}, {"wet_far", "s3"},
		{"unknown_mid", "s2"}, {"stale_mid", "s2"}, {"cutoff", "far"},
		{"pirate_fuel", "hold"}, {"bunker_near", "s1"},
	} {
		if _, err := c.db.Exec(`INSERT INTO stations (station_id, station_name, system_id, system_name, first_seen_utc, last_updated_utc)
			VALUES (?, ?, ?, ?, '', '')`, s.station, s.station, s.system, s.system); err != nil {
			t.Fatalf("seed station %s: %v", s.station, err)
		}
	}
	rows := []StationFuel{
		{StationID: "wet_near", FuelPriceAllIn: 10, CapturedAt: fresh, FuelReserve: 5000, FuelCapacity: 9000, ReserveObservedAt: fresh},
		{StationID: "dry_near", FuelPriceAllIn: 2, CapturedAt: fresh, FuelReserve: 0, FuelCapacity: 9000, ReserveObservedAt: fresh},
		{StationID: "wet_far", FuelPriceAllIn: 5, CapturedAt: fresh, FuelReserve: 5000, FuelCapacity: 9000, ReserveObservedAt: fresh},
		{StationID: "unknown_mid", FuelPriceAllIn: 8, CapturedAt: fresh, FuelReserve: -1, FuelCapacity: -1},
		{StationID: "stale_mid", FuelPriceAllIn: 3, CapturedAt: stale, FuelReserve: 5000, FuelCapacity: 9000, ReserveObservedAt: stale},
		{StationID: "cutoff", FuelPriceAllIn: 1, CapturedAt: fresh, FuelReserve: 5000, FuelCapacity: 9000, ReserveObservedAt: fresh},
		{StationID: "pirate_fuel", FuelPriceAllIn: 1, CapturedAt: fresh, FuelReserve: 5000, FuelCapacity: 9000, ReserveObservedAt: fresh},
		// Desk unknown, but an allied faction bunker holds plenty.
		{StationID: "bunker_near", FuelPriceAllIn: 4, CapturedAt: fresh, FuelReserve: -1, FuelCapacity: -1,
			FactionFuelReserve: 3000, FactionFuelCapacity: 5000, FactionID: "crft", ReserveObservedAt: fresh},
	}
	for _, r := range rows {
		if err := c.UpsertStationFuel(ctx, r); err != nil {
			t.Fatalf("seed fuel %s: %v", r.StationID, err)
		}
	}

	graph := navigation.JumpGraph{
		"home": {"s1", "hold"}, "s1": {"home", "s2"}, "s2": {"s1", "s3"},
		"s3": {"s2"}, "hold": {"home"},
	}
	stops, err := c.NearestFuel(ctx, "home", 1000, graph,
		map[string]bool{"crft": true}, map[string]bool{"hold": true}, 6*time.Hour, now)
	if err != nil {
		t.Fatalf("NearestFuel: %v", err)
	}

	got := make([]string, len(stops))
	for i, s := range stops {
		got[i] = s.StationID
	}
	// Known-wet first by (jumps, price): bunker_near(1j,4) beats wet_near(1j,10),
	// then wet_far(3j). Unknowns after: unknown_mid then stale_mid (2j; stale
	// reading downgrades to unknown, price 3 < 8 → stale_mid first).
	want := []string{"bunker_near", "wet_near", "wet_far", "stale_mid", "unknown_mid"}
	if len(got) != len(want) {
		t.Fatalf("stops = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stops = %v, want %v", got, want)
		}
	}
	if !stops[0].KnownWet || stops[3].KnownWet {
		t.Errorf("KnownWet flags wrong: %+v", stops)
	}
	// dry_near (fresh measured 0) and pirate_fuel (excluded system) and
	// cutoff (unreachable) must all be absent — checked implicitly by `want`.
}
