package spar

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// fakeBattleClient feeds a scripted sequence of BattleStates and records the
// battle actions dispatched against it.
type fakeBattleClient struct {
	states  []*game.BattleState
	idx     int
	actions []string // "<action>:<stance-or-target>"
}

func (f *fakeBattleClient) GetBattleStatus(_ context.Context) error { return nil }

func (f *fakeBattleClient) Battle(_ context.Context, action string, payload map[string]any) error {
	tag := action
	if s, ok := payload["stance"].(string); ok {
		tag += ":" + s
	} else if t, ok := payload["target_id"].(string); ok {
		tag += ":" + t
	}
	f.actions = append(f.actions, tag)
	return nil
}

func (f *fakeBattleClient) GetState() *game.State {
	var cur *game.BattleState
	if f.idx < len(f.states) {
		cur = f.states[f.idx]
	}
	f.idx++
	return &game.State{BattleState: cur}
}

func TestBattleOver(t *testing.T) {
	twoSides := &game.BattleState{IsParticipant: true, Participants: []game.BattleParticipant{
		{PlayerID: "me", SideID: "1", HullPct: 50},
		{PlayerID: "foe", SideID: "2", HullPct: 30},
	}}
	if BattleOver(twoSides) {
		t.Fatal("two living sides: battle should NOT be over")
	}
	oneSide := &game.BattleState{IsParticipant: true, Participants: []game.BattleParticipant{
		{PlayerID: "me", SideID: "1", HullPct: 50},
		{PlayerID: "foe", SideID: "2", HullPct: 0},
	}}
	if !BattleOver(oneSide) {
		t.Fatal("one living side: battle should be over")
	}
	if !BattleOver(nil) {
		t.Fatal("nil battle state: should be over")
	}
}

func TestRunPolicyLoop_DispatchesThenStops(t *testing.T) {
	inBattle := &game.BattleState{BattleID: "b1", IsParticipant: true, Participants: []game.BattleParticipant{
		{PlayerID: "me", SideID: "1", Zone: "mid", Stance: "fire", HullPct: 100},
		{PlayerID: "foe", SideID: "2", Zone: "mid", HullPct: 100},
	}}
	over := &game.BattleState{BattleID: "b1", IsParticipant: true, Participants: []game.BattleParticipant{
		{PlayerID: "me", SideID: "1", HullPct: 100},
		{PlayerID: "foe", SideID: "2", HullPct: 0},
	}}
	f := &fakeBattleClient{states: []*game.BattleState{inBattle, over}}

	err := RunPolicyLoop(context.Background(), f, "me", NewRetreater(), time.Millisecond)
	if err != nil {
		t.Fatalf("RunPolicyLoop error: %v", err)
	}
	if len(f.actions) != 1 || f.actions[0] != "stance:flee" {
		t.Fatalf("actions = %v, want [stance:flee]", f.actions)
	}
}

func TestFleeBot(t *testing.T) {
	f := &fakeBattleClient{}
	if err := fleeBot(context.Background(), f); err != nil {
		t.Fatalf("fleeBot error: %v", err)
	}
	if len(f.actions) != 1 || f.actions[0] != "stance:flee" {
		t.Fatalf("actions = %v, want [stance:flee]", f.actions)
	}
}

func TestBattleSignature_ChangesOnState(t *testing.T) {
	a := &game.BattleState{Participants: []game.BattleParticipant{
		{PlayerID: "me", Zone: "mid", Stance: "fire", HullPct: 100, ShieldPct: 50},
	}}
	aCopy := &game.BattleState{Participants: []game.BattleParticipant{
		{PlayerID: "me", Zone: "mid", Stance: "fire", HullPct: 100, ShieldPct: 50},
	}}
	if battleSignature(a) != battleSignature(aCopy) {
		t.Fatal("signature must be stable for identical state")
	}
	b := &game.BattleState{Participants: []game.BattleParticipant{
		{PlayerID: "me", Zone: "mid", Stance: "fire", HullPct: 80, ShieldPct: 50},
	}}
	if battleSignature(a) == battleSignature(b) {
		t.Fatal("signature must change when hull changes")
	}
}
