package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// collectFake is the collector's storage at several stations, read remotely
// with view_storage station_id; withdrawals land in cargo.
type collectFake struct {
	game.GameClient
	state     *game.State
	storage   map[string]map[string]int // station -> item -> units
	viewing   string
	navs      []string // "system/poi" per nav call
	withdrawn map[string]int
	gifts     []map[string]any
}

func (f *collectFake) GetState() *game.State           { return f.state }
func (f *collectFake) GetStatus(context.Context) error { return nil }
func (f *collectFake) GetCargo(context.Context) error  { return nil }
func (f *collectFake) Undock(context.Context) error    { f.state.Doc = false; return nil }
func (f *collectFake) Dock(context.Context) error      { f.state.Doc = true; return nil }

func (f *collectFake) ViewStorageAt(_ context.Context, station string) error {
	f.viewing = station
	return nil
}

func (f *collectFake) GetRawJSON(key string) []byte {
	if key != "storage" {
		return nil
	}
	return storageFrame(f.viewing+"_base", f.storage[f.viewing])
}

func (f *collectFake) FindRoute(_ context.Context, target string) ([]game.RouteStep, error) {
	return []game.RouteStep{{SystemID: "start"}, {SystemID: "sys_of_" + target}}, nil
}

// WithdrawItems moves units from storage at the current station into cargo.
// Like the server, it refuses a withdrawal the hold cannot take whole.
func (f *collectFake) WithdrawItems(_ context.Context, id string, qty float64) error {
	sh := &f.state.Ship
	if sh.CargoCapacity > 0 && sh.CargoUsed+qty > sh.CargoCapacity {
		return errors.New("not enough cargo space")
	}
	if f.withdrawn == nil {
		f.withdrawn = map[string]int{}
	}
	f.withdrawn[id] += int(qty)
	if st := f.storage[f.state.CurrentPOI]; st != nil {
		st[id] -= int(qty)
	}
	sh.Cargo = append(sh.Cargo, game.CargoItem{ItemID: id, Quantity: qty})
	sh.CargoUsed += qty
	return nil
}

// SendGift from cargo empties that cargo line.
func (f *collectFake) SendGift(_ context.Context, p map[string]any) error {
	f.gifts = append(f.gifts, p)
	sh := &f.state.Ship
	kept := sh.Cargo[:0]
	for _, c := range sh.Cargo {
		if c.ItemID == p["item_id"] {
			sh.CargoUsed -= c.Quantity
			continue
		}
		kept = append(kept, c)
	}
	sh.Cargo = kept
	return nil
}

func collectDeps(t *testing.T, f *collectFake) CollectDeps {
	return CollectDeps{
		Client: f, KB: newDropsKB(t),
		usernameFor: func(id string) (string, error) { return "user:" + id, nil },
		nav: func(_ context.Context, system, poi string) error {
			f.navs = append(f.navs, system+"/"+poi)
			f.state.CurrentPOI = poi
			return nil
		},
	}
}

func TestCollectGiftsPicksFullestStation(t *testing.T) {
	t.Parallel()
	f := &collectFake{
		state: &game.State{Doc: true, CurrentPOI: "somewhere"},
		storage: map[string]map[string]int{
			"st_a": {"drifter_hide": 20, "iron_ore": 500},
			"st_b": {"drifter_hide": 40, "grazer_horn": 30, "fuel_cell": 100},
			"st_c": {"grazer_horn": 55},
		},
	}
	if err := CollectGifts(context.Background(), collectDeps(t, f), []string{"st_a", "st_b", "st_c"}); err != nil {
		t.Fatalf("CollectGifts: %v", err)
	}
	wantNavs := []string{"sys_of_st_b/st_b", haulDumpSystem + "/" + haulDumpPOI}
	if len(f.navs) != 2 || f.navs[0] != wantNavs[0] || f.navs[1] != wantNavs[1] {
		t.Errorf("navs = %v, want %v", f.navs, wantNavs)
	}
	if len(f.withdrawn) != 2 || f.withdrawn["drifter_hide"] != 40 || f.withdrawn["grazer_horn"] != 30 {
		t.Errorf("withdrawn = %v, want 40 drifter_hide + 30 grazer_horn (no fuel_cell)", f.withdrawn)
	}
	if len(f.gifts) != 2 {
		t.Fatalf("gifts = %v, want two", f.gifts)
	}
	for _, g := range f.gifts {
		if g["recipient"] != "user:"+haulDumpRecipient || g["source"] != "cargo" {
			t.Errorf("gift %v: want to user:%s from cargo", g, haulDumpRecipient)
		}
	}
}

