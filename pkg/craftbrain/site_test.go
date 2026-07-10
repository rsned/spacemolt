package craftbrain

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestChooseSiting_PrefersHandUnderBudget(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("smelt", "alloy", 1, 2.0, false, map[string]int{"ore": 1})
	f.facilities["smelt"] = []Facility{{StationID: "hub", FacilityID: "f1", RentalFeePerRun: 10, TicksPerRun: 0.5, OutputPerRun: 1}}
	e := New(f)
	opts := DefaultOptions()

	s, ok, err := e.chooseSiting(context.Background(), "alloy", 10, []knowledge.RecipeDef{f.recipes["smelt"]}, 0, opts)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.facility != nil {
		t.Errorf("want hand-craft under budget, got facility %s", s.facility.FacilityID)
	}
	if s.runs != 10 {
		t.Errorf("runs = %d, want 10", s.runs)
	}
	if s.ticks != 20 {
		t.Errorf("ticks = %v, want 20 (10 runs x 2.0)", s.ticks)
	}
	if s.status != StatusOK {
		t.Errorf("status = %q, want ok", s.status)
	}
}

func TestChooseSiting_SwitchesToFacilityOverBudget(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("smelt", "alloy", 1, 2.0, false, map[string]int{"ore": 1})
	f.facilities["smelt"] = []Facility{
		{StationID: "hub", FacilityID: "pricey", RentalFeePerRun: 100, TicksPerRun: 0.1, OutputPerRun: 1},
		{StationID: "far", FacilityID: "cheap", RentalFeePerRun: 5, TicksPerRun: 0.5, OutputPerRun: 1},
	}
	e := New(f)
	opts := DefaultOptions()
	opts.MaxHandTicks = 10 // 500 units x 2.0 ticks = 1000, way over

	s, ok, err := e.chooseSiting(context.Background(), "alloy", 500, []knowledge.RecipeDef{f.recipes["smelt"]}, 0, opts)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.facility == nil {
		t.Fatal("want a facility over budget")
	}
	if s.facility.FacilityID != "cheap" {
		t.Errorf("want cheapest by fee*runs, got %s", s.facility.FacilityID)
	}
	if s.feeTotal != 5*500 {
		t.Errorf("feeTotal = %d, want 2500", s.feeTotal)
	}
}

// Budget blown but nothing to escape to: hand-craft anyway, tagged slow.
func TestChooseSiting_OverBudgetNoFacilityIsSlowNotBlocked(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("smelt", "alloy", 1, 2.0, false, map[string]int{"ore": 1})
	e := New(f)
	opts := DefaultOptions()
	opts.MaxHandTicks = 1

	s, ok, err := e.chooseSiting(context.Background(), "alloy", 500, []knowledge.RecipeDef{f.recipes["smelt"]}, 0, opts)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.facility != nil {
		t.Error("no facility exists; must hand-craft")
	}
	if s.status != StatusSlow {
		t.Errorf("status = %q, want %q", s.status, StatusSlow)
	}
}

// facility_only with no known facility: not sited, so the caller falls back.
func TestChooseSiting_FacilityOnlyNoFacilityNotSited(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("grow", "matrix", 1, 21, true, map[string]int{"gas": 1})
	e := New(f)

	_, ok, err := e.chooseSiting(context.Background(), "matrix", 3, []knowledge.RecipeDef{f.recipes["grow"]}, 0, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("facility_only with no facility must not be sited")
	}
}

// runs = ceil(demand / output_per_run) at the facility's own output rate.
func TestChooseSiting_FacilityRunsUseFacilityOutput(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("press", "plate", 1, 100, true, map[string]int{"ore": 1})
	f.facilities["press"] = []Facility{{StationID: "hub", FacilityID: "f1", RentalFeePerRun: 2, TicksPerRun: 1, OutputPerRun: 4}}
	e := New(f)

	s, ok, err := e.chooseSiting(context.Background(), "plate", 9, []knowledge.RecipeDef{f.recipes["press"]}, 0, DefaultOptions())
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.runs != 3 { // ceil(9/4)
		t.Errorf("runs = %d, want 3", s.runs)
	}
}

func TestCheapestFacility_TieBreakOnTicksThenID(t *testing.T) {
	fs := []Facility{
		{FacilityID: "b", RentalFeePerRun: 10, TicksPerRun: 2, OutputPerRun: 1},
		{FacilityID: "a", RentalFeePerRun: 10, TicksPerRun: 1, OutputPerRun: 1},
		{FacilityID: "c", RentalFeePerRun: 10, TicksPerRun: 1, OutputPerRun: 1},
	}
	got := cheapestFacility(fs, 3)
	if got.FacilityID != "a" {
		t.Errorf("got %s, want a (lowest ticks, then id)", got.FacilityID)
	}
}
