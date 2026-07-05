package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// newShipsTestServer builds a knowledge DB with ship-listing fixtures via the
// real KB (migrations + StoreShipListings), then reopens it read-only the way
// main() does and returns a mux with the ships routes registered.
func newShipsTestServer(t *testing.T) *http.ServeMux {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "kb.db")

	kbw, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	captured := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	snapA := knowledge.ShipListings{
		SystemID: "nyx", SystemName: "Nyx", StationID: "station_a", StationName: "Alpha Port",
		GameTick: 100, CapturedAt: captured,
		Listings: []knowledge.ShipListing{
			{ListingID: "a1", ShipID: "s1", ClassID: "archimedes", ShipName: "Archimedes", Category: "hauler", Tier: 1, Hull: -1, MaxHull: 200, Shield: 50, ModulesCount: 3, Price: 12000, Seller: "npc"},
			{ListingID: "a2", ShipID: "s2", ClassID: "archimedes", ShipName: "Archimedes", Category: "hauler", Tier: 1, Hull: 150, MaxHull: 200, Shield: 50, ModulesCount: 3, Price: 10834, Seller: "player_x"},
			{ListingID: "a3", ShipID: "s3", ClassID: "gas_raider", ShipName: "Gas Raider", Category: "combat", Tier: 2, Hull: -1, MaxHull: 400, Shield: 120, ModulesCount: 5, Price: 212000},
		},
	}
	snapB := knowledge.ShipListings{
		SystemID: "vega", SystemName: "Vega", StationID: "station_b", StationName: "Beta Dock",
		GameTick: 100, CapturedAt: captured,
		Listings: []knowledge.ShipListing{
			{ListingID: "b1", ShipID: "s4", ClassID: "archimedes", ShipName: "Archimedes", Category: "hauler", Tier: 1, Hull: -1, MaxHull: 200, Shield: 50, ModulesCount: 3, Price: 11500},
		},
	}
	if err := kbw.StoreShipListings(ctx, snapA, "test"); err != nil {
		t.Fatal(err)
	}
	if err := kbw.StoreShipListings(ctx, snapB, "test"); err != nil {
		t.Fatal(err)
	}
	if err := kbw.Close(); err != nil {
		t.Fatal(err)
	}

	kb, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	srv := &server{kb: kb}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ships", srv.shipsHandler)
	mux.HandleFunc("GET /api/ships/{id}", srv.shipClassHandler)
	return mux
}

func TestShipsHandlerAggregates(t *testing.T) {
	mux := newShipsTestServer(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ships", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got []shipClassSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 classes, got %d: %+v", len(got), got)
	}
	// Sorted by listing_count desc: archimedes (3) before gas_raider (1).
	arch := got[0]
	if arch.ClassID != "archimedes" || arch.ListingCount != 3 {
		t.Fatalf("first row must be archimedes with 3 listings, got %+v", arch)
	}
	if arch.MinPrice != 10834 || arch.MaxPrice != 12000 {
		t.Errorf("archimedes price range = %d–%d, want 10834–12000", arch.MinPrice, arch.MaxPrice)
	}
	if arch.StationCount != 2 {
		t.Errorf("archimedes station_count = %d, want 2", arch.StationCount)
	}
	if arch.CheapestStationID != "station_a" || arch.CheapestStationName != "Alpha Port" {
		t.Errorf("cheapest station = %s/%s, want station_a/Alpha Port", arch.CheapestStationID, arch.CheapestStationName)
	}
	gas := got[1]
	if gas.ClassID != "gas_raider" || gas.MinPrice != 212000 || gas.Tier != 2 {
		t.Errorf("gas_raider row wrong: %+v", gas)
	}
}

func TestShipClassHandlerDrillDown(t *testing.T) {
	mux := newShipsTestServer(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ships/archimedes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got []shipListingRow
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 archimedes listings, got %d", len(got))
	}
	// Sorted by price asc.
	if got[0].Price != 10834 || got[1].Price != 11500 || got[2].Price != 12000 {
		t.Errorf("prices not ascending: %d, %d, %d", got[0].Price, got[1].Price, got[2].Price)
	}
	if got[0].Hull != 150 || got[0].Seller != "player_x" {
		t.Errorf("cheapest listing detail wrong: %+v", got[0])
	}
	if got[1].StationID != "station_b" || got[1].SystemName != "Vega" {
		t.Errorf("station/system columns wrong: %+v", got[1])
	}
	if got[2].Hull != -1 {
		t.Errorf("unreported hull must stay -1, got %d", got[2].Hull)
	}
}

func TestShipClassHandlerUnknownClass(t *testing.T) {
	mux := newShipsTestServer(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ships/no_such_class", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("unknown class must return empty array, got %q", body)
	}
}
