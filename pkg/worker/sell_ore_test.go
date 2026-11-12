package worker

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/knowledge/knowledgetest"
)

func newSellOreKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: knowledgetest.Path(t)})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	if err := kb.StoreItems(context.Background(), []knowledge.CatalogItem{
		{ID: "copper_ore", Category: "ore", Rarity: "common"},
		{ID: "nickel_ore", Category: "ore", Rarity: "uncommon"},
		{ID: "platinum_ore", Category: "ore", Rarity: "rare"},
		{ID: "void_essence", Category: "ore", Rarity: "exotic"},
		{ID: "steel_plate", Category: "refined", Rarity: "common"},
	}); err != nil {
		t.Fatalf("StoreItems: %v", err)
	}
	return kb
}

func sellOreClient(cargo ...game.CargoItem) *fakeClient {
	f := &fakeClient{state: &game.State{Doc: true}}
	f.state.Ship.Cargo = cargo
	return f
}

// Common and uncommon ores are sold; rare-and-up ores, non-ores and items the
// catalog does not know stay in the hold for deposit_all.
func TestSellOreSellsOnlyNonRareOre(t *testing.T) {
	f := sellOreClient(
		game.CargoItem{ItemID: "copper_ore", Quantity: 90},
		game.CargoItem{ItemID: "nickel_ore", Quantity: 10},
		game.CargoItem{ItemID: "platinum_ore", Quantity: 3},
		game.CargoItem{ItemID: "void_essence", Quantity: 1},
		game.CargoItem{ItemID: "steel_plate", Quantity: 5},
		game.CargoItem{ItemID: "mystery_ore", Quantity: 2},
	)
	var out bytes.Buffer
	n, err := SellOre(context.Background(), f, newSellOreKB(t), &out)
	if err != nil {
		t.Fatalf("SellOre: %v", err)
	}
	var sold []string
	for _, c := range f.calls {
		if s, ok := strings.CutPrefix(c, "sell:"); ok {
			sold = append(sold, s)
		}
	}
	if !slices.Equal(sold, []string{"copper_ore", "nickel_ore"}) || n != 2 {
		t.Fatalf("sold %v (n=%d), want copper_ore and nickel_ore only", sold, n)
	}
}

// A refused sale (no buyer at this station) is logged and the rest still
// sells; the pass goes on to deposit_all.
func TestSellOreKeepsGoingWhenASaleFails(t *testing.T) {
	f := sellOreClient(game.CargoItem{ItemID: "copper_ore", Quantity: 90})
	f.sellErr = errors.New("no buyers for copper_ore")
	var out bytes.Buffer
	n, err := SellOre(context.Background(), f, newSellOreKB(t), &out)
	if err != nil || n != 0 {
		t.Fatalf("SellOre = %d, %v; want 0, nil", n, err)
	}
	if !strings.Contains(out.String(), "no buyers") {
		t.Errorf("failure not logged:\n%s", out.String())
	}
}

func TestSellOreNeedsDock(t *testing.T) {
	f := sellOreClient(game.CargoItem{ItemID: "copper_ore", Quantity: 90})
	f.state.Doc = false
	if _, err := SellOre(context.Background(), f, newSellOreKB(t), &bytes.Buffer{}); err == nil {
		t.Fatal("want an error when not docked")
	}
	if slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "sell:") }) {
		t.Fatalf("sold while undocked: %v", f.calls)
	}
}
