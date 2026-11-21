package worker

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// factionPullFake models faction storage -> cargo -> personal storage, moving
// state.Ship.CargoUsed the way the server does.
type factionPullFake struct {
	*fakeClient
	withdrawn  map[string]int
	deposited  map[string]int
	pending    map[string]int // withdrawn but not yet landed in cargo (tick-deferred)
	direct     map[string]int // deposit_items source=faction target=self (no cargo)
	withdrawEr error
	depositEr  error
	directEr   error
}

func newFactionPullFake(free float64, raw string) *factionPullFake {
	fc := &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 150, CargoUsed: 150 - free}}}
	if raw != "" {
		fc.raw = map[string][]byte{"faction_storage": []byte(raw)}
	}
	f := &factionPullFake{fakeClient: fc, withdrawn: map[string]int{}, deposited: map[string]int{}, pending: map[string]int{}, direct: map[string]int{}}
	// The real server executes a withdraw on the next tick and the client's
	// cargo cache only updates on an explicit GetCargo, so the fake lands the
	// withdrawn units in state.Ship.Cargo on that refresh and not before.
	fc.onGetCargo = func() {
		for id, q := range f.pending {
			f.addCargo(id, q)
			f.state.Ship.CargoUsed += float64(q)
		}
		f.pending = map[string]int{}
	}
	return f
}

func (f *factionPullFake) addCargo(id string, q int) {
	for i := range f.state.Ship.Cargo {
		if f.state.Ship.Cargo[i].ItemID == id {
			f.state.Ship.Cargo[i].Quantity += float64(q)
			return
		}
	}
	f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: id, Quantity: float64(q)})
}

func (f *factionPullFake) ViewFactionStorage(context.Context) error { return nil }

func (f *factionPullFake) FactionWithdrawItems(_ context.Context, id string, q int) error {
	if f.withdrawEr != nil {
		return f.withdrawEr
	}
	f.withdrawn[id] += q
	f.pending[id] += q
	return nil
}

func (f *factionPullFake) DepositItems(_ context.Context, id string, q float64) error {
	if f.depositEr != nil {
		return f.depositEr
	}
	if cargoCount(f.state, id) < int(q) {
		return errors.New("item " + id + " not found in cargo")
	}
	f.deposited[id] += int(q)
	f.addCargo(id, -int(q))
	f.state.Ship.CargoUsed -= q
	return nil
}

func (f *factionPullFake) DepositItemsPayload(_ context.Context, p map[string]any) error {
	if f.directEr != nil {
		return f.directEr
	}
	if p["source"] != "faction" || p["target"] != "self" {
		return errors.New("unexpected deposit_items payload")
	}
	f.direct[p["item_id"].(string)] += p["quantity"].(int)
	return nil
}

const factionReply = `{"base_id":"b1","items":[
 {"item_id":"pirate_moonshine","name":"Moonshine","quantity":72,"size":1},
 {"item_id":"water_ice","name":"Water Ice","quantity":1335,"size":1}]}`

// The server moves faction storage straight to personal storage with
// deposit_items source=faction target=self; the hold plays no part (operator
// 2026-11-20: purified_water crawled 175 per pass through a 175-space hold).
func TestFactionPullMovesAllowListedOnlyDirect(t *testing.T) {
	fc := newFactionPullFake(150, factionReply)
	var out bytes.Buffer
	moved, err := FactionPull(context.Background(), fc, []FactionPullRule{{Item: "pirate_moonshine"}}, &out)
	if err != nil || moved != 72 {
		t.Fatalf("moved=%d err=%v, want 72 nil", moved, err)
	}
	if fc.direct["pirate_moonshine"] != 72 || len(fc.withdrawn) != 0 {
		t.Errorf("direct=%v withdrawn=%v, want a direct 72 and no cargo withdraw", fc.direct, fc.withdrawn)
	}
	if _, ok := fc.direct["water_ice"]; ok {
		t.Errorf("water_ice must not be touched: %v", fc.direct)
	}
	if !strings.Contains(out.String(), "faction_pull: moved 72 pirate_moonshine faction->personal") {
		t.Errorf("log %q", out.String())
	}
}

func TestFactionPullIgnoresHoldSize(t *testing.T) {
	fc := newFactionPullFake(0, factionReply) // hold full
	moved, err := FactionPull(context.Background(), fc, []FactionPullRule{{Item: "water_ice", Size: 2}}, &bytes.Buffer{})
	if err != nil || moved != 1335 || fc.direct["water_ice"] != 1335 {
		t.Fatalf("moved=%d err=%v direct=%v, want all 1335 regardless of hold and unit size", moved, err, fc.direct)
	}
}

func TestFactionPullSkipsWithoutAReading(t *testing.T) {
	fc := newFactionPullFake(150, "")
	var out bytes.Buffer
	moved, err := FactionPull(context.Background(), fc, []FactionPullRule{{Item: "pirate_moonshine"}}, &out)
	if err != nil || moved != 0 || len(fc.direct) != 0 {
		t.Fatalf("moved=%d err=%v direct=%v", moved, err, fc.direct)
	}
	if !strings.Contains(out.String(), "faction_pull: no faction storage reading; skipping") {
		t.Errorf("log %q", out.String())
	}
}

