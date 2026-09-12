package worker

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// Why a plain `craft` exists alongside craft_node.
//
// craft_node is the crafting-CHAIN executor: it takes RECIPE NUM_OUTPUTS
// STATION FACILITY, resolves a facility, and can travel. That is the wrong
// shape for the iron-to-steel conversion, which is a hand-craft performed in
// place by an agent already sitting on its own ore.
//
// The driver (2026-09-11): drone marketbots are accumulating iron ore toward
// the 100,000-per-item-type storage cap -- marketbot_010 went 46,965 -> 68,396
// in one day (+21k/day) and the `overmind` agent is at 99,887, effectively
// capped. refine_steel is 5 iron_ore -> 2 steel_plate, so converting both
// frees 60% of the space AND moves the remainder into a different item type
// with its own separate cap. Steel plate is also the common component, so the
// output is worth more to the fleet than the ore.
func TestDispatchCraftQueuesTheRecipe(t *testing.T) {
	f := &craftFakeClient{fakeClient: &fakeClient{state: &game.State{}}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)

	if err := d.Run(context.Background(), []string{"craft", "refine_steel", "1000"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(f.craftQuantityCalls) != 1 {
		t.Fatalf("CraftWithQuantity calls = %d, want 1 (%+v)", len(f.craftQuantityCalls), f.craftQuantityCalls)
	}
	got := f.craftQuantityCalls[0]
	if got.recipeID != "refine_steel" || got.quantity != 1000 {
		t.Fatalf("call = %+v, want refine_steel x1000", got)
	}
}

// quantity is the number of OUTPUT items wanted (the server rounds up to whole
// runs), not the number of runs and not the input count. Getting this backwards
// would consume 5x the intended ore, so it is pinned by a test.
func TestDispatchCraftQuantityIsOutputItems(t *testing.T) {
	f := &craftFakeClient{fakeClient: &fakeClient{state: &game.State{}}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)

	// 1000 steel_plate out of a 5:2 recipe = 500 runs = 2500 iron_ore in.
	if err := d.Run(context.Background(), []string{"craft", "refine_steel", "1000"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := f.craftQuantityCalls[0].quantity; got != 1000 {
		t.Fatalf("quantity = %d, want the requested OUTPUT count 1000", got)
	}
}

// A bad or missing quantity must fail loudly rather than silently crafting
// some default amount of an irreversible conversion.
func TestDispatchCraftRejectsBadArgs(t *testing.T) {
	for _, tc := range [][]string{
		{"craft"},
		{"craft", "refine_steel"},
		{"craft", "refine_steel", "zero"},
		{"craft", "refine_steel", "0"},
		{"craft", "refine_steel", "-5"},
	} {
		f := &craftFakeClient{fakeClient: &fakeClient{state: &game.State{}}}
		d := NewWorkerDispatch(f, nil, nil, io.Discard)
		err := d.Run(context.Background(), tc)
		if err == nil {
			t.Errorf("Run(%v) must error, got nil", tc)
		}
		if len(f.craftQuantityCalls) != 0 {
			t.Errorf("Run(%v) must not craft anything, got %+v", tc, f.craftQuantityCalls)
		}
	}
}

// The usage string has to name the argument order, because craft and
// craft_node take different ones and confusing them is easy.
func TestDispatchCraftUsageNamesTheArgs(t *testing.T) {
	f := &craftFakeClient{fakeClient: &fakeClient{state: &game.State{}}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	err := d.Run(context.Background(), []string{"craft"})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "RECIPE") || !strings.Contains(err.Error(), "QUANTITY") {
		t.Errorf("usage %q should name RECIPE and QUANTITY", err.Error())
	}
}
