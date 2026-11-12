package worker

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/knowledge/knowledgetest"
)

const stockBase = "cj_base"

// craftStockFake serves faction storage and the craft queue, and records
// crafts.
type craftStockFake struct {
	*fakeClient
	crafted []int
}

func (f *craftStockFake) ViewFactionStorage(context.Context) error { return nil }
func (f *craftStockFake) CraftWithPreset(_ context.Context, _ string, qty int, _ string) error {
	f.crafted = append(f.crafted, qty)
	return nil
}
func (f *craftStockFake) CraftWithQuantity(_ context.Context, _ string, qty int) error {
	f.crafted = append(f.crafted, qty)
	return nil
}

func newCraftStock(t *testing.T, held int, queue string) (*WorkerDispatch, *craftStockFake, *bytes.Buffer) {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: knowledgetest.Path(t)})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	if err := kb.StoreRecipes(context.Background(), []knowledge.RecipeDef{{
		ID: "process_copper_wiring", Name: "Process Copper Wiring",
		Inputs:  []knowledge.RecipeIngredient{{ItemID: "copper_ore", Quantity: 4}},
		Outputs: []knowledge.RecipeProduct{{ItemID: "copper_wiring", Quantity: 2}},
	}}); err != nil {
		t.Fatalf("StoreRecipes: %v", err)
	}
	st := &game.State{Doc: true}
	st.Player.DockedAtBase = stockBase
	f := &craftStockFake{fakeClient: &fakeClient{state: st, raw: map[string][]byte{
		"faction_storage": []byte(`{"base_id":"` + stockBase + `","items":[{"item_id":"copper_wiring","quantity":` + strconv.Itoa(held) + `}]}`),
		"_last":           []byte(queue),
	}}}
	var out bytes.Buffer
	return &WorkerDispatch{Client: f, KB: kb, Out: &out}, f, &out
}

const emptyQueue = `{"action":"queue","jobs":[]}`

// Below target with nothing queued: crafts exactly the shortfall.
func TestCraftStockCraftsShortfall(t *testing.T) {
	d, f, _ := newCraftStock(t, 1200, emptyQueue)
	if err := d.CraftStock(context.Background(), "process_copper_wiring", "copper_wiring", 5000, "prefer_own"); err != nil {
		t.Fatal(err)
	}
	if len(f.crafted) != 1 || f.crafted[0] != 3800 {
		t.Fatalf("crafted %v, want [3800]", f.crafted)
	}
}

// Output still queued at this base counts toward the target; a job at
// another base does not.
func TestCraftStockCountsQueuedOutput(t *testing.T) {
	q := `{"action":"queue","jobs":[
		{"recipe":"Process Copper Wiring","runs_remaining":1000,"base_id":"` + stockBase + `"},
		{"recipe":"Process Copper Wiring","runs_remaining":5000,"base_id":"elsewhere"},
		{"recipe":"Purify Argon","runs_remaining":9000,"base_id":"` + stockBase + `"}]}`
	d, f, _ := newCraftStock(t, 1200, q)
	if err := d.CraftStock(context.Background(), "process_copper_wiring", "copper_wiring", 5000, ""); err != nil {
		t.Fatal(err)
	}
	// 5000 - 1200 held - 1000 runs x 2 queued = 1800.
	if len(f.crafted) != 1 || f.crafted[0] != 1800 {
		t.Fatalf("crafted %v, want [1800]", f.crafted)
	}
}

// At or above target: nothing is crafted.
func TestCraftStockAtTargetCraftsNothing(t *testing.T) {
	d, f, out := newCraftStock(t, 6000, emptyQueue)
	if err := d.CraftStock(context.Background(), "process_copper_wiring", "copper_wiring", 5000, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.crafted) != 0 || !strings.Contains(out.String(), "nothing to craft") {
		t.Fatalf("crafted %v; out %q", f.crafted, out.String())
	}
}

// An unreadable queue crafts nothing rather than risk over-ordering.
func TestCraftStockUnreadableQueueCraftsNothing(t *testing.T) {
	d, f, _ := newCraftStock(t, 0, `{"action":"craft_started"}`)
	if err := d.CraftStock(context.Background(), "process_copper_wiring", "copper_wiring", 5000, ""); err == nil {
		t.Fatal("want an error for a non-queue reply")
	}
	if len(f.crafted) != 0 {
		t.Fatalf("crafted %v, want none", f.crafted)
	}
}
