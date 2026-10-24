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
	withdrawEr error
	depositEr  error
}

func newFactionPullFake(free float64, raw string) *factionPullFake {
	fc := &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 150, CargoUsed: 150 - free}}}
	if raw != "" {
		fc.raw = map[string][]byte{"faction_storage": []byte(raw)}
	}
	f := &factionPullFake{fakeClient: fc, withdrawn: map[string]int{}, deposited: map[string]int{}, pending: map[string]int{}}
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

const factionReply = `{"base_id":"b1","items":[
 {"item_id":"pirate_moonshine","name":"Moonshine","quantity":72,"size":1},
 {"item_id":"water_ice","name":"Water Ice","quantity":1335,"size":1}]}`

func TestFactionPullMovesAllowListedOnly(t *testing.T) {
	fc := newFactionPullFake(150, factionReply)
	var out bytes.Buffer
	moved, err := FactionPull(context.Background(), fc, []string{"pirate_moonshine"}, &out)
	if err != nil || moved != 72 {
		t.Fatalf("moved=%d err=%v, want 72 nil", moved, err)
	}
	if fc.withdrawn["pirate_moonshine"] != 72 || fc.deposited["pirate_moonshine"] != 72 {
		t.Errorf("withdrawn=%v deposited=%v", fc.withdrawn, fc.deposited)
	}
	if _, ok := fc.withdrawn["water_ice"]; ok {
		t.Errorf("water_ice must not be touched: %v", fc.withdrawn)
	}
	if !strings.Contains(out.String(), "faction_pull: moved 72 pirate_moonshine faction->personal") {
		t.Errorf("log %q", out.String())
	}
}

func TestFactionPullBoundedByFreeHold(t *testing.T) {
	fc := newFactionPullFake(50, factionReply)
	moved, err := FactionPull(context.Background(), fc, []string{"pirate_moonshine"}, &bytes.Buffer{})
	if err != nil || moved != 50 {
		t.Fatalf("moved=%d err=%v, want 50 nil", moved, err)
	}
	if fc.withdrawn["pirate_moonshine"] != 50 || fc.deposited["pirate_moonshine"] != 50 {
		t.Errorf("withdrawn=%v deposited=%v", fc.withdrawn, fc.deposited)
	}
}

func TestFactionPullSkipsWithoutAReading(t *testing.T) {
	fc := newFactionPullFake(150, "")
	var out bytes.Buffer
	moved, err := FactionPull(context.Background(), fc, []string{"pirate_moonshine"}, &out)
	if err != nil || moved != 0 || len(fc.withdrawn) != 0 {
		t.Fatalf("moved=%d err=%v withdrawn=%v", moved, err, fc.withdrawn)
	}
	if !strings.Contains(out.String(), "faction_pull: no faction storage reading; skipping") {
		t.Errorf("log %q", out.String())
	}
}

func TestFactionPullDepositFailureStops(t *testing.T) {
	fc := newFactionPullFake(150, factionReply)
	fc.depositEr = errors.New("boom")
	moved, err := FactionPull(context.Background(), fc, []string{"pirate_moonshine", "water_ice"}, &bytes.Buffer{})
	if err == nil || moved != 0 {
		t.Fatalf("moved=%d err=%v, want 0 and error", moved, err)
	}
	if _, ok := fc.withdrawn["water_ice"]; ok {
		t.Errorf("must stop after the failed deposit: %v", fc.withdrawn)
	}
}

func TestParseFactionPull(t *testing.T) {
	got, err := ParseFactionPull(" a , b ,")
	if err != nil || len(got) != 2 || got[0] != "a" || got[1] != "b" {
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
	if want := []string{"moonshine", "stolen_art", "iron_ore"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFactionPullDepositsWhatLandedInCargo(t *testing.T) {
	// Live 2026-10-24: the withdraw ack returns before the server moves the
	// goods, so an immediate deposit failed "not found in cargo" every pass
	// while each pass withdrew another 6 into cargo. The pull must wait a
	// tick, refresh cargo, and deposit what is actually aboard — including
	// units a previous pass left stranded.
	f := newFactionPullFake(150, factionReply)
	f.addCargo("pirate_moonshine", 12) // stranded by an earlier failed pass
	f.state.Ship.CargoUsed += 12
	var out bytes.Buffer
	moved, err := FactionPull(context.Background(), f, []string{"pirate_moonshine"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if f.withdrawn["pirate_moonshine"] != 72 {
		t.Fatalf("want withdraw 72, got %v", f.withdrawn)
	}
	if f.deposited["pirate_moonshine"] != 84 || moved != 84 {
		t.Fatalf("want deposit 84 (72 landed + 12 stranded) and moved 84, got deposited %v moved %d", f.deposited, moved)
	}
	if !slices.Contains(f.calls, "get_cargo") {
		t.Fatalf("want a cargo refresh between withdraw and deposit, calls %v", f.calls)
	}
	if cargoCount(f.state, "pirate_moonshine") != 0 {
		t.Fatalf("cargo should be drained, got %d", cargoCount(f.state, "pirate_moonshine"))
	}
}

func TestFactionPullDrainsStrandedCargoWhenFactionIsEmpty(t *testing.T) {
	// Live 2026-10-24 14:30: the failed passes had moved every unit from
	// faction storage into cargo; with faction quantity 0 the pull skipped
	// the item and the 60 units sat in cargo forever. Allow-listed cargo
	// must be deposited even when there is nothing left to withdraw.
	f := newFactionPullFake(150, `{"base_id":"b1","items":[]}`)
	f.addCargo("pirate_moonshine", 60)
	f.state.Ship.CargoUsed += 60
	var out bytes.Buffer
	moved, err := FactionPull(context.Background(), f, []string{"pirate_moonshine"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if f.withdrawn["pirate_moonshine"] != 0 {
		t.Fatalf("nothing to withdraw, got %v", f.withdrawn)
	}
	if f.deposited["pirate_moonshine"] != 60 || moved != 60 {
		t.Fatalf("want deposit 60 from cargo, got deposited %v moved %d", f.deposited, moved)
	}
}
