package worker

import (
	"context"
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
	deposits []map[string]any
	gifts    []map[string]any
}

func (f *bankFake) GetState() *game.State             { return f.state }
func (f *bankFake) GetCargo(context.Context) error    { return nil }
func (f *bankFake) ViewStorage(context.Context) error { return nil }

func (f *bankFake) GetRawJSON(key string) []byte {
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

func bankDeps(f *bankFake) HuntDeps {
	return HuntDeps{
		Client: f, KB: knowledge.NewMemoryKB(), HomeStation: "home_poi", GiftTo: "trucker-9",
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
	if err := huntBankSurplus(context.Background(), bankDeps(f), nil, map[string]int{"glow_gland": 3}); err != nil {
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
		if err := huntBankSurplus(context.Background(), bankDeps(f), nil, nil); err != nil {
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
		if err := huntBankSurplus(context.Background(), bankDeps(f), nil, nil); err != nil {
			t.Fatalf("huntBankSurplus: %v", err)
		}
		if len(f.gifts) != 0 {
			t.Errorf("gifts = %v, want none at 49 units", f.gifts)
		}
	})
	t.Run("no collector banks only", func(t *testing.T) {
		t.Parallel()
		f := &bankFake{state: bankDockedAt("home_poi", map[string]float64{"drifter_hide": 80})}
		deps := bankDeps(f)
		deps.GiftTo = ""
		if err := huntBankSurplus(context.Background(), deps, nil, nil); err != nil {
			t.Fatalf("huntBankSurplus: %v", err)
		}
		if len(f.deposits) != 1 || len(f.gifts) != 0 {
			t.Errorf("deposits %v gifts %v, want one deposit and no gift", f.deposits, f.gifts)
		}
	})
	t.Run("away from base does nothing", func(t *testing.T) {
		t.Parallel()
		f := &bankFake{state: bankDockedAt("elsewhere", map[string]float64{"drifter_hide": 80})}
		if err := huntBankSurplus(context.Background(), bankDeps(f), nil, nil); err != nil {
			t.Fatalf("huntBankSurplus: %v", err)
		}
		if len(f.deposits) != 0 || len(f.gifts) != 0 {
			t.Errorf("deposits %v gifts %v, want nothing away from the home station", f.deposits, f.gifts)
		}
	})
}

func TestHuntBankSurplusAtHomeGiftsToMiningbot0000(t *testing.T) {
	t.Parallel()
	f := &bankFake{state: bankDockedAt(haulDumpPOI, map[string]float64{"drifter_hide": 60})}
	deps := bankDeps(f)
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
	deps := bankDeps(&f.bankFake)
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
