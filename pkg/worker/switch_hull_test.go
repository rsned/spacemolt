package worker

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// Live list_ships reply at Conjunction Junction, 2026-11-21 (trimmed).
const ownedShipsCJ = `{"active_ship_class":"cobble","active_ship_id":"cob1",
 "faction_garage":[
  {"class_id":"congregation","ship_id":"cong1"},
  {"class_id":"congregation","ship_id":"cong2"}],
 "ships":[{"class_id":"cobble","ship_id":"cob1","is_active":true}]}`

type switchFake struct {
	*fakeClient
	switched  []string
	refuseIDs map[string]bool
}

func (f *switchFake) ListShips(context.Context) error { return nil }
func (f *switchFake) SwitchShip(_ context.Context, id string) error {
	f.switched = append(f.switched, id)
	if f.refuseIDs[id] {
		return errors.New("ship_not_here: that ship is not stored at this station")
	}
	return nil
}

func newSwitchDispatch(raw string, docked bool) (*switchFake, *WorkerDispatch, *bytes.Buffer) {
	fc := &switchFake{fakeClient: &fakeClient{state: &game.State{Doc: docked}, raw: map[string][]byte{"owned_ships": []byte(raw)}}}
	var out bytes.Buffer
	return fc, NewWorkerDispatch(fc, nil, nil, &out), &out
}

// A trucker docked at CJ boards a Congregation from the faction garage
// (route fleet, 2026-11-21: craft_boss stores finished hulls there).
func TestSwitchHullTakesGarageHull(t *testing.T) {
	fc, d, out := newSwitchDispatch(ownedShipsCJ, true)
	if err := d.Run(context.Background(), []string{"switch_hull", "congregation"}); err != nil {
		t.Fatal(err)
	}
	if len(fc.switched) != 1 || fc.switched[0] != "cong1" {
		t.Fatalf("switched %v, want [cong1]", fc.switched)
	}
	if !strings.Contains(out.String(), "switch_hull: boarded congregation cong1") {
		t.Errorf("log %q", out.String())
	}
}

// Already flying the class: nothing to do, every hourly pass after the first.
func TestSwitchHullAlreadyInClassIsNoop(t *testing.T) {
	raw := strings.Replace(ownedShipsCJ, `"active_ship_class":"cobble"`, `"active_ship_class":"congregation"`, 1)
	fc, d, _ := newSwitchDispatch(raw, true)
	if err := d.Run(context.Background(), []string{"switch_hull", "congregation"}); err != nil || len(fc.switched) != 0 {
		t.Fatalf("err %v switched %v", err, fc.switched)
	}
}

// Another trucker may claim the same garage hull first; try the next one.
func TestSwitchHullTriesNextWhenOneIsRefused(t *testing.T) {
	fc, d, _ := newSwitchDispatch(ownedShipsCJ, true)
	fc.refuseIDs = map[string]bool{"cong1": true}
	if err := d.Run(context.Background(), []string{"switch_hull", "congregation"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fc.switched, ",") != "cong1,cong2" {
		t.Fatalf("switched %v", fc.switched)
	}
}

// No hull of the class here, or undocked: a quiet no-op, not an error.
func TestSwitchHullNoneHereOrUndockedIsNoop(t *testing.T) {
	fc, d, _ := newSwitchDispatch(`{"active_ship_class":"cobble","faction_garage":[],"ships":[]}`, true)
	if err := d.Run(context.Background(), []string{"switch_hull", "congregation"}); err != nil || len(fc.switched) != 0 {
		t.Fatalf("err %v switched %v", err, fc.switched)
	}
	fc, d, _ = newSwitchDispatch(ownedShipsCJ, false)
	if err := d.Run(context.Background(), []string{"switch_hull", "congregation"}); err != nil || len(fc.switched) != 0 {
		t.Fatalf("undocked: err %v switched %v", err, fc.switched)
	}
}

// After boarding, the cached ship state is still the old hull's; switch_hull
// re-reads cargo so the next command sizes loads from the new hold.
func TestSwitchHullRefreshesCargoAfterBoarding(t *testing.T) {
	fc, d, _ := newSwitchDispatch(ownedShipsCJ, true)
	refreshed := false
	fc.onGetCargo = func() { refreshed = len(fc.switched) > 0 }
	if err := d.Run(context.Background(), []string{"switch_hull", "congregation"}); err != nil {
		t.Fatal(err)
	}
	if !refreshed {
		t.Fatalf("no get_cargo after boarding; calls %v", fc.calls)
	}
}
