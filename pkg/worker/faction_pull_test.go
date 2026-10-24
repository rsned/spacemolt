package worker

import (
	"bytes"
	"context"
	"errors"
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
	withdrawEr error
	depositEr  error
}

func newFactionPullFake(free float64, raw string) *factionPullFake {
	fc := &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 150, CargoUsed: 150 - free}}}
	if raw != "" {
		fc.raw = map[string][]byte{"faction_storage": []byte(raw)}
	}
	return &factionPullFake{fakeClient: fc, withdrawn: map[string]int{}, deposited: map[string]int{}}
}

func (f *factionPullFake) ViewFactionStorage(context.Context) error { return nil }

func (f *factionPullFake) FactionWithdrawItems(_ context.Context, id string, q int) error {
	if f.withdrawEr != nil {
		return f.withdrawEr
	}
	f.withdrawn[id] += q
	f.state.Ship.CargoUsed += float64(q)
	return nil
}

func (f *factionPullFake) DepositItems(_ context.Context, id string, q float64) error {
	if f.depositEr != nil {
		return f.depositEr
	}
	f.deposited[id] += int(q)
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
