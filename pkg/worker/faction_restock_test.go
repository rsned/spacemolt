package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// restockFake answers view_faction_storage with a fixed stock and records
// faction sell orders.
type restockFake struct {
	game.GameClient
	docked string
	held   map[string]int
	orders []string
}

func (f *restockFake) GetState() *game.State {
	s := &game.State{}
	s.Player.DockedAtBase = f.docked
	return s
}

func (f *restockFake) ViewFactionStorage(context.Context) error { return nil }

func (f *restockFake) GetRawJSON(key string) []byte {
	if key != "faction_storage" {
		return nil
	}
	type item struct {
		ItemID   string `json:"item_id"`
		Quantity int    `json:"quantity"`
	}
	items := make([]item, 0, len(f.held))
	for id, q := range f.held {
		items = append(items, item{id, q})
	}
	raw, _ := json.Marshal(map[string]any{"base_id": f.docked, "items": items})
	return raw
}

func (f *restockFake) FactionCreateSellOrder(_ context.Context, item string, price float64, qty int) error {
	f.orders = append(f.orders, fmt.Sprintf("%s@%.0fx%d", item, price, qty))
	return nil
}

// Moonshine hauled into faction storage at Conjunction Junction is listed at
// the operator's price (1,500 minimum, 2026-11-01); a same-price order
// consolidates server-side, so each run just lists what has arrived.
func TestFactionRestockListsArrivedStock(t *testing.T) {
	c := &restockFake{docked: "cj", held: map[string]int{"pirate_moonshine": 600, "void_dust": 3}}
	n, err := FactionRestock(context.Background(), c, "pirate_moonshine", 1500)
	if err != nil || n != 600 {
		t.Fatalf("FactionRestock = %d, %v; want 600", n, err)
	}
	if len(c.orders) != 1 || c.orders[0] != "pirate_moonshine@1500x600" {
		t.Errorf("orders = %v", c.orders)
	}
}

func TestFactionRestockNothingHeldIsNoop(t *testing.T) {
	c := &restockFake{docked: "cj", held: map[string]int{"void_dust": 3}}
	if n, err := FactionRestock(context.Background(), c, "pirate_moonshine", 1500); err != nil || n != 0 || len(c.orders) != 0 {
		t.Errorf("= %d, %v, %v", n, err, c.orders)
	}
}

func TestFactionRestockUndockedIsNoop(t *testing.T) {
	c := &restockFake{held: map[string]int{"pirate_moonshine": 600}}
	if n, err := FactionRestock(context.Background(), c, "pirate_moonshine", 1500); err != nil || n != 0 || len(c.orders) != 0 {
		t.Errorf("= %d, %v, %v", n, err, c.orders)
	}
}

func TestFactionRestockIsSupported(t *testing.T) {
	if !supported["faction_restock"] {
		t.Error(`"faction_restock" missing from the supported command set`)
	}
}
