package knowledge

import (
	"context"
	"testing"
	"time"
)

// Live browse_ships fixture (trimmed from a 2026-07-04 Nyx Nexus capture):
// one listing with hull absent, one damaged (hull 22), one at full (hull 200).
const browseShipsFixture = `{
  "base_id": "nyx_nexus_station",
  "base_name": "Nyx Nexus Station",
  "count": 3,
  "listings": [
    {"category":"Combat","class_id":"eviction_notice","listed_at":"2026-05-23T20:58:34Z","listing_id":"70b2ce927871dc69f45996d517f33636","max_hull":480,"modules_count":6,"price":133174,"scale":3,"seller":"[Station Manager: Nyx Nexus Station]","shield":200,"ship_id":"720608fde6e20c73af4552bc90e9b382","ship_name":"Eviction Notice","tier":3},
    {"category":"Combat","class_id":"close_enough","hull":22,"listed_at":"2026-05-23T20:58:34Z","listing_id":"3f106b29d47eef035b42db3801cd859b","max_hull":200,"modules_count":5,"price":110975,"scale":2,"seller":"[Station Manager: Nyx Nexus Station]","shield":140,"ship_id":"adebe3be73391e60968e2879be328e94","ship_name":"Close Enough","tier":2},
    {"category":"Industrial","class_id":"losers_weepers","hull":70,"listed_at":"2026-06-18T17:32:17Z","listing_id":"0c5a1ea1f539a789885ab56a8494cb23","max_hull":70,"modules_count":2,"price":13688,"scale":1,"seller":"[Station Manager: Nyx Nexus Station]","shield":25,"ship_id":"66c0373c1002e3cf6ef776bc878bb74b","ship_name":"Losers Weepers","tier":1}
  ]
}`

func TestShipListingsFromBrowseJSON(t *testing.T) {
	baseID, ships, err := ShipListingsFromBrowseJSON([]byte(browseShipsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if baseID != "nyx_nexus_station" {
		t.Errorf("baseID = %q, want nyx_nexus_station", baseID)
	}
	if len(ships) != 3 {
		t.Fatalf("got %d listings, want 3", len(ships))
	}
	first := ships[0]
	if first.ListingID != "70b2ce927871dc69f45996d517f33636" || first.ClassID != "eviction_notice" ||
		first.Price != 133174 || first.Tier != 3 || first.MaxHull != 480 ||
		first.Seller != "[Station Manager: Nyx Nexus Station]" || first.ListedAt != "2026-05-23T20:58:34Z" {
		t.Errorf("first listing mapped wrong: %+v", first)
	}
	if first.Hull != -1 {
		t.Errorf("absent hull must map to -1, got %d", first.Hull)
	}
	if ships[1].Hull != 22 {
		t.Errorf("damaged hull must pass through, got %d", ships[1].Hull)
	}
}

func TestStoreShipListingsReplacesPerStation(t *testing.T) {
	ctx := context.Background()
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = kb.Close() }()

	snap := func(station string, n int) ShipListings {
		ls := make([]ShipListing, n)
		for i := range ls {
			ls[i] = ShipListing{ListingID: station + string(rune('a'+i)), ShipID: "s", ClassID: "lemming", Price: 9738, Hull: -1}
		}
		return ShipListings{
			SystemID: "nyx", SystemName: "Nyx", StationID: station, StationName: station,
			GameTick: 100, CapturedAt: time.Now().UTC(), Listings: ls,
		}
	}

	if err := kb.StoreShipListings(ctx, snap("station_a", 3), "test"); err != nil {
		t.Fatal(err)
	}
	if err := kb.StoreShipListings(ctx, snap("station_b", 2), "test"); err != nil {
		t.Fatal(err)
	}
	// Re-capture station_a with fewer listings: must REPLACE, not append.
	if err := kb.StoreShipListings(ctx, snap("station_a", 1), "test"); err != nil {
		t.Fatal(err)
	}

	got, err := kb.GetLatestShipListings(ctx, "nyx", "station_a")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Listings) != 1 {
		t.Fatalf("station_a must hold exactly the last snapshot (1 listing), got %+v", got)
	}
	if got.Listings[0].ClassID != "lemming" || got.Listings[0].Price != 9738 {
		t.Errorf("scanned listing fields wrong: %+v", got.Listings[0])
	}
	other, err := kb.GetLatestShipListings(ctx, "nyx", "station_b")
	if err != nil {
		t.Fatal(err)
	}
	if other == nil || len(other.Listings) != 2 {
		t.Fatalf("station_b snapshot must be untouched, got %+v", other)
	}
}

func TestMemoryKBStoreShipListingsReplaces(t *testing.T) {
	ctx := context.Background()
	kb := NewMemoryKB()
	mk := func(station string, n int) ShipListings {
		return ShipListings{SystemID: "nyx", StationID: station, Listings: make([]ShipListing, n)}
	}
	_ = kb.StoreShipListings(ctx, mk("a", 3), "t")
	_ = kb.StoreShipListings(ctx, mk("a", 1), "t")
	got, _ := kb.GetLatestShipListings(ctx, "nyx", "a")
	if got == nil || len(got.Listings) != 1 {
		t.Fatalf("MemoryKB must replace per station, got %+v", got)
	}
}
