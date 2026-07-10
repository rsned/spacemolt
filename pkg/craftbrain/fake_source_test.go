package craftbrain

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// fakeSource is an in-memory Source. Every unit test in this package builds
// one; no test opens a database or contacts the server.
type fakeSource struct {
	recipes    map[string]knowledge.RecipeDef
	facilities map[string][]Facility        // recipe_id -> sites
	onHand     map[string][]Holding         // item_id -> holdings
	buyable    map[string][]finditem.Result // item_id -> sellers
	systems    map[string]string            // station_id -> system_id
	jumps      map[string]int               // system_id -> jumps from origin
	coverage   Coverage
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		recipes:    map[string]knowledge.RecipeDef{},
		facilities: map[string][]Facility{},
		onHand:     map[string][]Holding{},
		buyable:    map[string][]finditem.Result{},
		systems:    map[string]string{},
		jumps:      map[string]int{},
	}
}

// addRecipe registers a recipe producing outQty of outItem per run from ins.
func (f *fakeSource) addRecipe(id, outItem string, outQty int, craftTime float64, facilityOnly bool, ins map[string]int) {
	inputs := make([]knowledge.RecipeIngredient, 0, len(ins))
	for item, q := range ins {
		inputs = append(inputs, knowledge.RecipeIngredient{ItemID: item, Quantity: q})
	}
	f.recipes[id] = knowledge.RecipeDef{
		ID:           id,
		Name:         id,
		CraftingTime: craftTime,
		FacilityOnly: facilityOnly,
		Inputs:       inputs,
		Outputs:      []knowledge.RecipeProduct{{ItemID: outItem, Quantity: outQty}},
	}
}

func (f *fakeSource) Recipes(context.Context) (map[string]knowledge.RecipeDef, error) {
	return f.recipes, nil
}

func (f *fakeSource) Facilities(_ context.Context, recipeID string) ([]Facility, error) {
	return f.facilities[recipeID], nil
}

func (f *fakeSource) OnHand(_ context.Context, itemID string) ([]Holding, error) {
	return f.onHand[itemID], nil
}

func (f *fakeSource) Buyable(_ context.Context, itemID string, _ int) ([]finditem.Result, error) {
	return f.buyable[itemID], nil
}

func (f *fakeSource) SystemOf(_ context.Context, stationID string) (string, error) {
	return f.systems[stationID], nil
}

func (f *fakeSource) Jumps(_ context.Context, _ string, toSystems []string) (map[string]int, error) {
	out := make(map[string]int, len(toSystems))
	for _, s := range toSystems {
		out[s] = f.jumps[s]
	}
	return out, nil
}

func (f *fakeSource) Coverage(context.Context) (Coverage, error) { return f.coverage, nil }

// fresh returns a CapturedAt inside the default MaxStockAge window.
//
//nolint:unused // scaffolding consumed by Task 4's DAG-expansion tests
func fresh() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }

// testNow is the deterministic clock all tests pass via Options.Now.
//
//nolint:unused // scaffolding consumed by Task 4's DAG-expansion tests
func testNow() time.Time { return time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC) }

var _ Source = (*fakeSource)(nil)
