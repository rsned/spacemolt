package worker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

type fleeFake struct {
	calls []string
	err   error
}

func (f *fleeFake) Battle(_ context.Context, action string, p map[string]any) error {
	f.calls = append(f.calls, action+" "+p["stance"].(string))
	return f.err
}

// explorer-8, 2026-11-11: in battle at Oxbow Belt B, retrying "jump Chertan".
// The gate must flee and hold the command.
func TestBattleGateFleesAndHoldsCommand(t *testing.T) {
	st := &game.State{InBattle: true, CurrentPOI: "oxbow_belt_b"}
	st.Ship.Hull, st.Ship.MaxHull = 300, 900
	f := &fleeFake{}
	var out strings.Builder

	err := battleGate(context.Background(), &out, st, []string{"explore"}, f)
	if !errors.Is(err, ErrFleeingBattle) {
		t.Fatalf("err = %v, want ErrFleeingBattle", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "stance flee" {
		t.Errorf("battle calls = %v, want one flee stance", f.calls)
	}
	if !strings.Contains(out.String(), "in combat at oxbow_belt_b, hull 300/900 — fleeing") {
		t.Errorf("log = %q", out.String())
	}
}

func TestBattleGatePassesThrough(t *testing.T) {
	for name, tc := range map[string]struct {
		st     *game.State
		tokens []string
		bc     BattleClient
	}{
		"not in battle":  {&game.State{}, []string{"explore"}, &fleeFake{}},
		"battle command": {&game.State{InBattle: true}, []string{"battle", "stance", "fire"}, &fleeFake{}},
		"gate disarmed":  {&game.State{InBattle: true}, []string{"explore"}, nil},
		"no state":       {nil, []string{"explore"}, &fleeFake{}},
	} {
		if err := battleGate(context.Background(), io.Discard, tc.st, tc.tokens, tc.bc); err != nil {
			t.Errorf("%s: err = %v, want nil", name, err)
		}
	}
}

// A refused flee still holds the command: running it would be refused too.
func TestBattleGateHoldsWhenFleeFails(t *testing.T) {
	f := &fleeFake{err: errors.New("cannot flee yet")}
	err := battleGate(context.Background(), io.Discard, &game.State{InBattle: true}, []string{"mine"}, f)
	if !errors.Is(err, ErrFleeingBattle) {
		t.Errorf("err = %v, want ErrFleeingBattle", err)
	}
}

func TestBattleGateClientExemptsHunt(t *testing.T) {
	f := &fleeFake{}
	if BattleGateClient("hunt", f) != nil {
		t.Error("hunt role must not get the flee gate")
	}
}

// A stale in-battle flag must not hold the worker: the server's "not in a
// battle" refusal of the flee stance lets the command run.
func TestBattleGateRunsCommandWhenFlagStale(t *testing.T) {
	f := &fleeFake{err: errors.New("not_in_battle: You are not in a battle")}
	if err := battleGate(context.Background(), io.Discard, &game.State{InBattle: true}, []string{"mine"}, f); err != nil {
		t.Errorf("err = %v, want nil for a stale flag", err)
	}
}
