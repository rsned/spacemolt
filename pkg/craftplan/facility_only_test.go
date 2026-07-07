package craftplan

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// facilityRecipe is recipe() with FacilityOnly set.
func facilityRecipe(id, output string, facilityOnly bool) serverapi.Recipe {
	r := recipe(id, id, "Production",
		[]serverapi.RecipeItem{item("iron_ore", 1)},
		[]serverapi.RecipeItem{item(output, 1)},
		nil)
	r.FacilityOnly = facilityOnly
	return r
}

func TestFacilityOnlyNoAlternative(t *testing.T) {
	cases := []struct {
		name string
		r    serverapi.Recipe
		recs map[string]serverapi.Recipe
		want bool
	}{
		{
			name: "facility-only, sole recipe for output",
			r:    facilityRecipe("forge_ghost_rounds", "ghost_rounds", true),
			recs: map[string]serverapi.Recipe{
				"forge_ghost_rounds": facilityRecipe("forge_ghost_rounds", "ghost_rounds", true),
			},
			want: true,
		},
		{
			name: "facility-only, but a hand-craftable alternative exists for the same output",
			r:    facilityRecipe("forge_ghost_rounds", "ghost_rounds", true),
			recs: map[string]serverapi.Recipe{
				"forge_ghost_rounds":  facilityRecipe("forge_ghost_rounds", "ghost_rounds", true),
				"handload_cartridges": facilityRecipe("handload_cartridges", "ghost_rounds", false),
			},
			want: false,
		},
		{
			name: "facility-only, but the only alternative is also facility-only",
			r:    facilityRecipe("forge_ghost_rounds", "ghost_rounds", true),
			recs: map[string]serverapi.Recipe{
				"forge_ghost_rounds": facilityRecipe("forge_ghost_rounds", "ghost_rounds", true),
				"press_ghost_rounds": facilityRecipe("press_ghost_rounds", "ghost_rounds", true),
			},
			want: true,
		},
		{
			name: "not facility-only",
			r:    facilityRecipe("refine_steel", "steel_plate", false),
			recs: map[string]serverapi.Recipe{
				"refine_steel": facilityRecipe("refine_steel", "steel_plate", false),
			},
			want: false,
		},
		{
			name: "facility-only recipe with no outputs",
			r:    serverapi.Recipe{ID: "weird", FacilityOnly: true},
			recs: map[string]serverapi.Recipe{},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := facilityOnlyNoAlternative(c.r, c.recs); got != c.want {
				t.Fatalf("facilityOnlyNoAlternative = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFormatPlanFacilityOnlyNote(t *testing.T) {
	base := &PlanResult{
		Recipe:    recipe("forge_ghost_rounds", "Ghost Rounds", "Production", []serverapi.RecipeItem{item("hot_cell", 2)}, []serverapi.RecipeItem{item("ghost_rounds", 2)}, nil),
		Quantity:  2,
		Runs:      1,
		StationID: "voss_redoubt_station",
		Ready:     true,
	}

	withNote := *base
	withNote.FacilityOnlyNoAlt = true
	if out := FormatPlan(&withNote); !strings.Contains(out, "facility-only recipe") {
		t.Fatalf("expected facility-only note, got:\n%s", out)
	}

	without := *base
	without.FacilityOnlyNoAlt = false
	if out := FormatPlan(&without); strings.Contains(out, "facility-only recipe") {
		t.Fatalf("did not expect facility-only note, got:\n%s", out)
	}
}

// TestPlanSetsFacilityOnlyNoAlt proves the flag is wired end-to-end through
// Engine.Plan from the resolved recipe + catalog.
func TestPlanSetsFacilityOnlyNoAlt(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"forge_ghost_rounds": facilityRecipe("forge_ghost_rounds", "ghost_rounds", true),
		},
		inventory: Inventory{Storage: map[string]int{"iron_ore": 10}},
	}
	res, err := New(src).Plan(context.Background(), PlanOpts{ID: "ghost_rounds", Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.FacilityOnlyNoAlt {
		t.Fatalf("expected FacilityOnlyNoAlt=true for a sole facility-only recipe, got false")
	}
}
