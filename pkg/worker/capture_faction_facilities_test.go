package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

type fakeFactionFacilityStore struct {
	calls []struct {
		factionID, baseID string
		rows              []knowledge.FactionFacilityRow
	}
}

func (f *fakeFactionFacilityStore) ReplaceFactionFacilities(_ context.Context,
	factionID, baseID string, rows []knowledge.FactionFacilityRow,
) error {
	f.calls = append(f.calls, struct {
		factionID, baseID string
		rows              []knowledge.FactionFacilityRow
	}{factionID, baseID, rows})

	return nil
}

// The facility faction_list reply already fetched by the hourly `facilities`
// task carries our OWN faction's production lines, and nothing wrote them:
// faction_facilities held ONE row, from 2026-05-21, while public_facilities
// held 2,509 rows minutes old. The scheduled capture_faction command is a
// different code path entirely (assets.CaptureFaction -> assets.db) and never
// touches facilities, so the table that should describe our own factories was
// five months stale.
func TestUpsertFactionFromFacilityList_RecordsOurLines(t *testing.T) {
	store := &fakeFactionFacilityStore{}
	raw := []byte(`{
	  "base_id": "grand_exchange_station",
	  "faction_facilities": [
	    {"facility_id":"backwater_still","name":"Backwater Still","category":"production",
	     "level":1,"status":"active","recipe_id":"distill_moonshine"},
	    {"facility_id":"bootleg_cell_shop","name":"Bootleg Cell Shop","category":"production",
	     "level":1,"status":"active","production":{"recipe":"crack_hot_cells"}}
	  ],
	  "station_facilities": []
	}`)

	n, err := upsertFactionFromFacilityList(context.Background(), store, raw, "crft-id")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("captured %d, want 2", n)
	}
	if len(store.calls) != 1 {
		t.Fatalf("Replace called %d times, want 1", len(store.calls))
	}
	call := store.calls[0]
	if call.factionID != "crft-id" || call.baseID != "grand_exchange_station" {
		t.Errorf("replaced (%s,%s), want (crft-id,grand_exchange_station)", call.factionID, call.baseID)
	}
	// recipe_id comes either from the top level or from production.recipe --
	// both shapes appear in live payloads.
	got := map[string]string{}
	for _, r := range call.rows {
		got[r.FacilityID] = r.RecipeID
	}
	if got["backwater_still"] != "distill_moonshine" {
		t.Errorf("backwater_still recipe = %q", got["backwater_still"])
	}
	if got["bootleg_cell_shop"] != "crack_hot_cells" {
		t.Errorf("bootleg_cell_shop recipe = %q (production.recipe fallback)", got["bootleg_cell_shop"])
	}
}

// ReplaceFactionFacilities PRUNES, so it must never run against a reply we do
// not believe is a complete listing for that station. GetRawJSON("_last") can
// hand back another command's reply, and treating that as "our faction has no
// facilities here" would erase live rows -- the same hazard the public-facility
// capture already guards.
func TestUpsertFactionFromFacilityList_RefusesToPruneOnADoubtfulReply(t *testing.T) {
	for _, tc := range []struct {
		name      string
		raw       string
		factionID string
	}{
		{"no base_id", `{"faction_facilities":[{"facility_id":"x","category":"production"}]}`, "crft-id"},
		{"no facility sections at all", `{"base_id":"grand_exchange_station"}`, "crft-id"},
		{"another command's reply", `{"action":"view_market","listings":[]}`, "crft-id"},
		{"not in a faction", `{"base_id":"grand_exchange_station","faction_facilities":[]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeFactionFacilityStore{}
			if _, err := upsertFactionFromFacilityList(context.Background(), store, []byte(tc.raw), tc.factionID); err != nil {
				t.Fatal(err)
			}
			if len(store.calls) != 0 {
				t.Errorf("Replace called on a doubtful reply; it prunes: %+v", store.calls)
			}
		})
	}
}

// A listing that DOES name sections but contains no faction line of ours is a
// real observation -- our facilities there are gone -- and must prune.
func TestUpsertFactionFromFacilityList_PrunesOnAGenuineEmptyListing(t *testing.T) {
	store := &fakeFactionFacilityStore{}
	raw := []byte(`{"base_id":"grand_exchange_station","faction_facilities":[],
	                "station_facilities":[{"facility_id":"npc_forge","category":"production"}]}`)

	n, err := upsertFactionFromFacilityList(context.Background(), store, raw, "crft-id")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("captured %d, want 0", n)
	}
	if len(store.calls) != 1 {
		t.Fatalf("want one Replace with zero rows (a real prune), got %d calls", len(store.calls))
	}
	if len(store.calls[0].rows) != 0 {
		t.Errorf("want zero rows, got %d", len(store.calls[0].rows))
	}
}
