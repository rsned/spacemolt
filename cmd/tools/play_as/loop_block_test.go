package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/worker"
)

// recordingDispatcher returns a dispatch func that records each call and
// returns the next scripted error. nil entries mean success.
func recordingDispatcher(script []error) (func([]string) error, *[][]string) {
	var calls [][]string
	i := 0
	fn := func(tokens []string) error {
		cp := make([]string, len(tokens))
		copy(cp, tokens)
		calls = append(calls, cp)
		if i < len(script) {
			err := script[i]
			i++
			return err
		}
		return nil
	}
	return fn, &calls
}

func mustParseStmts(t *testing.T, body string) []worker.Statement {
	t.Helper()
	s, err := worker.ParseStatements(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return s
}

func TestExecuteLoop_RepeatsBody(t *testing.T) {
	body := mustParseStmts(t, "mine; refuel")
	dispatch, calls := recordingDispatcher(nil)
	err := executeLoop(context.Background(), io.Discard, 3, false, body, 0, dispatch)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(*calls) != 6 {
		t.Fatalf("expected 6 calls (3 iterations × 2 stmts), got %d", len(*calls))
	}
	expected := []string{"mine", "refuel", "mine", "refuel", "mine", "refuel"}
	for i, got := range *calls {
		if got[0] != expected[i] {
			t.Errorf("call %d: got %q, want %q", i, got[0], expected[i])
		}
	}
}

func TestExecuteLoop_ContextCancelStopsLoop(t *testing.T) {
	// Simulates Ctrl+C: the interrupter cancels the loop's context mid-run.
	// The loop must stop well before its count instead of running to the end.
	body := mustParseStmts(t, "mine")
	ctx, cancel := context.WithCancel(context.Background())
	ran := 0
	dispatch := func(tokens []string) error {
		ran++
		if ran == 2 {
			cancel()
		}
		return nil
	}
	err := executeLoop(ctx, io.Discard, 100, false, body, 0, dispatch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if ran >= 100 {
		t.Fatalf("loop ignored cancellation; ran %d/100 iterations", ran)
	}
}

func TestExecuteLoop_CanceledStatementErrIsCleanAbort(t *testing.T) {
	// A statement failing with context.Canceled (an interrupted in-flight
	// await) aborts the loop cleanly with the ⛔ notice, not a ❌ error line.
	body := mustParseStmts(t, "mine")
	dispatch := func(tokens []string) error { return context.Canceled }
	var out strings.Builder
	err := executeLoop(context.Background(), &out, 5, false, body, 0, dispatch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if s := out.String(); !strings.Contains(s, "⛔ interrupted") {
		t.Errorf("expected ⛔ interrupted notice, got:\n%s", s)
	}
	if s := out.String(); strings.Contains(s, "❌") {
		t.Errorf("interrupt must not print a ❌ error line, got:\n%s", s)
	}
}

func TestExecuteLoop_Nested(t *testing.T) {
	body := mustParseStmts(t, "travel sol_belt; loop 4 mine; dock")
	dispatch, calls := recordingDispatcher(nil)
	err := executeLoop(context.Background(), io.Discard, 2, false, body, 0, dispatch)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Each outer iteration: travel + 4× mine + dock = 6 calls. 2 iters = 12.
	if len(*calls) != 12 {
		t.Fatalf("expected 12 calls, got %d", len(*calls))
	}
	wantPrefix := []string{"travel", "mine", "mine", "mine", "mine", "dock"}
	for i, w := range wantPrefix {
		if (*calls)[i][0] != w {
			t.Errorf("call %d: got %q, want %q", i, (*calls)[i][0], w)
		}
	}
}

func TestExecuteLoop_NoForceAbortsOnError(t *testing.T) {
	body := mustParseStmts(t, "mine; refuel; dock")
	boom := errors.New("boom")
	dispatch, calls := recordingDispatcher([]error{nil, boom})
	err := executeLoop(context.Background(), io.Discard, 5, false, body, 0, dispatch)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(*calls) != 2 {
		t.Errorf("expected loop to stop after 2 calls, got %d", len(*calls))
	}
}

func TestExecuteLoop_ForceContinuesOnError(t *testing.T) {
	body := mustParseStmts(t, "mine; refuel")
	boom := errors.New("boom")
	script := []error{boom, boom, boom, boom, boom, boom}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 3, true, body, 0, dispatch)
	if err != nil {
		t.Fatalf("force loop should swallow errors, got %v", err)
	}
	if len(*calls) != 6 {
		t.Errorf("expected 6 calls even with errors, got %d", len(*calls))
	}
}

func TestExecuteLoop_InnerForceSwallowsOuterContinues(t *testing.T) {
	body := mustParseStmts(t, "loop -f 3 mine; dock")
	boom := errors.New("boom")
	script := []error{boom, boom, boom, nil, boom, boom, boom, nil}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 2, false, body, 0, dispatch)
	if err != nil {
		t.Fatalf("outer should complete, got %v", err)
	}
	if len(*calls) != 8 {
		t.Errorf("expected 8 calls, got %d", len(*calls))
	}
}

func TestExecuteLoop_InnerNoForceAbortsInnerPropagates(t *testing.T) {
	body := mustParseStmts(t, "loop 3 mine; dock")
	boom := errors.New("boom")
	script := []error{boom}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 2, false, body, 0, dispatch)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if len(*calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(*calls))
	}
}

