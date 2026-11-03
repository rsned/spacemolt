package knowledge

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

const crftFaction = "e727c0e918d994c72db2978fe5b18edc"

func TestMigration66SeedsCongregationLicense(t *testing.T) {
	kb := newTestKB(t)
	var n int
	if err := kb.db.QueryRow(`SELECT COUNT(*) FROM ship_licenses`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
	var faction, class, empire, agent, seen string
	var cost int
	var tick int64
	if err := kb.db.QueryRow(`SELECT faction_id, ship_class, empire, agent_id, cost_paid, tick, seen_at
		FROM ship_licenses`).Scan(&faction, &class, &empire, &agent, &cost, &tick, &seen); err != nil {
		t.Fatal(err)
	}
	if faction != crftFaction || class != "congregation" || empire != "outerrim" ||
		agent != "craftsman-1" || cost != 1000000 || tick != 2301382 || seen != "2026-11-02T19:54:20Z" {
		t.Errorf("row = %q %q %q %q %d %d %q", faction, class, empire, agent, cost, tick, seen)
	}
}

func TestRecordShipyardQuoteNullRoyalty(t *testing.T) {
	kb := newTestKB(t)
	err := kb.RecordShipyardQuote(context.Background(), ShipyardQuote{
		ShipClass: "congregation", StationID: "st1", Mode: "quote", Royalty: nil,
		SeenAt: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	var r sql.NullInt64
	if err := kb.db.QueryRow(`SELECT royalty FROM shipyard_quotes`).Scan(&r); err != nil {
		t.Fatal(err)
	}
	if r.Valid {
		t.Errorf("royalty = %d, want NULL", r.Int64)
	}
}

func TestRecordShipyardQuoteRoundTrip(t *testing.T) {
	kb := newTestKB(t)
	royalty := 734
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	in := ShipyardQuote{
		ShipClass: "congregation", StationID: "st1", FactionID: crftFaction, AgentID: "craftsman-1",
		Mode: "quote", Labor: 1000, MaterialCost: 5000, ProvideMaterialsTotal: 4000,
		YardMargin: 300, ShipyardTierHere: 2, Royalty: &royalty, Tick: 2301400, SeenAt: at,
		Raw: []byte(`{"x":1}`),
	}
	if err := kb.RecordShipyardQuote(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	var o ShipyardQuote
	var roy sql.NullInt64
	var seen, raw string
	if err := kb.db.QueryRow(`SELECT ship_class, station_id, faction_id, agent_id, mode, labor,
		material_cost, provide_materials_total, yard_margin, shipyard_tier_here, royalty, tick, seen_at, raw
		FROM shipyard_quotes`).Scan(&o.ShipClass, &o.StationID, &o.FactionID, &o.AgentID, &o.Mode, &o.Labor,
		&o.MaterialCost, &o.ProvideMaterialsTotal, &o.YardMargin, &o.ShipyardTierHere, &roy, &o.Tick, &seen, &raw); err != nil {
		t.Fatal(err)
	}
	if !roy.Valid || roy.Int64 != 734 || seen != "2026-11-02T12:00:00Z" || raw != `{"x":1}` {
		t.Errorf("royalty=%v seen=%q raw=%q", roy, seen, raw)
	}
	o.Royalty, o.SeenAt, o.Raw = in.Royalty, in.SeenAt, in.Raw
	if o.ShipClass != in.ShipClass || o.StationID != in.StationID || o.FactionID != in.FactionID ||
		o.AgentID != in.AgentID || o.Mode != in.Mode || o.Labor != in.Labor ||
		o.MaterialCost != in.MaterialCost || o.ProvideMaterialsTotal != in.ProvideMaterialsTotal ||
		o.YardMargin != in.YardMargin || o.ShipyardTierHere != in.ShipyardTierHere || o.Tick != in.Tick {
		t.Errorf("round trip mismatch: %+v", o)
	}
}

func TestRecordShipLicenseReplaces(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	l := ShipLicense{FactionID: "f1", ShipClass: "frankenhauler", Empire: "outerrim", AgentID: "a",
		CostPaid: 20000000, Tick: 10, SeenAt: time.Now()}
	if err := kb.RecordShipLicense(ctx, l); err != nil {
		t.Fatal(err)
	}
	l.Tick = 20
	if err := kb.RecordShipLicense(ctx, l); err != nil {
		t.Fatal(err)
	}
	var n int
	var tick int64
	if err := kb.db.QueryRow(`SELECT COUNT(*), MAX(tick) FROM ship_licenses WHERE faction_id='f1'`).Scan(&n, &tick); err != nil {
		t.Fatal(err)
	}
	if n != 1 || tick != 20 {
		t.Errorf("n=%d tick=%d", n, tick)
	}
}
