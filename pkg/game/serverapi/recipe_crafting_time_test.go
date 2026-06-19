package serverapi

import (
	"encoding/json"
	"testing"
)

// TestRecipeCraftingTimeFractional is a regression test for server v0.389.0
// which started sending fractional crafting_time values (e.g. 3.6).
// Previously CraftingTime was typed int, which caused a decode error:
//
//	json: cannot unmarshal number 3.6 into Go struct field Recipe.items.crafting_time of type int
func TestRecipeCraftingTimeFractional(t *testing.T) {
	raw := `{
		"id": "smelt_titanium",
		"name": "Smelt Titanium",
		"description": "Smelts raw titanium ore",
		"category": "Refining",
		"inputs":  [{"item_id": "titanium_ore", "quantity": 2}],
		"outputs": [{"item_id": "titanium_ingot", "quantity": 1}],
		"crafting_time": 3.6
	}`

	var r Recipe
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal error (crafting_time=3.6): %v", err)
	}
	if r.CraftingTime != 3.6 {
		t.Fatalf("CraftingTime: got %v, want 3.6", r.CraftingTime)
	}
}
