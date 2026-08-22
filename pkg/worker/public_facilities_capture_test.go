package worker

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestUpsertPublicFromFacilityList(t *testing.T) {
	raw, err := os.ReadFile("testdata/facility_list_public.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create in-memory KB: %v", err)
	}

	ctx := context.Background()

	n, _, err := upsertPublicFromFacilityList(ctx, kb, raw, 100)
	if err != nil {
		t.Fatalf("upsertPublicFromFacilityList returned error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 upserted row (private facility filtered out), got %d", n)
	}

	ceramite, err := kb.FacilitiesForRecipe(ctx, "ceramite_plating")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(ceramite_plating) error: %v", err)
	}
	if len(ceramite) != 1 {
		t.Fatalf("expected 1 facility for ceramite_plating, got %d", len(ceramite))
	}
	f := ceramite[0]
	if f.StationID != "grand_exchange_station" {
		t.Errorf("StationID = %q, want grand_exchange_station", f.StationID)
	}
	if f.Level != 2 {
		t.Errorf("Level = %d, want 2", f.Level)
	}
	if f.RentalFeePerRun != 50 {
		t.Errorf("RentalFeePerRun = %d, want 50", f.RentalFeePerRun)
	}
	if f.OwnerFaction != "CRFT" {
		t.Errorf("OwnerFaction = %q, want CRFT", f.OwnerFaction)
	}

	reactor, err := kb.FacilitiesForRecipe(ctx, "reactor_core")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(reactor_core) error: %v", err)
	}
	if len(reactor) != 0 {
		t.Fatalf("expected 0 facilities for reactor_core (private, not captured), got %d", len(reactor))
	}
}

// Live payloads (e.g. voss_redoubt) often carry NO public_facilities section at
// all: station-owned public production lines arrive under station_facilities and
// our own faction's rent-out lines under faction_facilities. Both must reach the
// catalog. Those sections also mix public and private lines, and a private line
// (The Red Room) omits production.public entirely rather than setting it false —
// so an explicit true is required, not merely a non-false.
func TestUpsertPublicFromFacilityList_AllSections(t *testing.T) {
	raw, err := os.ReadFile("testdata/facility_list_sections.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create in-memory KB: %v", err)
	}

	ctx := context.Background()

	n, _, err := upsertPublicFromFacilityList(ctx, kb, raw, 100)
	if err != nil {
		t.Fatalf("upsertPublicFromFacilityList returned error: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 upserted rows (station + faction public production), got %d", n)
	}

	steel, err := kb.FacilitiesForRecipe(ctx, "refine_steel")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(refine_steel) error: %v", err)
	}
	if len(steel) != 1 {
		t.Fatalf("expected 1 facility for refine_steel (station_facilities), got %d", len(steel))
	}
	if got := steel[0].StationID; got != "voss_redoubt_station" {
		t.Errorf("StationID = %q, want voss_redoubt_station", got)
	}
	if got := steel[0].Level; got != 2 {
		t.Errorf("Level = %d, want 2", got)
	}
	if got := steel[0].RentalFeePerRun; got != 35 {
		t.Errorf("RentalFeePerRun = %d, want 35", got)
	}

	cells, err := kb.FacilitiesForRecipe(ctx, "crack_hot_cells")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(crack_hot_cells) error: %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 facility for crack_hot_cells (faction_facilities), got %d", len(cells))
	}
	if got := cells[0].OwnerFaction; got != "VOSS" {
		t.Errorf("OwnerFaction = %q, want VOSS", got)
	}

	// The Red Room: category=production but production.public is ABSENT. It is
	// private and must never be offered as a craft-for-hire site.
	redMist, err := kb.FacilitiesForRecipe(ctx, "load_red_mist")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(load_red_mist) error: %v", err)
	}
	if len(redMist) != 0 {
		t.Fatalf("expected 0 facilities for load_red_mist (public key absent = private), got %d", len(redMist))
	}
}

// facilityListJSON builds a `facility list` reply for one station carrying the
// given public production lines.
func facilityListJSON(baseID string, facilityIDs ...string) []byte {
	var b strings.Builder
	b.WriteString(`{"base_id":"` + baseID + `","station_facilities":[`)
	for i, id := range facilityIDs {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"facility_id":"` + id + `","name":"` + id + `","category":"production",` +
			`"recipe_id":"refine_steel","level":1,` +
			`"production":{"public":true,"rental_fee_per_run":10}}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func facilityIDsAtStation(t *testing.T, kb *knowledge.SQLiteKB, station string) []string {
	t.Helper()
	facs, err := kb.FacilitiesForRecipe(context.Background(), "refine_steel")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe: %v", err)
	}
	var out []string
	for _, f := range facs {
		if f.StationID == station {
			out = append(out, f.FacilityID)
		}
	}
	sort.Strings(out)
	return out
}

