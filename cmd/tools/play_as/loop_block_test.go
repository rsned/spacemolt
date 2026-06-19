package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestParseStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // expected Raw values, in order
	}{
		{"empty", "", nil},
		{"whitespace only", "  \n\t \n", nil},
		{"single newline-separated", "travel sol_belt\nmine\nrefuel",
			[]string{"travel sol_belt", "mine", "refuel"}},
		{"single semicolon-separated", "travel sol_belt; mine; refuel",
			[]string{"travel sol_belt", "mine", "refuel"}},
		{"mixed separators", "travel sol_belt\nmine; mine\nrefuel",
			[]string{"travel sol_belt", "mine", "mine", "refuel"}},
		{"trailing semicolon", "mine; refuel;",
			[]string{"mine", "refuel"}},
		{"semicolon in double quote", `chat "hi; there"; mine`,
			[]string{`chat "hi; there"`, "mine"}},
		{"semicolon in single quote", `chat 'hi; there'; mine`,
			[]string{`chat 'hi; there'`, "mine"}},
		{"newline in double quote", "chat \"hi\nthere\"\nmine",
			[]string{"chat \"hi\nthere\"", "mine"}},
		{"line comment stripped", "# first\nmine # trailing\nrefuel",
			[]string{"mine", "refuel"}},
		{"hash in quote preserved", `chat "#1 fan"; mine`,
			[]string{`chat "#1 fan"`, "mine"}},
		{"nested block kept intact", "loop 3 { mine; refuel }; dock",
			[]string{"loop 3 { mine; refuel }", "dock"}},
		{"nested block with newlines", "loop 3 {\n  mine\n  refuel\n}\ndock",
			[]string{"loop 3 {\n  mine\n  refuel\n}", "dock"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStatements(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d statements, want %d: %v", len(got), len(tc.want), rawOf(got))
			}
			for i, s := range got {
				if s.Raw != tc.want[i] {
					t.Errorf("stmt %d: Raw = %q, want %q", i, s.Raw, tc.want[i])
				}
			}
		})
	}
}

func TestParseStatements_UnbalancedBraces(t *testing.T) {
	if _, err := parseStatements("loop 3 { mine"); err == nil {
		t.Fatal("expected error for unbalanced braces, got nil")
	}
	if _, err := parseStatements("mine }"); err == nil {
		t.Fatal("expected error for stray close brace, got nil")
	}
}

func TestParseStatements_TokensPopulated(t *testing.T) {
	got, err := parseStatements("sell iron_ore 5")
	if err != nil || len(got) != 1 {
		t.Fatalf("parse failed: %v, got %d stmts", err, len(got))
	}
	want := []string{"sell", "iron_ore", "5"}
	if len(got[0].Tokens) != len(want) {
		t.Fatalf("Tokens len = %d, want %d", len(got[0].Tokens), len(want))
	}
	for i, tok := range got[0].Tokens {
		if tok != want[i] {
			t.Errorf("Tokens[%d] = %q, want %q", i, tok, want[i])
		}
	}
}

func rawOf(ss []Statement) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Raw
	}
	return out
}

