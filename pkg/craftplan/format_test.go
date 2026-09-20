package craftplan

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// updateGoldens regenerates the golden files when set. Run:
//
//	go test ./pkg/craftplan/ -run TestFormat -update
//
// then commit the updated files.
var updateGoldens = flag.Bool("update", false, "update golden files in testdata/")

func TestFormatCraftableCompact(t *testing.T) {
	rows := []CraftableRow{
		{
			Recipe:         recipe("alloy_titanium_ingot", "Titanium Alloy Ingot", "Refining", nil, []serverapi.RecipeItem{item("titanium_alloy", 2)}, nil),
			CanMake:        47,
			OutputItemID:   "titanium_alloy",
			OutputQuantity: 2,
			Depth:          1,
		},
		{
			Recipe:         recipe("assemble_advanced_repair_kit", "Adv Repair Kit", "Consumables", nil, []serverapi.RecipeItem{item("advanced_repair_kit", 1)}, nil),
			CanMake:        31,
			OutputItemID:   "advanced_repair_kit",
			OutputQuantity: 1,
			Depth:          1,
		},
	}
	rows[0].Recipe.CraftingTime = 6
	rows[1].Recipe.CraftingTime = 12

	got := FormatCraftableCompact(rows, FormatCraftableOpts{StationID: "market_prime_exchange", Reachable: false})
	checkGolden(t, "golden_craftable_compact.txt", got)
}

func TestFormatPlanDirectShort(t *testing.T) {
	res := &PlanResult{
		Recipe: recipe("assemble_advanced_repair_kit", "Adv Repair Kit", "Consumables",
			[]serverapi.RecipeItem{item("circuit_board", 1), item("flex_polymer", 3), item("titanium_alloy", 3)},
			[]serverapi.RecipeItem{item("advanced_repair_kit", 1)},
			nil),
		Quantity:  5, // 1 unit/run → 5 runs
		Runs:      5,
		StationID: "market_prime",
		Inputs: []PlanInputRow{
			{ItemID: "circuit_board", Need: 5, HaveCargo: 0, HaveStorage: 3, Short: 2},
			{ItemID: "flex_polymer", Need: 15, HaveCargo: 0, HaveStorage: 20},
			{ItemID: "titanium_alloy", Need: 15, HaveCargo: 2, HaveStorage: 18},
		},
		Ready: false,
	}
	res.Recipe.CraftingTime = 12

	got := FormatPlan(res)
	checkGolden(t, "golden_plan_direct_short.txt", got)
}

func TestFormatPlanDirectReady(t *testing.T) {
	res := &PlanResult{
		Recipe: recipe("alloy_titanium_ingot", "Titanium Alloy Ingot", "Refining",
			[]serverapi.RecipeItem{item("iron_ore", 3), item("titanium_ore", 2)},
			[]serverapi.RecipeItem{item("titanium_alloy", 2)}, nil),
		Quantity: 10, // 2 units/run → 5 runs
		Runs:     5,
		Inputs: []PlanInputRow{
			{ItemID: "iron_ore", Need: 15, HaveCargo: 12, HaveStorage: 450},
			{ItemID: "titanium_ore", Need: 10, HaveStorage: 380},
		},
		Ready: true,
	}
	res.Recipe.CraftingTime = 6

	got := FormatPlan(res)
	checkGolden(t, "golden_plan_direct_ready.txt", got)
}

// checkGolden writes the actual output to testdata/name if -update is set,
// otherwise compares against the stored fixture.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to regenerate): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch.\nGOT:\n%s\nWANT:\n%s", name, got, string(want))
	}
}

// The catalog offers several routes to common items and the right one depends
// on stock, so an operator should not have to remember recipe ids to find the
// others. FormatPlan lists what resolution passed over.
func TestFormatPlanListsAlternatives(t *testing.T) {
	res := &PlanResult{
		Recipe:   serverapi.Recipe{ID: "craft_fuel_cell", Category: "Consumables"},
		Quantity: 1000, Runs: 1000, Ready: true,
		Alternatives: []serverapi.Recipe{
			{ID: "catalyze_fuel_cells", Category: "Consumables", FacilityOnly: true,
				Outputs: []serverapi.RecipeItem{{ItemID: "fuel_cell", Quantity: 50}}},
			{ID: "biogas_fuel_synthesis", Category: "Refining",
				Outputs: []serverapi.RecipeItem{{ItemID: "fuel_cell", Quantity: 2}}},
		},
	}
	out := FormatPlan(res)
	for _, want := range []string{"catalyze_fuel_cells", "biogas_fuel_synthesis", "facility"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatPlanOmitsAlternativesWhenNone(t *testing.T) {
	res := &PlanResult{Recipe: serverapi.Recipe{ID: "craft_fuel_cell"}, Quantity: 1, Runs: 1, Ready: true}
	if strings.Contains(FormatPlan(res), "other recipes") {
		t.Error("alternatives section printed with no alternatives")
	}
}
