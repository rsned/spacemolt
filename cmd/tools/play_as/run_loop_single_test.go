package main

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/worker"
)

// stubClientNilState implements the minimum of game.GameClient needed by
// runLoopSingle. GetState returns nil, which causes resolveTokens to fail
// immediately with a *tokenError for any $TOKEN$ argument — no game server
// calls are made. Any other method invocation panics via the embedded
// interface, which is intentional: if a test inadvertently reaches real game
// logic the panic is a clear signal to update the stub.
type stubClientNilState struct {
	game.GameClient
}

func (stubClientNilState) GetState() *game.State    { return nil }
func (stubClientNilState) GetRawJSON(string) []byte { return nil }

// TestRunLoopSingle_TokenErrorStopsScript verifies that a *tokenError
// (unresolvable $TOKEN$ in a single-form loop) causes runLoopSingle to return
// a non-nil error, so the enclosing script halts.
func TestRunLoopSingle_TokenErrorStopsScript(t *testing.T) {
	client := stubClientNilState{}
	// "travel $STATION$" — $STATION$ cannot be resolved because GetState()
	// returns nil, so executeCommand returns a *tokenError immediately.
	parts := []string{"loop", "3", "travel", "$STATION$"}
	err := runLoopSingle(client, context.Background(), parts, formatRaw)
	if err == nil {
		t.Fatal("runLoopSingle: expected non-nil error for *tokenError, got nil")
	}
	var tokErr *worker.TokenError
	if !errors.As(err, &tokErr) {
		t.Fatalf("expected *worker.TokenError, got %T: %v", err, err)
	}
}

// TestRunLoopSingle_NonForceFailureStopsScript verifies that a regular command
// failure in a non-force loop causes runLoopSingle to return the error.
// We use "sleep -1" which returns an error from arg validation alone
// (no game-server call needed).
func TestRunLoopSingle_NonForceFailureStopsScript(t *testing.T) {
	client := stubClientNilState{}
	parts := []string{"loop", "3", "sleep", "-1"}
	err := runLoopSingle(client, context.Background(), parts, formatRaw)
	if err == nil {
		t.Fatal("runLoopSingle: expected non-nil error for non-force failure, got nil")
	}
}

// TestRunLoopSingle_ForceLoopReturnsNilOnErrors verifies that force-mode (-f)
// continues through errors and returns nil (script should keep running).
func TestRunLoopSingle_ForceLoopReturnsNilOnErrors(t *testing.T) {
	client := stubClientNilState{}
	parts := []string{"loop", "-f", "2", "sleep", "-1"}
	err := runLoopSingle(client, context.Background(), parts, formatRaw)
	if err != nil {
		t.Fatalf("runLoopSingle with -f: expected nil error, got %v", err)
	}
}

// TestRunLoopSingle_GoalReachedReturnsNil verifies that a *GoalReachedError
// exits the loop but does NOT propagate as a stopping error (the script
// should continue to the next command).
func TestRunLoopSingle_GoalReachedReturnsNil(t *testing.T) {
	// We cannot drive a *GoalReachedError through executeCommand without a
	// real game server, so we verify the nil-state token-error case works and
	// trust the existing executeLoop tests for GoalReached handling, since
	// runLoopSingle shares the same per-iteration error-dispatch logic.
	t.Skip("GoalReachedError path requires a real game client; covered by loop_block_test.go")
}

// TestRunLoopSingle_ValidationErrorsReturnNil verifies that bad-argument
// paths (too few parts, bad count) return nil — these are user errors, not
// script-stopping conditions.
func TestRunLoopSingle_ValidationErrorsReturnNil(t *testing.T) {
	client := stubClientNilState{}

	// Too few parts (< 3).
	if err := runLoopSingle(client, context.Background(), []string{"loop", "3"}, formatRaw); err != nil {
		t.Errorf("too-few-parts: expected nil, got %v", err)
	}
	// Invalid count.
	if err := runLoopSingle(client, context.Background(), []string{"loop", "abc", "mine"}, formatRaw); err != nil {
		t.Errorf("bad-count: expected nil, got %v", err)
	}
	// -f with no command after count.
	if err := runLoopSingle(client, context.Background(), []string{"loop", "-f", "3"}, formatRaw); err != nil {
		t.Errorf("-f no-cmd: expected nil, got %v", err)
	}
}
