package main

import (
	"context"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// stubAttackClient records attack and battle calls. battleErr is what the
// battle command returns, to model a stale in-battle flag.
type stubAttackClient struct {
	game.GameClient
	state     *game.State
	attacks   []string
	battles   []map[string]any
	battleErr error
}

func (s *stubAttackClient) GetState() *game.State { return s.state }

func (s *stubAttackClient) Attack(_ context.Context, targetID string) error {
	s.attacks = append(s.attacks, targetID)

	return nil
}

func (s *stubAttackClient) Battle(_ context.Context, action string, payload map[string]any) error {
	p := map[string]any{"action": action}
	for k, v := range payload {
		p[k] = v
	}
	s.battles = append(s.battles, p)

	return s.battleErr
}

// Live 2026-11-21: `attack` typed mid-battle timed out after 3 ticks, and the
// REPL's command lock held reload and stance behind it for the whole wait.
// The server docs say a repeat attack inside a battle only re-points the
// target (and against pirates re-applies the rep penalty and summons more), so
// in battle it must go out as the instant `battle target`.
func TestAttackInBattleRetargets(t *testing.T) {
	c := &stubAttackClient{state: &game.State{InBattle: true}}
	if err := attackOrRetarget(context.Background(), c, "repeater_1", io.Discard); err != nil {
		t.Fatalf("attackOrRetarget: %v", err)
	}
	if len(c.attacks) != 0 {
		t.Fatalf("in battle must not send attack, sent %v", c.attacks)
	}
	if len(c.battles) != 1 || c.battles[0]["action"] != "target" || c.battles[0]["target_id"] != "repeater_1" {
		t.Fatalf("battles = %v, want one target repeater_1", c.battles)
	}
}

func TestAttackOutOfBattleAttacks(t *testing.T) {
	c := &stubAttackClient{state: &game.State{}}
	if err := attackOrRetarget(context.Background(), c, "repeater_1", io.Discard); err != nil {
		t.Fatalf("attackOrRetarget: %v", err)
	}
	if len(c.attacks) != 1 || len(c.battles) != 0 {
		t.Fatalf("attacks=%v battles=%v, want one attack and no battle", c.attacks, c.battles)
	}
}

// A stale in-battle flag (the fight already ended) gets not_in_battle back from
// the retarget; the attack must still go out rather than being swallowed.
func TestAttackStaleBattleFlagFallsBackToAttack(t *testing.T) {
	c := &stubAttackClient{
		state:     &game.State{InBattle: true},
		battleErr: &game.ServerError{Code: "not_in_battle", Message: "You are not in a battle"},
	}
	if err := attackOrRetarget(context.Background(), c, "repeater_1", io.Discard); err != nil {
		t.Fatalf("attackOrRetarget: %v", err)
	}
	if len(c.attacks) != 1 || c.attacks[0] != "repeater_1" {
		t.Fatalf("attacks = %v, want fallback attack on repeater_1", c.attacks)
	}
}
