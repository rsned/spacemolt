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

// The planner sites hand-crafts at the "any_docked_station" sentinel; it is
// not a real base, so the SQL lookups can never resolve it. Left unresolved,
// destSys is "" and EVERY haul leg in a hand-craft plan degrades to
// unknown_route (observed on the first live smoke run, 2026-07-10). The
// sentinel means "wherever the operator is docked", so resolve it to the
// origin system the source was constructed with. The literal string is the
// wire value craftbrain emits in plan JSON — pin it, not a shared const.
func TestCraftbrainSource_SystemOfResolvesCraftSentinelToOrigin(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	src := newCraftbrainSource(kb, nil, "haven")
	got, err := src.SystemOf(context.Background(), "any_docked_station")
	if err != nil {
		t.Fatal(err)
	}
	if got != "haven" {
		t.Errorf("SystemOf(any_docked_station) = %q, want origin system %q", got, "haven")
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

// Finding A: a malformed details_json must not produce a "phantom" facility
// that wins every ranking. ParseProduction returns an error on unparseable
// JSON, leaving TicksPerRun=0 and BacklogTicks=0 -- not a safe default from
// site.go's perspective, since it ranks candidates by
// runs*TicksPerRun+BacklogTicks: a corrupt row would look like a zero-cost,
// instant facility and beat every valid one. The row must be excluded
// entirely, not returned with those defaults, while a well-formed sibling
// row for the same recipe still comes back.
func TestCraftbrainSource_Facilities_SkipsMalformedDetailsJSON(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := kb.UpsertPublicFacilities(ctx, []knowledge.PublicFacility{
		{StationID: "corrupt_station", FacilityID: "bad-1", RecipeID: "refine_steel",
			Category: "production", Level: 1, RentalFeePerRun: 10, LastSeenTick: 100,
			DetailsJSON: "{not json"},
		{StationID: "good_station", FacilityID: "good-1", RecipeID: "refine_steel",
			Category: "production", Level: 2, RentalFeePerRun: 35, LastSeenTick: 100,
			DetailsJSON: `{"production":{"ticks_per_run":4.0,"output_per_run":2,"backlog_ticks":1.5,"rental_fee_per_run":35}}`},
	}); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	got, err := src.Facilities(ctx, "refine_steel")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 facility (malformed row excluded), got %d: %+v", len(got), got)
	}
	if got[0].StationID != "good_station" {
		t.Errorf("StationID = %q, want good_station (the well-formed sibling survives)", got[0].StationID)
	}
}

// Finding B: Coverage()'s facility_only-covered count (and its Stations
// count) must apply the exact predicate FacilitiesForRecipe uses (public = 1
// AND category = 'production'), or the honesty footer overstates coverage: a
// facility_only recipe whose only public_facilities row is a non-production
// category would count as "covered" here while Facilities() returns nothing
// for it and the node comes out BLOCKED.
func TestCraftbrainSource_Coverage_ExcludesNonProductionFacilities(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := kb.StoreRecipes(ctx, []knowledge.RecipeDef{
		{ID: "reactor_core", Name: "Reactor Core", FacilityOnly: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.UpsertPublicFacilities(ctx, []knowledge.PublicFacility{
		{StationID: "service_station", FacilityID: "svc-1", RecipeID: "reactor_core",
			Category: "service", Level: 1, RentalFeePerRun: 10, LastSeenTick: 100},
	}); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	cov, err := src.Coverage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cov.FacilityOnlyTotal != 1 {
		t.Fatalf("FacilityOnlyTotal = %d, want 1", cov.FacilityOnlyTotal)
	}
	if cov.FacilityOnlyCovered != 0 {
		t.Errorf("FacilityOnlyCovered = %d, want 0 (only row is category=service, not production)", cov.FacilityOnlyCovered)
	}
	if cov.Stations != 0 {
		t.Errorf("Stations = %d, want 0 (service_station's only row is non-production)", cov.Stations)
	}

	// Sanity: a sibling production row at a different station does count,
	// proving the predicate isn't just excluding everything.
	if err := kb.UpsertPublicFacilities(ctx, []knowledge.PublicFacility{
		{StationID: "production_station", FacilityID: "prod-1", RecipeID: "reactor_core",
			Category: "production", Level: 1, RentalFeePerRun: 20, LastSeenTick: 100},
	}); err != nil {
		t.Fatal(err)
	}
	cov, err = src.Coverage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cov.FacilityOnlyCovered != 1 {
		t.Errorf("FacilityOnlyCovered = %d, want 1 after adding a production row", cov.FacilityOnlyCovered)
	}
	if cov.Stations != 1 {
		t.Errorf("Stations = %d, want 1 (only the production station counts)", cov.Stations)
	}
}

// Finding C: Recipes() must not hand back its cache by reference. The Source
// contract is that callers own what they get back; the cache exists purely to
// save the DB round-trip within one plan, not to be aliased out.
func TestCraftbrainSource_Recipes_ReturnsIndependentCopy(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := kb.StoreRecipes(ctx, []knowledge.RecipeDef{
		{ID: "refine_steel", Name: "Refine Steel"},
	}); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	first, err := src.Recipes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Mutate the caller's copy; this must not reach the cache or a later call.
	delete(first, "refine_steel")
	first["bogus"] = knowledge.RecipeDef{ID: "bogus"}

	second, err := src.Recipes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := second["refine_steel"]; !ok {
		t.Error("second Recipes() call missing refine_steel; first caller's mutation leaked into the cache")
	}
	if _, ok := second["bogus"]; ok {
		t.Error("second Recipes() call has 'bogus'; first caller's mutation leaked into the cache")
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