func TestExecuteLoop_OuterForceCatchesInnerError(t *testing.T) {
	body := mustParseStmts(t, "loop 3 mine; dock")
	boom := errors.New("boom")
	// Each outer iteration: first mine fails → inner aborts → outer catches, skips dock.
	script := []error{boom, boom}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 2, true, body, 0, dispatch)
	if err != nil {
		t.Fatalf("outer -f should swallow, got %v", err)
	}
	if len(*calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(*calls))
	}
}

func TestExecuteLoop_GoalReachedExitsInnermost(t *testing.T) {
	// Body is a single "mine" statement. The dispatcher returns nil
	// for the first 4 calls, then *GoalReachedError on the 5th —
	// simulating cargo filling on iteration 5 of 20.
	body := mustParseStmts(t, "mine")
	script := []error{nil, nil, nil, nil, &game.GoalReachedError{
		Command: "mine",
		Code:    "no_cargo_space",
		Message: "Cargo hold is full",
	}}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 20, false, body, 0, dispatch)
	if err != nil {
		t.Fatalf("goal-reached should exit cleanly (nil), got %v", err)
	}
	// 5 calls total: 4 nil successes + the 1 goal-reached signal.
	if len(*calls) != 5 {
		t.Errorf("expected 5 calls (loop exits on goal-reached), got %d", len(*calls))
	}
}

func TestExecuteLoop_GoalReachedExitsInnerLoopOuterContinues(t *testing.T) {
	// Outer loop has 2 iterations; each runs: travel, inner loop of
	// up to 40 mine, dock. Inner mine goal-reaches on iter 3 of the
	// FIRST outer iteration AND iter 3 of the SECOND — so each outer
	// iteration produces 1 travel + 3 mine + 1 dock = 5 calls. Two
	// outer iterations = 10 calls. If the sentinel bled out to the
	// outer loop we'd see fewer than 10.
	body := mustParseStmts(t, "travel sol_belt; loop 40 mine; dock")
	goal := &game.GoalReachedError{Command: "mine", Code: "no_cargo_space", Message: "Cargo hold is full"}
	script := []error{
		// outer iter 1: travel ok; mine nil, nil, goal; dock ok
		nil, nil, nil, goal, nil,
		// outer iter 2: travel ok; mine nil, nil, goal; dock ok
		nil, nil, nil, goal, nil,
	}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 2, false, body, 0, dispatch)
	if err != nil {
		t.Fatalf("outer should succeed after inner goal-exits, got %v", err)
	}
	if len(*calls) != 10 {
		t.Errorf("expected 10 calls (2×(travel+3mine+dock)), got %d", len(*calls))
	}
	want := []string{"travel", "mine", "mine", "mine", "dock", "travel", "mine", "mine", "mine", "dock"}
	for i, w := range want {
		if (*calls)[i][0] != w {
			t.Errorf("call %d: got %q, want %q", i, (*calls)[i][0], w)
		}
	}
}

func TestExecuteLoop_GoalReachedIgnoresForceFlag(t *testing.T) {
	// -f only tolerates errors; goal-reached is a success and still
	// exits the innermost loop. `loop -f 20 mine` should stop on the
	// first goal-reached, not power through.
	body := mustParseStmts(t, "mine")
	goal := &game.GoalReachedError{Command: "mine", Code: "no_cargo_space", Message: "Cargo hold is full"}
	script := []error{nil, goal} // goal-reaches on iter 2
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 20, true /* force */, body, 0, dispatch)
	if err != nil {
		t.Fatalf("goal-reached under -f should exit cleanly, got %v", err)
	}
	if len(*calls) != 2 {
		t.Errorf("expected 2 calls (loop exits on goal-reached even with -f), got %d", len(*calls))
	}
}

func TestExecuteLoopTokenErrorAbortsUnderForce(t *testing.T) {
	// runStatement returns a *tokenError on the "travel" command. Even with
	// force=true, the loop must abort immediately and return that error.
	calls := 0
	runStatement := func(tokens []string) error {
		calls++
		if len(tokens) > 0 && tokens[0] == "travel" {
			return &tokenError{"no station POI in system Sol (sys-001)"}
		}
		return nil
	}
	body := []worker.Statement{
		{Raw: "mine", Tokens: []string{"mine"}},
		{Raw: "travel $STATION$", Tokens: []string{"travel", "$STATION$"}},
		{Raw: "mine", Tokens: []string{"mine"}},
	}
	err := executeLoop(context.Background(), io.Discard, 5, true, body, 0, runStatement)
	var te *tokenError
	if !errors.As(err, &te) {
		t.Fatalf("expected *tokenError, got %v", err)
	}
	// mine (1) + travel (2) on the first iteration only; must not continue.
	if calls != 2 {
		t.Fatalf("expected 2 statement calls before abort, got %d", calls)
	}
}

func TestExecuteLoopTokenErrorPropagatesThroughNestedLoop(t *testing.T) {
	runStatement := func(tokens []string) error {
		if len(tokens) > 0 && tokens[0] == "travel" {
			return &tokenError{"unknown token $FOO$"}
		}
		return nil
	}
	// Outer force loop containing an inner force loop whose body errors.
	body := []worker.Statement{
		{Raw: "loop -f 3 { travel $FOO$ }", Tokens: []string{"loop", "-f", "3", "{", "travel", "$FOO$", "}"}},
	}
	err := executeLoop(context.Background(), io.Discard, 2, true, body, 0, runStatement)
	var te *tokenError
	if !errors.As(err, &te) {
		t.Fatalf("expected *tokenError to propagate out of nested loop, got %v", err)
	}
}
