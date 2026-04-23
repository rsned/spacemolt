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

func TestSimpleCommand_GoalReachedReturnsNil(t *testing.T) {
	client := stubGameClientForSimple{}
	fn := func(context.Context) error {
		return &game.GoalReachedError{
			Command: "mine",
			Code:    "no_cargo_space",
			Message: "Cargo hold is full",
		}
	}
	err := simpleCommand(client, fn, context.Background(), 0, "mine", formatRaw)
	if err != nil {
		t.Fatalf("simpleCommand should return nil on GoalReachedError, got %v", err)
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
