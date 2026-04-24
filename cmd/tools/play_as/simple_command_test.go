package main

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// stubGameClientForSimple satisfies the tiny slice of game.GameClient
// that simpleCommand actually touches: GetRawJSON (for the raw-payload
// lookup paths) only. Everything else panics if invoked — those paths
// are out of scope for this test.
type stubGameClientForSimple struct {
	game.GameClient
}

func (stubGameClientForSimple) GetRawJSON(string) []byte { return nil }

func TestSimpleCommand_GoalReachedPropagates(t *testing.T) {
	// simpleCommand must propagate *game.GoalReachedError unchanged so the
	// loop executor and REPL dispatcher can both recognize and display it.
	// Previously simpleCommand swallowed the sentinel and returned nil,
	// which made the loop executor see "iteration succeeded" and keep
	// running pointless mine-on-full-cargo iterations.
	client := stubGameClientForSimple{}
	want := &game.GoalReachedError{
		Command: "mine",
		Code:    "no_cargo_space",
		Message: "Cargo hold is full",
	}
	fn := func(context.Context) error { return want }
	err := simpleCommand(client, fn, context.Background(), 0, "mine", formatRaw)
	var goal *game.GoalReachedError
	if !errors.As(err, &goal) {
		t.Fatalf("simpleCommand should propagate *GoalReachedError, got %T (%v)", err, err)
	}
	if goal.Code != "no_cargo_space" {
		t.Errorf("goal.Code = %q, want %q", goal.Code, "no_cargo_space")
	}
}

func TestSimpleCommand_PassesThroughRegularErrors(t *testing.T) {
	client := stubGameClientForSimple{}
	want := errors.New("boom")
	fn := func(context.Context) error { return want }
	err := simpleCommand(client, fn, context.Background(), 0, "mine", formatRaw)
	if err != want {
		t.Errorf("simpleCommand should pass regular errors through, got %v", err)
	}
}
