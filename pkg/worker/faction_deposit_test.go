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

// faction_deposit_all is the Cloverfield dumping ground's second half: leftover
// cargo the haulers gift to miningbot_0000 lands in its own storage, and this
// sweep moves every item across to faction storage -- except energy crystals,
// which are reserved for the drone project and are never moved for it.
func TestFactionDepositAllSkipsReserved(t *testing.T) {
	c := &depositFake{docked: "cj", held: map[string]int{"coolant_fluid": 400, "shield_matrix": 29, "energy_crystal": 40}}
	n, err := FactionDepositAll(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(c.deposits) != 2 {
		t.Fatalf("deposited %d (%+v), want coolant_fluid and shield_matrix", n, c.deposits)
	}
	for _, d := range c.deposits {
		if d["item_id"] == "energy_crystal" {
			t.Error("energy crystals moved")
		}
		if d["source"] != "storage" {
			t.Errorf("source = %v, want storage", d["source"])
		}
	}
}

func TestFactionDepositAllUndockedIsNoop(t *testing.T) {
	c := &depositFake{held: map[string]int{"coolant_fluid": 400}}
	if n, err := FactionDepositAll(context.Background(), c); err != nil || n != 0 || len(c.deposits) != 0 {
		t.Errorf("undocked = %d, %v, %v", n, err, c.deposits)
	}
}

func TestFactionDepositAllIsSupported(t *testing.T) {
	if !supported["faction_deposit_all"] {
		t.Error(`"faction_deposit_all" missing from the supported command set`)
	}
}