// The bug this fixes: a facility that gets dismantled between two visits used
// to stay on file forever, still answering "you can build this recipe here".
func TestUpsertPublicFromFacilityList_PrunesFacilityThatVanished(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	ctx := context.Background()

	if _, _, err := upsertPublicFromFacilityList(ctx, kb, facilityListJSON("alpha", "f1", "f2"), 100); err != nil {
		t.Fatalf("first capture: %v", err)
	}
	if got := facilityIDsAtStation(t, kb, "alpha"); len(got) != 2 {
		t.Fatalf("after first capture = %v, want two facilities", got)
	}

	// Second visit: f2 is gone.
	captured, pruned, err := upsertPublicFromFacilityList(ctx, kb, facilityListJSON("alpha", "f1"), 200)
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if captured != 1 || pruned != 1 {
		t.Errorf("captured=%d pruned=%d, want 1 and 1", captured, pruned)
	}
	if got := facilityIDsAtStation(t, kb, "alpha"); len(got) != 1 || got[0] != "f1" {
		t.Errorf("after second capture = %v, want [f1]", got)
	}
}

// A station whose LAST public line is gone still returns facilities — just no
// public production ones. That is a real observation and must prune; before
// the prune existed this path returned early and the row lived forever.
func TestUpsertPublicFromFacilityList_PrunesWhenNoPublicLinesRemain(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	ctx := context.Background()

	if _, _, err := upsertPublicFromFacilityList(ctx, kb, facilityListJSON("alpha", "f1"), 100); err != nil {
		t.Fatalf("first capture: %v", err)
	}

	// Same station, one facility, now PRIVATE (production.public omitted).
	private := []byte(`{"base_id":"alpha","station_facilities":[` +
		`{"facility_id":"f1","name":"f1","category":"production","recipe_id":"refine_steel",` +
		`"production":{"rental_fee_per_run":10}}]}`)
	captured, pruned, err := upsertPublicFromFacilityList(ctx, kb, private, 200)
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if captured != 0 || pruned != 1 {
		t.Errorf("captured=%d pruned=%d, want 0 and 1", captured, pruned)
	}
	if got := facilityIDsAtStation(t, kb, "alpha"); len(got) != 0 {
		t.Errorf("after second capture = %v, want none", got)
	}
}

// The safety property. A capture that did not come back cleanly must never be
// read as "this station has no facilities" — that would delete live rows, the
// same failure shape as the catalog refresh that erased the legacy mining
// hulls. GetRawJSON("_last") really can hand back another command's reply.
func TestUpsertPublicFromFacilityList_IncompleteReplyNeverPrunes(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"no station id", []byte(`{"station_facilities":[{"facility_id":"x","category":"production","production":{"public":true}}]}`)},
		{"no sections at all", []byte(`{"base_id":"alpha"}`)},
		{"another command's reply", []byte(`{"base_id":"alpha","credits":1000}`)},
		{"empty sections", []byte(`{"base_id":"alpha","station_facilities":[],"faction_facilities":[],"public_facilities":[]}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
			if err != nil {
				t.Fatalf("open kb: %v", err)
			}
			ctx := context.Background()
			if _, _, err := upsertPublicFromFacilityList(ctx, kb, facilityListJSON("alpha", "f1"), 100); err != nil {
				t.Fatalf("seed capture: %v", err)
			}

			captured, pruned, err := upsertPublicFromFacilityList(ctx, kb, tc.raw, 200)
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			if captured != 0 || pruned != 0 {
				t.Errorf("captured=%d pruned=%d, want 0 and 0 (no prune from an incomplete reply)", captured, pruned)
			}
			if got := facilityIDsAtStation(t, kb, "alpha"); len(got) != 1 || got[0] != "f1" {
				t.Errorf("facilities = %v, want [f1] still on file", got)
			}
		})
	}
}

// One station's scrape must not touch another station's catalog.
func TestUpsertPublicFromFacilityList_PruneIsScopedToOneStation(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	ctx := context.Background()
	if _, _, err := upsertPublicFromFacilityList(ctx, kb, facilityListJSON("alpha", "f1"), 100); err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	if _, _, err := upsertPublicFromFacilityList(ctx, kb, facilityListJSON("beta", "g1"), 100); err != nil {
		t.Fatalf("seed beta: %v", err)
	}

	// alpha loses its only public line.
	if _, pruned, err := upsertPublicFromFacilityList(ctx, kb, []byte(`{"base_id":"alpha","station_facilities":[{"facility_id":"f1","category":"storage"}]}`), 200); err != nil {
		t.Fatalf("alpha recapture: %v", err)
	} else if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if got := facilityIDsAtStation(t, kb, "beta"); len(got) != 1 || got[0] != "g1" {
		t.Errorf("beta = %v, want [g1] untouched", got)
	}
}
