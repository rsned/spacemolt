package craftbrain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// goldenPlan pins the end-to-end DAG for a multi-level target that exercises
// every stage: ceil-per-run, a shared intermediate, a facility siting, a
// stale remote holding with a haul leg, and a facility_only dead end.
//
// Regenerate with: go test ./pkg/craftbrain/ -run TestGoldenPlan -update
func TestGoldenPlan(t *testing.T) {
	f := newFakeSource()
	// sensor_array <- 2 optics + 1 matrix (facility_only, unbuyable -> BLOCKED)
	f.addRecipe("build_sensor_array", "sensor_array", 1, 4.0, false,
		map[string]int{"optics": 2, "matrix": 1})
	// optics <- 3 wire ; wire is 2-per-run so 6 wire needs 3 runs exactly
	f.addRecipe("build_optics", "optics", 1, 1.0, false, map[string]int{"wire": 3})
	f.addRecipe("draw_wire", "wire", 2, 0.5, true, map[string]int{"copper_ore": 1})
	f.addRecipe("grow_matrix", "matrix", 1, 21.0, true, map[string]int{"neon_gas": 1})

	// wire has a facility; matrix has none and is unbuyable.
	f.facilities["draw_wire"] = []Facility{
		{StationID: "confederacy_central_command", FacilityID: "cheap", RentalFeePerRun: 4, TicksPerRun: 0.05, OutputPerRun: 2},
		{StationID: "grand_exchange_station", FacilityID: "pricey", RentalFeePerRun: 90, TicksPerRun: 0.01, OutputPerRun: 2},
	}
	// A stale remote holding of copper_ore, two jumps out.
	f.onHand["copper_ore"] = []Holding{
		{Holder: "hauler-3", BaseID: "far_base", Qty: 2, CapturedAt: testNow().Add(-72 * time.Hour)},
	}
	f.systems["any_docked_station"] = "sol"
	f.systems["far_base"] = "beta"
	f.jumps["beta"] = 2
	f.coverage = Coverage{Stations: 30, FacilityOnlyCovered: 101, FacilityOnlyTotal: 317}

	opts := DefaultOptions()
	opts.Now = testNow()
	got, err := New(f).Plan(context.Background(), "sensor_array", 2, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	path := filepath.Join("testdata", "golden_plan.json")
	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, append(gotJSON, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (regenerate with UPDATE_GOLDEN=1): %v", err)
	}
	if string(gotJSON)+"\n" != string(want) {
		t.Errorf("plan DAG changed.\n--- got ---\n%s\n--- want ---\n%s", gotJSON, want)
	}
}

// Independent assertions on the same fixture, so a careless golden
// regeneration cannot silently bless a wrong plan.
//
// INTENTIONAL DUPLICATION — do not extract a shared fixture helper. A golden
// test asserts only "unchanged", never "correct": run UPDATE_GOLDEN=1 once
// against a broken engine and the wrong DAG is blessed forever. These
// assertions are the safety net, and a net woven from the same thread as the
// thing it catches is no net at all. If both tests shared a fixture builder, a
// bug in that builder would make them agree and the check would evaporate.
func TestGoldenPlan_Invariants(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("build_sensor_array", "sensor_array", 1, 4.0, false,
		map[string]int{"optics": 2, "matrix": 1})
	f.addRecipe("build_optics", "optics", 1, 1.0, false, map[string]int{"wire": 3})
	f.addRecipe("draw_wire", "wire", 2, 0.5, true, map[string]int{"copper_ore": 1})
	f.addRecipe("grow_matrix", "matrix", 1, 21.0, true, map[string]int{"neon_gas": 1})
	f.facilities["draw_wire"] = []Facility{
		{StationID: "confederacy_central_command", FacilityID: "cheap", RentalFeePerRun: 4, TicksPerRun: 0.05, OutputPerRun: 2},
		{StationID: "grand_exchange_station", FacilityID: "pricey", RentalFeePerRun: 90, TicksPerRun: 0.01, OutputPerRun: 2},
	}
	f.systems["any_docked_station"] = "sol"
	opts := DefaultOptions()
	opts.Now = testNow()
	p, err := New(f).Plan(context.Background(), "sensor_array", 2, opts)
	if err != nil {
		t.Fatal(err)
	}

	// 2 sensor arrays -> 4 optics -> 12 wire -> ceil(12/2)=6 runs of draw_wire.
	var wireRuns int
	var wireFacility string
	var matrixBlocked bool
	for _, n := range p.Nodes {
		if n.ItemID == "wire" && n.Kind == KindCraft {
			wireRuns, wireFacility = n.Runs, n.FacilityID
		}
		if n.ItemID == "matrix" && n.Kind == KindBlocked {
			matrixBlocked = true
		}
	}
	if wireRuns != 6 {
		t.Errorf("wire runs = %d, want 6", wireRuns)
	}
	if wireFacility != "cheap" {
		t.Errorf("wire sited at %q, want cheap (lowest fee x runs)", wireFacility)
	}
	if !matrixBlocked {
		t.Error("matrix is facility_only with no facility and no seller; want BLOCKED")
	}
}
