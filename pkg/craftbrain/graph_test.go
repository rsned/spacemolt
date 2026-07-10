package craftbrain

import (
	"slices"
	"testing"
)

func TestProducerIndex(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("smelt_a", "alloy", 1, 1, false, map[string]int{"ore": 2})
	f.addRecipe("alt_a", "alloy", 3, 5, true, map[string]int{"ore": 4})
	prod := producerIndex(f.recipes)
	got := prod["alloy"]
	if len(got) != 2 {
		t.Fatalf("want 2 producers of alloy, got %d", len(got))
	}
	// Deterministic: sorted by recipe ID.
	if got[0].ID != "alt_a" || got[1].ID != "smelt_a" {
		t.Errorf("producers not sorted by id: %s, %s", got[0].ID, got[1].ID)
	}
	if len(prod["ore"]) != 0 {
		t.Errorf("ore is raw; want no producers, got %d", len(prod["ore"]))
	}
}

// widget <- 2x gadget <- 3x ore, and widget also takes ore directly.
// Order must place widget before gadget before ore.
func TestTopoOrder_ConsumersBeforeProducers(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_widget", "widget", 1, 1, false, map[string]int{"gadget": 2, "ore": 1})
	f.addRecipe("make_gadget", "gadget", 1, 1, false, map[string]int{"ore": 3})
	order, dropped := topoOrder("widget", producerIndex(f.recipes))
	if len(dropped) != 0 {
		t.Fatalf("unexpected dropped edges: %v", dropped)
	}
	iw := slices.Index(order, "widget")
	ig := slices.Index(order, "gadget")
	io := slices.Index(order, "ore")
	if iw < 0 || ig < 0 || io < 0 {
		t.Fatalf("missing items in order %v", order)
	}
	if iw >= ig || ig >= io {
		t.Errorf("want widget < gadget < ore, got order %v", order)
	}
}

// Only items reachable from the target appear.
func TestTopoOrder_OnlyReachable(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_widget", "widget", 1, 1, false, map[string]int{"ore": 1})
	f.addRecipe("make_unrelated", "unrelated", 1, 1, false, map[string]int{"junk": 1})
	order, _ := topoOrder("widget", producerIndex(f.recipes))
	if slices.Contains(order, "unrelated") || slices.Contains(order, "junk") {
		t.Errorf("unreachable items leaked into order: %v", order)
	}
}

// refine: scrap -> plate ; recycle: plate -> scrap. Must terminate and record
// the broken edge rather than hang or fail.
func TestTopoOrder_BreaksCycle(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("refine", "plate", 1, 1, false, map[string]int{"scrap": 2})
	f.addRecipe("recycle", "scrap", 1, 1, false, map[string]int{"plate": 1})
	order, dropped := topoOrder("plate", producerIndex(f.recipes))
	if len(dropped) == 0 {
		t.Fatal("expected a dropped edge to break the cycle")
	}
	if !slices.Contains(order, "plate") || !slices.Contains(order, "scrap") {
		t.Errorf("both items must still appear, got %v", order)
	}
}
