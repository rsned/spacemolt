package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// depositFake answers view_storage with a fixed stock and records every
// faction deposit payload.
type depositFake struct {
	game.GameClient
	docked   string
	held     map[string]int
	deposits []map[string]any
}

func (f *depositFake) GetState() *game.State {
	s := &game.State{}
	s.Player.DockedAtBase = f.docked
	return s
}

func (f *depositFake) ViewStorage(context.Context) error { return nil }

func (f *depositFake) GetRawJSON(key string) []byte {
	if key != "storage" {
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

func (f *depositFake) FactionDepositItemsPayload(_ context.Context, payload map[string]any) error {
	f.deposits = append(f.deposits, payload)
	return nil
}

// Goods gifted to a faction officer land in its own storage, and crafting draws
// a recipe's inputs from one place. With alloy in miningbot_0000's storage and
// liquid hydrogen and steel in faction storage, build_fuel_tank refused every
// hour (2026-10-30). The sweep moves the whole stock of a named item across.
func TestFactionDepositMovesWholeStock(t *testing.T) {
	c := &depositFake{docked: "cj", held: map[string]int{"titanium_alloy": 612, "energy_crystal": 40}}

	if err := FactionDeposit(context.Background(), c, "titanium_alloy"); err != nil {
		t.Fatal(err)
	}
	if len(c.deposits) != 1 {
		t.Fatalf("deposits = %+v, want one", c.deposits)
	}
	d := c.deposits[0]
	if d["item_id"] != "titanium_alloy" || d["quantity"] != 612 || d["source"] != "storage" {
		t.Errorf("payload = %+v, want all 612 titanium_alloy from storage", d)
	}
}

// Nothing held is the normal case between deliveries, not an error.
func TestFactionDepositNothingHeld(t *testing.T) {
	c := &depositFake{docked: "cj", held: map[string]int{}}

	if err := FactionDeposit(context.Background(), c, "ceramite_plating"); err != nil {
		t.Fatal(err)
	}
	if len(c.deposits) != 0 {
		t.Errorf("deposited %+v with nothing held", c.deposits)
	}
}

// Storage is per station, so an undocked agent has nothing to move.
func TestFactionDepositUndocked(t *testing.T) {
	c := &depositFake{held: map[string]int{"titanium_alloy": 5}}

	if err := FactionDeposit(context.Background(), c, "titanium_alloy"); err != nil {
		t.Fatal(err)
	}
	if len(c.deposits) != 0 {
		t.Errorf("deposited %+v while undocked", c.deposits)
	}
}

// The verb is reachable from a schedule entry.
func TestFactionDepositIsSupported(t *testing.T) {
	if !supported["faction_deposit"] {
		t.Error(`"faction_deposit" missing from the supported command set`)
	}
}
