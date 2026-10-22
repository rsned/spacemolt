package ovdash

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// fixtureOppMarket builds a market DB holding one claimed opportunity whose
// endpoints are deliberately dual-named: the BUY side is a BASE id and the SELL
// side is a POI id. Station ids in this game come from two namespaces and are
// never comparable across commands, so a resolver that consults only `pois`
// silently renders the buy station as a raw hex id with no system and no jump
// count -- which is precisely the detail the drawer exists to show.
func fixtureOppMarket(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "market.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	stmts := []string{
		`CREATE TABLE arbitrage_opportunities (
			id INTEGER PRIMARY KEY, from_station_id TEXT, to_station_id TEXT,
			item_id TEXT, action_type TEXT, buy_price REAL, sell_price REAL,
			quantity REAL, source_units REAL, gross_profit REAL,
			claimed_by TEXT, claimed_at TEXT, status TEXT, expires_at TEXT,
			discovered_at TEXT, discovered_by TEXT, notes TEXT)`,
		`INSERT INTO arbitrage_opportunities VALUES
			(1514259,'base_grand_exchange','node_beta_industrial_station',
			 'phase_disruptor','buy_then_sell',21000,38874,1,1,17874,
			 'salvager-9','2026-10-11T00:43:28Z','claimed','2026-10-11T06:43:00Z',
			 '2026-10-11T00:43:00Z','arbitrage_scanner','logistics:deferred')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// fixtureOppKB builds a knowledge DB with a 3-hop chain haven->mid->node_beta,
// plus a decoy edge so a correct BFS is distinguishable from "count the rows".
func fixtureOppKB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kb.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	stmts := []string{
		`CREATE TABLE systems (id TEXT PRIMARY KEY, name TEXT,
			position_x REAL, position_y REAL)`,
		`CREATE TABLE pois (id TEXT PRIMARY KEY, name TEXT, system_id TEXT)`,
		`CREATE TABLE bases (id TEXT PRIMARY KEY, poi_id TEXT, name TEXT)`,
		`CREATE TABLE connections (from_system TEXT, to_system TEXT,
			distance INTEGER DEFAULT 0, last_updated_tick INTEGER DEFAULT 0)`,
		// Collinear at unit spacing so each lane's stored distance equals the
		// spatial separation exactly -- that equality is what marks a lane
		// ORDINARY rather than a one-way wormhole, so these stay bidirectional.
		`INSERT INTO systems VALUES ('haven','Haven',0,0),('mid','Midpoint',1,0),
			('node_beta','Node Beta',2,0),('elsewhere','Elsewhere',5,0)`,
		`INSERT INTO pois VALUES
			('grand_exchange','Grand Exchange','haven'),
			('node_beta_industrial_station','Node Beta Industrial Station','node_beta')`,
		// The BASE row points at the POI; only via poi_id can the base id reach a system.
		`INSERT INTO bases VALUES ('base_grand_exchange','grand_exchange','Grand Exchange')`,
		`INSERT INTO connections VALUES ('haven','mid',1,0),('mid','node_beta',1,0),
			('haven','elsewhere',5,0)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// The drawer's whole purpose is the four numbers the table does NOT carry:
// unit margin, margin %, jumps, and profit-per-jump. travel_ticks and fuel_cost
// exist as columns but are ALWAYS 0 in production, so jumps must come from the
// connection graph or the drawer repeats the lie that every run is free.
func TestLoadOpportunity_EnrichesWithJumpsAndMargin(t *testing.T) {
	got, err := LoadOpportunity(context.Background(),
		fixtureOppMarket(t), fixtureOppKB(t), 1514259)
	if err != nil {
		t.Fatal(err)
	}
	if got.ItemID != "phase_disruptor" || got.Status != "claimed" {
		t.Errorf("item/status = %q/%q", got.ItemID, got.Status)
	}
	// Dual-named endpoints must BOTH resolve to a display name and a system.
	if got.FromName != "Grand Exchange" || got.FromSystem != "Haven" {
		t.Errorf("buy endpoint = %q in %q, want Grand Exchange in Haven "+
			"(a base id must resolve through bases.poi_id)", got.FromName, got.FromSystem)
	}
	if got.ToName != "Node Beta Industrial Station" || got.ToSystem != "Node Beta" {
		t.Errorf("sell endpoint = %q in %q", got.ToName, got.ToSystem)
	}
	// haven -> mid -> node_beta
	if got.Jumps != 2 {
		t.Errorf("Jumps = %d, want 2 (BFS over connections, not travel_ticks)", got.Jumps)
	}
	if got.UnitMargin != 17874 {
		t.Errorf("UnitMargin = %v, want 17874", got.UnitMargin)
	}
	if int(got.MarginPct) != 85 {
		t.Errorf("MarginPct = %v, want ~85", got.MarginPct)
	}
	if int(got.ProfitPerJump) != 8937 {
		t.Errorf("ProfitPerJump = %v, want 8937 (17874/2)", got.ProfitPerJump)
	}
	if got.ClaimedBy != "salvager-9" || got.Notes != "logistics:deferred" {
		t.Errorf("claim/notes = %q/%q", got.ClaimedBy, got.Notes)
	}
}

// A drawer opened on an id the scanner has since pruned must say so, not return
// a zero-valued record that renders as a real 0-credit opportunity.
func TestLoadOpportunity_MissingIDIsNotFound(t *testing.T) {
	_, err := LoadOpportunity(context.Background(),
		fixtureOppMarket(t), fixtureOppKB(t), 999999)
	if !errors.Is(err, ErrOpportunityNotFound) {
		t.Fatalf("err = %v, want ErrOpportunityNotFound", err)
	}
}

// Unreachable endpoints (disconnected graph, or a station whose system we have
// never surveyed) must report "unknown" rather than 0, because 0 jumps reads as
// "same station, free run" -- the most expensive possible misreading.
func TestLoadOpportunity_UnreachableRouteReportsUnknownNotZero(t *testing.T) {
	mkt := filepath.Join(t.TempDir(), "market.db")
	db, err := sql.Open(sqliteDriver, mkt)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE arbitrage_opportunities (
			id INTEGER PRIMARY KEY, from_station_id TEXT, to_station_id TEXT,
			item_id TEXT, action_type TEXT, buy_price REAL, sell_price REAL,
			quantity REAL, source_units REAL, gross_profit REAL,
			claimed_by TEXT, claimed_at TEXT, status TEXT, expires_at TEXT,
			discovered_at TEXT, discovered_by TEXT, notes TEXT)`,
		`INSERT INTO arbitrage_opportunities VALUES
			(7,'grand_exchange','unsurveyed_station','ore','buy_then_sell',
			 10,20,5,5,50,NULL,NULL,'available',NULL,NULL,'scanner',NULL)`,
	}
	for _, s := range stmts {
		if _, err = db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.Close() //nolint:errcheck,gosec

	got, err := LoadOpportunity(context.Background(), mkt, fixtureOppKB(t), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Jumps != -1 {
		t.Errorf("Jumps = %d, want -1 (unknown); 0 would read as a free same-station run", got.Jumps)
	}
	if got.ProfitPerJump != 0 {
		t.Errorf("ProfitPerJump = %v, want 0 when the route is unknown", got.ProfitPerJump)
	}
}
