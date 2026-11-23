package worker

import (
	"context"
	"io"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// bankFake models one station's personal storage: deposits move cargo into
// it, view_storage reports it, gifts from storage take it away.
type bankFake struct {
	game.GameClient
	state    *game.State
	storage  map[string]int
	active   []byte // active_missions frame
	deposits []map[string]any
	gifts    []map[string]any
}

func (f *bankFake) GetState() *game.State             { return f.state }
func (f *bankFake) GetCargo(context.Context) error    { return nil }
func (f *bankFake) ViewStorage(context.Context) error { return nil }

func (f *bankFake) GetRawJSON(key string) []byte {
	if key == "active_missions" {
		return f.active
	}
	if key != "storage" {
		return nil
	}
	return storageFrame("here_base", f.storage)
}

func (f *bankFake) DepositItemsPayload(_ context.Context, p map[string]any) error {
	f.deposits = append(f.deposits, p)
	id, qty := p["item_id"].(string), p["quantity"].(int)
	if f.storage == nil {
		f.storage = map[string]int{}
	}
	f.storage[id] += qty
	return nil
}

func (f *bankFake) SendGift(_ context.Context, p map[string]any) error {
	f.gifts = append(f.gifts, p)
	delete(f.storage, p["item_id"].(string))
	return nil
}

func bankDockedAt(poi string, cargo map[string]float64) *game.State {
	st := &game.State{Doc: true, CurrentPOI: poi}
	for _, id := range slices.Sorted(maps.Keys(cargo)) {
		st.Ship.Cargo = append(st.Ship.Cargo, game.CargoItem{ItemID: id, Quantity: cargo[id]})
	}
	return st
}

// dropsKB is a MemoryKB that also answers the creature-drop query: drops are
// the items a kill record shows a creature dropping. Its catalog marks the
// shipboard consumables.
type dropsKB struct {
	*knowledge.MemoryKB
	drops map[string]bool
}

func (k dropsKB) SpeciesDroppingItem(_ context.Context, itemID string) ([]string, error) {
	if k.drops[itemID] {
		return []string{"drifter"}, nil
	}
	return nil, nil
}

func newDropsKB(t *testing.T) dropsKB {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	if err := kb.StoreItems(context.Background(), []knowledge.CatalogItem{
		{ID: "premium_fuel_cell", Category: "consumable"},
		{ID: "hull_patch", Category: "consumable"},
		{ID: "iron_ore", Category: "ore"},
	}); err != nil {
		t.Fatal(err)
	}
	return dropsKB{MemoryKB: kb, drops: map[string]bool{
		"drifter_hide": true, "grazer_horn": true, "glow_gland": true,
	}}
}

func bankDeps(t *testing.T, f *bankFake) HuntDeps {
	return HuntDeps{
		Client: f, KB: newDropsKB(t), HomeStation: "home_poi", GiftTo: "trucker-9",
		usernameFor: func(id string) (string, error) { return "user:" + id, nil },
	}
}

// depositedQty returns item -> quantity over the recorded deposits.
func depositedQty(ds []map[string]any) map[string]int {
	out := map[string]int{}
	for _, d := range ds {
		out[d["item_id"].(string)] += d["quantity"].(int)
	}
	return out
}

func TestHuntBankSurplusKeepsNeedItem(t *testing.T) {
	t.Parallel()
	f := &bankFake{state: bankDockedAt("home_poi", map[string]float64{
		"fuel_cell": 5, "repair_kit": 2, "glow_gland": 4, "drifter_hide": 10,
	})}
	if err := huntBankSurplus(context.Background(), bankDeps(t, f), nil, map[string]int{"glow_gland": 3}); err != nil {
		t.Fatalf("huntBankSurplus: %v", err)
	}
	got := depositedQty(f.deposits)
	want := map[string]int{"drifter_hide": 10, "glow_gland": 1}
	if !maps.Equal(got, want) {
		t.Errorf("deposits = %v, want %v (need item kept to its quantity, consumables untouched)", got, want)
	}
	for _, d := range f.deposits {
		if d["source"] != "cargo" || d["target"] != "self" {
			t.Errorf("deposit %v: want source=cargo target=self", d)
		}
	}
	if len(f.gifts) != 0 {
		t.Errorf("gifts = %v, want none below the %d-unit batch", f.gifts, huntGiftBatch)
	}
}

func TestHuntBankSurplusGiftsAtBatch(t *testing.T) {
	t.Parallel()
	t.Run("at batch", func(t *testing.T) {
		t.Parallel()
		f := &bankFake{
			state:   bankDockedAt("home_poi", map[string]float64{"drifter_hide": 20}),
			storage: map[string]int{"grazer_horn": 30, "fuel_cell": 40},
		}
		if err := huntBankSurplus(context.Background(), bankDeps(t, f), nil, nil); err != nil {
			t.Fatalf("huntBankSurplus: %v", err)
		}
		if len(f.gifts) != 2 {
			t.Fatalf("gifts = %v, want drifter_hide and grazer_horn", f.gifts)
		}
		for i, want := range []struct {
			id  string
			qty int
		}{{"drifter_hide", 20}, {"grazer_horn", 30}} {
			g := f.gifts[i]
			if g["item_id"] != want.id || g["quantity"] != want.qty || g["recipient"] != "user:trucker-9" || g["source"] != "storage" {
				t.Errorf("gift %d = %v, want %d %s to user:trucker-9 from storage", i, g, want.qty, want.id)
			}
		}
		if f.storage["fuel_cell"] != 40 {
			t.Errorf("fuel_cell in storage = %d, want 40 left alone", f.storage["fuel_cell"])
		}
	})
	t.Run("below batch", func(t *testing.T) {
		t.Parallel()
		f := &bankFake{
			state:   bankDockedAt("home_poi", map[string]float64{"drifter_hide": 19}),
			storage: map[string]int{"grazer_horn": 30},
		}
		if err := huntBankSurplus(context.Background(), bankDeps(t, f), nil, nil); err != nil {
			t.Fatalf("huntBankSurplus: %v", err)
		}
		if len(f.gifts) != 0 {
			t.Errorf("gifts = %v, want none at 49 units", f.gifts)
		}
	})
	t.Run("no collector banks only", func(t *testing.T) {
		t.Parallel()
		f := &bankFake{state: bankDockedAt("home_poi", map[string]float64{"drifter_hide": 80})}
		deps := bankDeps(t, f)
		deps.GiftTo = ""
		if err := huntBankSurplus(context.Background(), deps, nil, nil); err != nil {
			t.Fatalf("huntBankSurplus: %v", err)
		}
		if len(f.deposits) != 1 || len(f.gifts) != 0 {
			t.Errorf("deposits %v gifts %v, want one deposit and no gift", f.deposits, f.gifts)
		}
	})
	t.Run("away from base stores and never gifts", func(t *testing.T) {
		t.Parallel()
		f := &bankFake{state: bankDockedAt("elsewhere", map[string]float64{"drifter_hide": 80})}
		if err := huntBankSurplus(context.Background(), bankDeps(t, f), nil, nil); err != nil {
			t.Fatalf("huntBankSurplus: %v", err)
		}
		if len(f.deposits) != 1 || len(f.gifts) != 0 {
			t.Errorf("deposits %v gifts %v, want one deposit and no gift away from the home station", f.deposits, f.gifts)
		}
	})
}

func TestHuntBankSurplusAtHomeGiftsToMiningbot0000(t *testing.T) {
	t.Parallel()
	f := &bankFake{state: bankDockedAt(haulDumpPOI, map[string]float64{"drifter_hide": 60})}
	deps := bankDeps(t, f)
	deps.HomeStation = "" // home hunting at CJ banks with or without a base
	if err := huntBankSurplus(context.Background(), deps, nil, nil); err != nil {
		t.Fatalf("huntBankSurplus: %v", err)
	}
	if len(f.gifts) != 1 {
		t.Fatalf("gifts = %v, want one", f.gifts)
	}
	if g := f.gifts[0]; g["recipient"] != "user:"+haulDumpRecipient || g["quantity"] != 60 || g["source"] != "storage" {
		t.Errorf("gift = %v, want 60 drifter_hide to user:%s from storage", g, haulDumpRecipient)
	}
}

func TestHuntBankReservedByCatalogCategory(t *testing.T) {
	t.Parallel()
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	if err := kb.StoreItems(ctx, []knowledge.CatalogItem{{ID: "standard_rounds_box", Category: "ammo"}}); err != nil {
		t.Fatal(err)
	}
	if !huntBankIsReserved(ctx, kb, "standard_rounds_box") {
		t.Error("ammo-category item should be reserved")
	}
	if huntBankIsReserved(ctx, kb, "drifter_hide") {
		t.Error("an uncatalogued part should be bankable")
	}
}

// huntPassBankFake is a bankFake a whole Hunt pass can run against.
type huntPassBankFake struct{ bankFake }

func (f *huntPassBankFake) IsConnected() bool               { return true }
func (f *huntPassBankFake) GetStatus(context.Context) error { return nil }
func (f *huntPassBankFake) Battle(context.Context, string, map[string]any) error {
	return nil
}

// The pass banks at its docked point even when the board gate skips the
// mission queries: a hunter back home from the grounds is exactly the one
// whose board was dry last pass.
func TestHuntPassBanksAtHomeStation(t *testing.T) {
	t.Parallel()
	st := bankDockedAt("home_poi", map[string]float64{"drifter_hide": 12})
	st.System.ID = "home_sys"
	st.Ship.Hull, st.Ship.MaxHull = 100, 100
	f := &huntPassBankFake{bankFake{state: st}}
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	gate := &huntBoardGate{}
	gate.record("home_poi", now, true)
	deps := bankDeps(t, &f.bankFake)
	deps.Client = f
	deps.boardGate = gate
	deps.NowFn = func() time.Time { return now }
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if got := depositedQty(f.deposits); got["drifter_hide"] != 12 {
		t.Errorf("deposits = %v, want 12 drifter_hide banked at the home station", got)
	}
}

// Shipboard consumables in catalog category consumable -- premium fuel cells,
// hull patches -- are never banked or gifted.
func TestHuntBankNeverBanksConsumables(t *testing.T) {
	t.Parallel()
	f := &bankFake{
		state:   bankDockedAt("home_poi", map[string]float64{"premium_fuel_cell": 4, "hull_patch": 6, "drifter_hide": 60}),
		storage: map[string]int{"premium_fuel_cell": 70, "hull_patch": 70},
	}
	if err := huntBankSurplus(context.Background(), bankDeps(t, f), nil, nil); err != nil {
		t.Fatalf("huntBankSurplus: %v", err)
	}
	if got := depositedQty(f.deposits); !maps.Equal(got, map[string]int{"drifter_hide": 60}) {
		t.Errorf("deposits = %v, want only drifter_hide", got)
	}
	if len(f.gifts) != 1 || f.gifts[0]["item_id"] != "drifter_hide" {
		t.Errorf("gifts = %v, want only drifter_hide", f.gifts)
	}
}

// A held Station Need the pass did not resume still keeps its parts: keep is
// built from every held mission's item objectives.
func TestHuntBankKeepsHeldNeedNotResumed(t *testing.T) {
	t.Parallel()
	f := &bankFake{
		state:   bankDockedAt("home_poi", map[string]float64{"glow_gland": 5, "drifter_hide": 60}),
		storage: map[string]int{"glow_gland": 10},
		active: []byte(`{"missions":[{"mission_id":"m1","type":"delivery","objectives":[` +
			`{"type":"deliver_item","item_id":"glow_gland","required":5,"current":0}]}]}`),
	}
	huntBankAtDock(context.Background(), bankDeps(t, f), io.Discard, "t", nil)
	if got := depositedQty(f.deposits); !maps.Equal(got, map[string]int{"drifter_hide": 60}) {
		t.Errorf("deposits = %v, want only drifter_hide (glow_gland held for the need)", got)
	}
	for _, g := range f.gifts {
		if g["item_id"] == "glow_gland" {
			t.Errorf("gifted %v, want the held need's item kept", g)
		}
	}
	if len(f.gifts) != 1 {
		t.Errorf("gifts = %v, want drifter_hide only", f.gifts)
	}
}

// Deposits may bank anything spare, but only recorded creature drops are
// gifted: old ore and modules in base storage stay there.
func TestHuntBankGiftsOnlyCreatureDrops(t *testing.T) {
	t.Parallel()
	f := &bankFake{
		state:   bankDockedAt("home_poi", map[string]float64{"drifter_hide": 60}),
		storage: map[string]int{"iron_ore": 100, "mining_laser_i": 3},
	}
	if err := huntBankSurplus(context.Background(), bankDeps(t, f), nil, nil); err != nil {
		t.Fatalf("huntBankSurplus: %v", err)
	}
	if len(f.gifts) != 1 || f.gifts[0]["item_id"] != "drifter_hide" || f.gifts[0]["quantity"] != 60 {
		t.Errorf("gifts = %v, want 60 drifter_hide only", f.gifts)
	}
	if f.storage["iron_ore"] != 100 || f.storage["mining_laser_i"] != 3 {
		t.Errorf("storage = %v, want ore and module left alone", f.storage)
	}
}