func TestCollectGiftsSkipsBelowMinimum(t *testing.T) {
	t.Parallel()
	f := &collectFake{
		state: &game.State{Doc: true, CurrentPOI: "somewhere"},
		storage: map[string]map[string]int{
			"st_a": {"drifter_hide": 49, "fuel_cell": 500},
			"st_b": {"grazer_horn": 10},
		},
	}
	if err := CollectGifts(context.Background(), collectDeps(t, f), []string{"st_a", "st_b"}); err != nil {
		t.Fatalf("CollectGifts: %v", err)
	}
	if len(f.navs) != 0 || len(f.withdrawn) != 0 || len(f.gifts) != 0 {
		t.Errorf("navs %v withdrawn %v gifts %v, want nothing below %d units", f.navs, f.withdrawn, f.gifts, collectMinUnits)
	}
}

// A run whose CJ leg failed leaves the parts aboard; the next run delivers
// them before collecting more.
func TestCollectGiftsDeliversCargoFirst(t *testing.T) {
	t.Parallel()
	st := &game.State{Doc: true, CurrentPOI: "st_b"}
	st.Ship.Cargo = []game.CargoItem{{ItemID: "drifter_hide", Quantity: 40}, {ItemID: "fuel_cell", Quantity: 3}}
	f := &collectFake{state: st, storage: map[string]map[string]int{"st_a": {"grazer_horn": 90}}}
	if err := CollectGifts(context.Background(), collectDeps(t, f), []string{"st_a"}); err != nil {
		t.Fatalf("CollectGifts: %v", err)
	}
	if len(f.navs) != 1 || f.navs[0] != haulDumpSystem+"/"+haulDumpPOI {
		t.Errorf("navs = %v, want straight to CJ", f.navs)
	}
	if len(f.withdrawn) != 0 {
		t.Errorf("withdrawn = %v, want nothing until the hold is delivered", f.withdrawn)
	}
	if len(f.gifts) != 1 || f.gifts[0]["item_id"] != "drifter_hide" || f.gifts[0]["quantity"] != 40 {
		t.Errorf("gifts = %v, want 40 drifter_hide only", f.gifts)
	}
}

// collect_gifts is a schedulable worker command, and needs its station list.
func TestCollectGiftsIsAWorkerCommand(t *testing.T) {
	t.Parallel()
	if !supported["collect_gifts"] {
		t.Fatal(`"collect_gifts" missing from the supported command set`)
	}
	d := NewWorkerDispatch(&collectFake{state: &game.State{}}, nil, nil, nil)
	if err := d.Run(context.Background(), []string{"collect_gifts"}); err == nil {
		t.Error("collect_gifts with no stations should error")
	}
}

// A stack larger than the hold is withdrawn in part, clamped to free space,
// delivered, and the rest is collected on the next run.
func TestCollectGiftsClampsToFreeCargo(t *testing.T) {
	t.Parallel()
	st := &game.State{Doc: true, CurrentPOI: "somewhere"}
	st.Ship.CargoCapacity, st.Ship.CargoUsed = 50, 10
	st.Ship.Cargo = []game.CargoItem{{ItemID: "fuel_cell", Quantity: 10}}
	f := &collectFake{state: st, storage: map[string]map[string]int{"st_a": {"grazer_horn": 100}}}
	deps := collectDeps(t, f)
	if err := CollectGifts(context.Background(), deps, []string{"st_a"}); err != nil {
		t.Fatalf("CollectGifts run 1: %v", err)
	}
	if f.withdrawn["grazer_horn"] != 40 {
		t.Errorf("run 1 withdrew %d grazer_horn, want 40 (the free space)", f.withdrawn["grazer_horn"])
	}
	if len(f.gifts) != 1 || f.gifts[0]["quantity"] != 40 {
		t.Fatalf("run 1 gifts = %v, want 40 grazer_horn delivered", f.gifts)
	}
	if got := f.storage["st_a"]["grazer_horn"]; got != 60 {
		t.Fatalf("storage after run 1 = %d, want 60 left", got)
	}
	if err := CollectGifts(context.Background(), deps, []string{"st_a"}); err != nil {
		t.Fatalf("CollectGifts run 2: %v", err)
	}
	if f.withdrawn["grazer_horn"] != 80 || len(f.gifts) != 2 {
		t.Errorf("after run 2: withdrew %d, gifts %v; want 80 total over two deliveries", f.withdrawn["grazer_horn"], f.gifts)
	}
}