func TestFactionPullTransferFailureStops(t *testing.T) {
	fc := newFactionPullFake(150, factionReply)
	fc.directEr = errors.New("boom")
	moved, err := FactionPull(context.Background(), fc, []FactionPullRule{{Item: "pirate_moonshine"}, {Item: "water_ice"}}, &bytes.Buffer{})
	if err == nil || moved != 0 {
		t.Fatalf("moved=%d err=%v, want 0 and error", moved, err)
	}
}

// Units an older cargo-routed pull left aboard are still deposited.
func TestFactionPullDrainsStrandedCargo(t *testing.T) {
	f := newFactionPullFake(150, factionReply)
	f.addCargo("pirate_moonshine", 12)
	f.state.Ship.CargoUsed += 12
	moved, err := FactionPull(context.Background(), f, []FactionPullRule{{Item: "pirate_moonshine"}}, &bytes.Buffer{})
	if err != nil || moved != 84 || f.deposited["pirate_moonshine"] != 12 || f.direct["pirate_moonshine"] != 72 {
		t.Fatalf("moved=%d err=%v deposited=%v direct=%v, want 12 from cargo + 72 direct", moved, err, f.deposited, f.direct)
	}
}

func TestFactionPullDrainsStrandedCargoWhenFactionIsEmpty(t *testing.T) {
	f := newFactionPullFake(150, `{"base_id":"b1","items":[]}`)
	f.addCargo("pirate_moonshine", 60)
	f.state.Ship.CargoUsed += 60
	moved, err := FactionPull(context.Background(), f, []FactionPullRule{{Item: "pirate_moonshine"}}, &bytes.Buffer{})
	if err != nil || moved != 60 || f.deposited["pirate_moonshine"] != 60 || len(f.direct) != 0 {
		t.Fatalf("moved=%d err=%v deposited=%v direct=%v", moved, err, f.deposited, f.direct)
	}
}

// A keep floor leaves that much in faction storage: the goods the pull serves
// are often also station upkeep or build stock, and the pull has no other way
// to know.
func TestFactionPullLeavesKeepInFactionStorage(t *testing.T) {
	fc := newFactionPullFake(1000, `{"base_id":"b1","items":[{"item_id":"control_node","quantity":300}]}`)
	moved, err := FactionPull(context.Background(), fc, []FactionPullRule{{Item: "control_node", Keep: 200}}, &bytes.Buffer{})
	if err != nil || moved != 100 || fc.direct["control_node"] != 100 {
		t.Fatalf("moved %d, direct %v; want 100 of the 300 held above a keep of 200", moved, fc.direct)
	}
}

func TestFactionPullAtOrBelowKeepWithdrawsNothing(t *testing.T) {
	fc := newFactionPullFake(1000, `{"base_id":"b1","items":[{"item_id":"control_node","quantity":200}]}`)
	moved, err := FactionPull(context.Background(), fc, []FactionPullRule{{Item: "control_node", Keep: 200}}, &bytes.Buffer{})
	if err != nil || moved != 0 || len(fc.direct) != 0 {
		t.Fatalf("moved %d, direct %v; want nothing at the floor", moved, fc.direct)
	}
}

func TestParseFactionPull(t *testing.T) {
	got, err := ParseFactionPull(" a , b ,")
	if err != nil || len(got) != 2 || got[0].Item != "a" || got[1].Item != "b" {
		t.Fatalf("got %v %v", got, err)
	}
	if got, err := ParseFactionPull(""); got != nil || err != nil {
		t.Errorf("empty: %v %v", got, err)
	}
	if _, err := ParseFactionPull("pirate_moonshine,energy_crystal"); err == nil {
		t.Error("energy_crystal must be refused")
	}
}

// A repeated id would reuse the one faction-storage reading and over-withdraw
// on its second pass, so the first occurrence wins.
func TestParseFactionPullDedupes(t *testing.T) {
	got, err := ParseFactionPull("moonshine, stolen_art,moonshine ,stolen_art,iron_ore")
	if err != nil {
		t.Fatalf("ParseFactionPull: %v", err)
	}
	if want := []FactionPullRule{{Item: "moonshine"}, {Item: "stolen_art"}, {Item: "iron_ore"}}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseFactionPullKeep(t *testing.T) {
	got, err := ParseFactionPull("trade_authenticator:200, pirate_moonshine")
	if err != nil {
		t.Fatal(err)
	}
	want := []FactionPullRule{{Item: "trade_authenticator", Keep: 200}, {Item: "pirate_moonshine"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for _, bad := range []string{"x:abc", "x:-1", ":5", "energy_crystal:10"} {
		if _, err := ParseFactionPull(bad); err == nil {
			t.Errorf("ParseFactionPull(%q) accepted", bad)
		}
	}
}
