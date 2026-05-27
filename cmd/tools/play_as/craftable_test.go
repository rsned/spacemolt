package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/craftplan"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// fixtureSource is a craftplan.Source backed by the captured JSON fixtures
// in testdata/. Exercises the same code paths the production adapter does
// without needing a live game client or SQLite DB.
type fixtureSource struct {
	recipes map[string]serverapi.Recipe
	storage map[string]int
}

func loadFixtureSource(t *testing.T) *fixtureSource {
	t.Helper()
	rawR, err := os.ReadFile("testdata/get_recipes.json")
	if err != nil {
		t.Fatalf("read get_recipes fixture: %v", err)
	}
	var rResp serverapi.GetRecipesResponse
	if err := json.Unmarshal(rawR, &rResp); err != nil {
		t.Fatalf("decode get_recipes fixture: %v", err)
	}
	rawS, err := os.ReadFile("testdata/view_storage.json")
	if err != nil {
		t.Fatalf("read view_storage fixture: %v", err)
	}
	var sResp serverapi.ViewStorageResponse
	if err := json.Unmarshal(rawS, &sResp); err != nil {
		t.Fatalf("decode view_storage fixture: %v", err)
	}
	storage := map[string]int{}
	for _, it := range sResp.Items {
		storage[it.ItemID] += int(it.Quantity)
	}
	return &fixtureSource{recipes: rResp.Recipes, storage: storage}
}

func (f *fixtureSource) Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error) {
	return f.recipes, nil
}
func (f *fixtureSource) Inventory(ctx context.Context, includeFaction bool) (craftplan.Inventory, error) {
	return craftplan.Inventory{Cargo: map[string]int{}, Storage: f.storage}, nil
}
func (f *fixtureSource) Skills(ctx context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}
func (f *fixtureSource) CurrentStationID(ctx context.Context) (string, error) {
	return "market_prime", nil
}
func (f *fixtureSource) IllegalAt(ctx context.Context, stationID string) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (f *fixtureSource) BOM(ctx context.Context, recipeIDs []string) (map[string][]craftplan.BOMRow, error) {
	return map[string][]craftplan.BOMRow{}, nil
}

func TestCraftable_FromFixtures(t *testing.T) {
	src := loadFixtureSource(t)
	eng := craftplan.New(src)
	rows, err := eng.Craftable(context.Background(), craftplan.CraftableOpts{})
	if err != nil {
		t.Fatalf("Craftable: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Recipe.ID != "alloy_titanium_ingot" {
		t.Errorf("recipe id = %s, want alloy_titanium_ingot", r.Recipe.ID)
	}
	// can_make = min(450/3, 380/2) = min(150, 190) = 150
	if r.CanMake != 150 {
		t.Errorf("can_make = %d, want 150", r.CanMake)
	}
}