func TestParseLoopHeader(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantCount int
		wantForce bool
		wantBody  string
		wantBlock bool
		wantErr   bool
	}{
		{"simple", "loop 5 mine", 5, false, "mine", false, false},
		{"force", "loop -f 10 mine", 10, true, "mine", false, false},
		{"multi-arg tail", "loop 3 sell iron_ore 5", 3, false, "sell iron_ore 5", false, false},
		{"block", "loop 20 { mine; refuel }", 20, false, " mine; refuel ", true, false},
		{"force block", "loop -f 5 { mine }", 5, true, " mine ", true, false},
		{"block newline body", "loop 3 {\n  mine\n  refuel\n}", 3, false, "\n  mine\n  refuel\n", true, false},
		{"no count", "loop mine", 0, false, "", false, true},
		{"bad count", "loop xx mine", 0, false, "", false, true},
		{"zero count", "loop 0 mine", 0, false, "", false, true},
		{"missing body", "loop 5", 0, false, "", false, true},
		{"missing body after -f", "loop -f 5", 0, false, "", false, true},
		{"unclosed block", "loop 5 { mine", 0, false, "", false, true},
		{"missing count before brace", "loop { mine }", 0, false, "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stmt := Statement{Raw: tc.in, Tokens: splitArgs(tc.in)}
			count, force, body, isBlock, err := parseLoopHeader(stmt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got count=%d body=%q", count, body)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if count != tc.wantCount {
				t.Errorf("count = %d, want %d", count, tc.wantCount)
			}
			if force != tc.wantForce {
				t.Errorf("force = %v, want %v", force, tc.wantForce)
			}
			if body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
			if isBlock != tc.wantBlock {
				t.Errorf("isBlock = %v, want %v", isBlock, tc.wantBlock)
			}
		})
	}
}

func TestParseLoopHeader_MissingCountBraceMessage(t *testing.T) {
	stmt := Statement{Raw: "loop { mine }", Tokens: splitArgs("loop { mine }")}
	_, _, _, _, err := parseLoopHeader(stmt)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing count") || !strings.Contains(err.Error(), "{") {
		t.Errorf("error should mention missing count and the brace, got: %v", err)
	}
}

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

func mustParseStmts(t *testing.T, body string) []Statement {
	t.Helper()
	s, err := parseStatements(body)
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

func TestScanBraceDepth(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantDepth int
		wantQuote bool
	}{
		{"empty", "", 0, false},
		{"balanced", "loop 5 { mine }", 0, false},
		{"open", "loop 5 { mine", 1, false},
		{"nested", "loop 2 { loop 3 { mine", 2, false},
		{"nested closed", "loop 2 { loop 3 { mine } }", 0, false},
		{"brace in double quote", `chat "}"`, 0, false},
		{"brace in single quote", `chat '}'`, 0, false},
		{"open brace in quote doesn't count", `chat "{" `, 0, false},
		{"unterminated quote", `chat "hi`, 0, true},
		{"close without open", "mine }", -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			depth, inQuote := scanBraceDepth(tc.in)
			if depth != tc.wantDepth {
				t.Errorf("depth: got %d, want %d", depth, tc.wantDepth)
			}
			if inQuote != tc.wantQuote {
				t.Errorf("inQuote: got %v, want %v", inQuote, tc.wantQuote)
			}
		})
	}
}

func TestHasTopLevelOpenBrace(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"loop 5 mine", false},
		{"loop 5 { mine }", true},
		{`loop 5 chat "hi { there"`, false},
		{`loop 5 chat 'hi { there'`, false},
		{"", false},
		{"{", true},
		{`"{" then {`, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := hasTopLevelOpenBrace(tc.in); got != tc.want {
				t.Errorf("hasTopLevelOpenBrace(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBlockPreview(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single", "mine", "mine"},
		{"multi", "travel sol_belt; mine; dock", "travel sol_belt; mine; dock"},
		{"newlines collapsed", "travel sol_belt\n  mine\n  dock", "travel sol_belt; mine; dock"},
		{"truncation", strings.Repeat("mine; ", 20), strings.Repeat("mine; ", 10) + "…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stmts, err := parseStatements(tc.in)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := blockPreview(stmts); got != tc.want {
				t.Errorf("blockPreview = %q, want %q", got, tc.want)
			}
		})
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
	body := []Statement{
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
	body := []Statement{
		{Raw: "loop -f 3 { travel $FOO$ }", Tokens: []string{"loop", "-f", "3", "{", "travel", "$FOO$", "}"}},
	}
	err := executeLoop(context.Background(), io.Discard, 2, true, body, 0, runStatement)
	var te *tokenError
	if !errors.As(err, &te) {
		t.Fatalf("expected *tokenError to propagate out of nested loop, got %v", err)
	}
}
