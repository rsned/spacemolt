package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

const browseShipsRaw = `{"base_id":"nyx_nexus_station","base_name":"Nyx Nexus Station","count":2,"listings":[
 {"category":"Combat","class_id":"eviction_notice","listed_at":"2026-05-23T20:58:34Z","listing_id":"l1","max_hull":480,"modules_count":6,"price":133174,"scale":3,"seller":"[Station Manager]","shield":200,"ship_id":"s1","ship_name":"Eviction Notice","tier":3},
 {"category":"Exploration","class_id":"lemming","hull":80,"listed_at":"2026-06-18T17:32:17Z","listing_id":"l2","max_hull":80,"modules_count":2,"price":9738,"scale":1,"seller":"[Station Manager]","shield":40,"ship_id":"s2","ship_name":"Lemming","tier":1}
]}`

// KBUpdateStation must decode browse_ships from the "ship_listings" raw key
// and store a snapshot with CapturedAt set (the old path read a dead "ships"
// key and silently skipped for months).
func TestKBUpdateStationStoresShipListings(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	st := &game.State{Doc: true}
	st.System.ID = "nyx"
	st.System.Name = "Nyx"
	st.CurrentPOI = "nyx_nexus_station"
	client := &fakeClient{
		state: st,
		raw:   map[string][]byte{"ship_listings": []byte(browseShipsRaw)},
	}

	if err := KBUpdateStation(context.Background(), client, kb, nil, "test"); err != nil {
		t.Fatal(err)
	}

	got, err := kb.GetLatestShipListings(context.Background(), "nyx", "nyx_nexus_station")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Listings) != 2 {
		t.Fatalf("want 2 stored listings, got %+v", got)
	}
	if got.CapturedAt.IsZero() {
		t.Error("CapturedAt must be set (was zero-time for all historical rows)")
	}
	if got.Listings[0].Hull != -1 && got.Listings[1].Hull != -1 {
		t.Error("absent hull must be stored as -1")
	}
}
