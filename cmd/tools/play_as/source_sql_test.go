package main

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// station_id is a base id; jumps key on the POI's system. Four fleet stations
// spell these differently, so a naive station_id == poi_id lookup silently
// yields "" and every haul leg costs RouteInf.
func TestCraftbrainSource_SystemOfResolvesBaseToPOISystem(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db := kb.DB()
	// pois requires name, type, position_x, position_y (all NOT NULL).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y)
		 VALUES ('sol_central','sol','Sol Central','station',0,0)`); err != nil {
		t.Fatal(err)
	}
	// bases requires poi_id and name (NOT NULL).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO bases (id, poi_id, name) VALUES ('confederacy_central_command','sol_central','CCC')`); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	got, err := src.SystemOf(ctx, "confederacy_central_command")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sol" {
		t.Errorf("SystemOf(confederacy_central_command) = %q, want %q", got, "sol")
	}
}

// A station_id that is already a poi id must still resolve.
func TestCraftbrainSource_SystemOfFallsBackToPOIID(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := kb.DB().ExecContext(ctx,
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y)
		 VALUES ('ramens_rest','haven','Ramens Rest','station',0,0)`); err != nil {
		t.Fatal(err)
	}
	src := newCraftbrainSource(kb, nil, "haven")
	got, err := src.SystemOf(ctx, "ramens_rest")
	if err != nil {
		t.Fatal(err)
	}
	if got != "haven" {
		t.Errorf("SystemOf(ramens_rest) = %q, want haven", got)
	}
}

// Hard requirement 1: Facilities() must call ParseProduction on every row it
// returns. site.go trusts OutputPerRun/TicksPerRun to already be parsed and
// reads them directly off the Facility; skipping ParseProduction would leave
// OutputPerRun at its Go zero value (0) instead of the safe default (1),
// silently corrupting run-count arithmetic upstream.
func TestCraftbrainSource_Facilities_ParsesProduction(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const details = `{"production":{"ticks_per_run":4.0,"output_per_run":2,"backlog_ticks":1.5,"rental_fee_per_run":35}}`
	if err := kb.UpsertPublicFacilities(ctx, []knowledge.PublicFacility{
		{StationID: "voss_redoubt_station", FacilityID: "sf-steel-1", RecipeID: "refine_steel",
			Category: "production", Level: 2, RentalFeePerRun: 35, LastSeenTick: 100,
			DetailsJSON: details},
	}); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	got, err := src.Facilities(ctx, "refine_steel")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 facility, got %d", len(got))
	}
	f := got[0]
	if f.OutputPerRun != 2 {
		t.Errorf("OutputPerRun = %d, want 2 (ParseProduction not called, or not parsed)", f.OutputPerRun)
	}
	if f.TicksPerRun != 4.0 {
		t.Errorf("TicksPerRun = %v, want 4.0", f.TicksPerRun)
	}
	if f.BacklogTicks != 1.5 {
		t.Errorf("BacklogTicks = %v, want 1.5", f.BacklogTicks)
	}
}

// Facilities() with no details_json payload must still default OutputPerRun
// to 1 (ParseProduction's safety default), never leave it at the Go zero
// value which would divide run counts by zero downstream.
func TestCraftbrainSource_Facilities_DefaultsOutputPerRunWithoutDetails(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := kb.UpsertPublicFacilities(ctx, []knowledge.PublicFacility{
		{StationID: "grand_exchange", FacilityID: "f1", RecipeID: "ceramite_plating",
			Category: "production", Level: 1, RentalFeePerRun: 10, LastSeenTick: 100},
	}); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	got, err := src.Facilities(ctx, "ceramite_plating")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 facility, got %d", len(got))
	}
	if got[0].OutputPerRun != 1 {
		t.Errorf("OutputPerRun = %d, want 1 (ParseProduction default)", got[0].OutputPerRun)
	}
}

// Hard requirement 3: OnHand must not emit duplicate (Holder, BaseID) rows.
// Two different factions each holding stock at the same base_id must
// collapse into a single Holding, since Holder is uniformly "" for the
// faction pool and the Holding model assumes at most one row per
// (holder, base).
func TestCraftbrainSource_OnHand_DedupesFactionStorageAcrossFactions(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db := kb.DB()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO faction_storage_items (faction_id, base_id, item_id, name, quantity, size, captured_utc)
		VALUES ('CRFT', 'shared_base', 'iron_ore', 'Iron Ore', 10, 1, '2026-07-09T10:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO faction_storage_items (faction_id, base_id, item_id, name, quantity, size, captured_utc)
		VALUES ('WAR', 'shared_base', 'iron_ore', 'Iron Ore', 5, 1, '2026-07-09T11:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	got, err := src.OnHand(ctx, "iron_ore")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 deduped Holding for shared_base, got %d: %+v", len(got), got)
	}
	if got[0].BaseID != "shared_base" || got[0].Holder != "" {
		t.Errorf("Holding = %+v, want BaseID=shared_base Holder=\"\"", got[0])
	}
	if got[0].Qty != 15 {
		t.Errorf("Qty = %d, want 15 (10+5 summed across factions)", got[0].Qty)
	}
}

// Personal storage_snapshots is upsert-only (UNIQUE(agent_id, base_id)):
// re-storing a snapshot for the same agent+base must still yield exactly one
// Holding, never two, and the quantity must reflect the latest write.
func TestCraftbrainSource_OnHand_PersonalStorageUpsertStaysSingleRow(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := kb.StoreStorageSnapshot(ctx, knowledge.StorageSnapshot{
		AgentID: "hauler-1", BaseID: "sol_central", Credits: 100,
		Items:      []knowledge.StorageSnapshotItem{{ItemID: "iron_ore", Quantity: 20}},
		CapturedAt: time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.StoreStorageSnapshot(ctx, knowledge.StorageSnapshot{
		AgentID: "hauler-1", BaseID: "sol_central", Credits: 100,
		Items:      []knowledge.StorageSnapshotItem{{ItemID: "iron_ore", Quantity: 35}},
		CapturedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	got, err := src.OnHand(ctx, "iron_ore")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 Holding, got %d: %+v", len(got), got)
	}
	if got[0].Qty != 35 {
		t.Errorf("Qty = %d, want 35 (latest snapshot write)", got[0].Qty)
	}
	if got[0].Holder != "hauler-1" || got[0].BaseID != "sol_central" {
		t.Errorf("Holding = %+v, want Holder=hauler-1 BaseID=sol_central", got[0])
	}
}

// Hard requirement 2: OnHand must have an explicit deterministic ORDER BY.
// Multiple holders/bases must come back in a stable, reproducible order
// across repeated calls -- not dependent on Go map iteration or SQLite's
// unspecified row order.
func TestCraftbrainSource_OnHand_DeterministicOrder(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, h := range []struct {
		agent, base string
		qty         float64
	}{
		{"zeta-hauler", "voss_redoubt", 5},
		{"alpha-hauler", "sol_central", 10},
		{"mid-hauler", "haven_station", 7},
	} {
		if err := kb.StoreStorageSnapshot(ctx, knowledge.StorageSnapshot{
			AgentID: h.agent, BaseID: h.base,
			Items:      []knowledge.StorageSnapshotItem{{ItemID: "titanium", Quantity: h.qty}},
			CapturedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	src := newCraftbrainSource(kb, nil, "sol")
	var first []craftbrainHolding
	for i := range 5 {
		got, err := src.OnHand(ctx, "titanium")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("run %d: want 3 holdings, got %d", i, len(got))
		}
		cur := make([]craftbrainHolding, len(got))
		for j, h := range got {
			cur[j] = craftbrainHolding{h.Holder, h.BaseID, h.Qty}
		}
		if first == nil {
			first = cur
			continue
		}
		for j := range cur {
			if cur[j] != first[j] {
				t.Fatalf("run %d order differs from run 0 at index %d: %+v vs %+v", i, j, cur, first)
			}
		}
	}
	// Explicitly assert the order matches ORDER BY agent_id ascending.
	want := []string{"alpha-hauler", "mid-hauler", "zeta-hauler"}
	for i, h := range first {
		if h.holder != want[i] {
			t.Errorf("index %d holder = %q, want %q (ORDER BY agent_id ascending)", i, h.holder, want[i])
		}
	}
}

// craftbrainHolding is a comparable projection of craftbrain.Holding used to
// diff ordering across repeated OnHand calls without importing the
// craftbrain package's non-comparable time.Time-bearing struct by value
// comparison quirks.
type craftbrainHolding struct {
	holder string
	base   string
	qty    int
}
