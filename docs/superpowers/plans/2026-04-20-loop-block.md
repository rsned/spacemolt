# `loop` Block Syntax Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the `play_as` REPL's `loop` meta-command to accept a braced body of commands (newline- or semicolon-separated) with arbitrary nesting.

**Architecture:** Three concerns in a new file `cmd/tools/play_as/loop_block.go`: (1) a quote-aware brace-depth scanner used by the REPL to decide when multi-line input is complete, (2) a statement splitter that breaks a block body on top-level `;` and `\n`, preserving nested `{...}` verbatim, and (3) a recursive `executeLoop` that uses an injected dispatcher so it is unit-testable without a game client. `main.go`'s existing `loop` branch gains a block-form path triggered by seeing `{` in the input; the existing single-command form is untouched.

**Tech Stack:** Go 1.24, `github.com/peterh/liner` for REPL input (already in use). Standard library only for the new code.

**Design spec:** `docs/superpowers/specs/2026-04-20-loop-block-design.md`

**Parser consistency note:** The existing `splitArgs` (`main.go:3987`) supports `"` and `'` quoting but does NOT handle backslash escapes. The new scanner/parser matches this exactly — no `\"` escape — to avoid divergent quote handling. Inside quotes everything is literal (including `{`, `}`, `;`, `\n`, `#`).

---

## Task 1: Brace-Depth Scanner

Quote-aware scanner that reports net `{`/`}` depth. Used by the REPL to decide when a multi-line block is complete.

**Files:**
- Create: `cmd/tools/play_as/loop_block.go`
- Test: `cmd/tools/play_as/loop_block_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/tools/play_as/loop_block_test.go`:

```go
package main

import "testing"

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
			depth, inQuote, err := scanBraceDepth(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if depth != tc.wantDepth {
				t.Errorf("depth: got %d, want %d", depth, tc.wantDepth)
			}
			if inQuote != tc.wantQuote {
				t.Errorf("inQuote: got %v, want %v", inQuote, tc.wantQuote)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./cmd/tools/play_as/ -run TestScanBraceDepth -v`
Expected: compile error — `scanBraceDepth` undefined.

- [ ] **Step 3: Create `loop_block.go` with the scanner**

Create `cmd/tools/play_as/loop_block.go`:

```go
package main

// scanBraceDepth reports the net brace depth of s and whether the scan
// ended inside a quoted string. Braces inside '"..."' or "'...'" are
// ignored. Returns a non-nil error only for malformed inputs that cannot
// occur from valid REPL text (reserved for future extension).
func scanBraceDepth(s string) (depth int, inQuote bool, err error) {
	var quoteRune rune
	for _, r := range s {
		if quoteRune != 0 {
			if r == quoteRune {
				quoteRune = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quoteRune = r
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth, quoteRune != 0, nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

Run: `go test ./cmd/tools/play_as/ -run TestScanBraceDepth -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/loop_block.go cmd/tools/play_as/loop_block_test.go
git commit -m "feat(play_as): add brace-depth scanner for loop blocks"
```

---

## Task 2: Statement Splitter

Splits a block body into top-level statements. Separators are `;` and `\n` at depth 0 outside quotes. Line comments start with `#`. Nested `{...}` preserved verbatim.

**Files:**
- Modify: `cmd/tools/play_as/loop_block.go`
- Modify: `cmd/tools/play_as/loop_block_test.go`

- [ ] **Step 1: Add failing tests**

Append to `cmd/tools/play_as/loop_block_test.go`:

```go
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
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./cmd/tools/play_as/ -run TestParseStatements -v`
Expected: compile error — `Statement`, `parseStatements` undefined.

- [ ] **Step 3: Implement `Statement` and `parseStatements`**

Append to `cmd/tools/play_as/loop_block.go`:

```go
import (
	"fmt"
	"strings"
)

// Statement is one top-level command inside a loop body. Raw is the
// original string (with surrounding whitespace trimmed); Tokens is the
// splitArgs result over Raw.
type Statement struct {
	Raw    string
	Tokens []string
}

// parseStatements splits body into top-level statements, separating on
// ';' and '\n' at brace-depth 0 outside quotes. '#' begins a line
// comment (to end-of-line) outside quotes. Nested '{...}' content is
// preserved verbatim in the enclosing Statement's Raw.
func parseStatements(body string) ([]Statement, error) {
	var out []Statement
	var cur strings.Builder
	var quoteRune rune
	depth := 0
	inComment := false

	flush := func() {
		raw := strings.TrimSpace(cur.String())
		cur.Reset()
		if raw == "" {
			return
		}
		out = append(out, Statement{Raw: raw, Tokens: splitArgs(raw)})
	}

	for _, r := range body {
		if inComment {
			if r == '\n' {
				inComment = false
				// treat the newline as a separator
				flush()
			}
			continue
		}
		if quoteRune != 0 {
			cur.WriteRune(r)
			if r == quoteRune {
				quoteRune = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quoteRune = r
			cur.WriteRune(r)
		case '#':
			if depth == 0 {
				inComment = true
			} else {
				cur.WriteRune(r)
			}
		case '{':
			depth++
			cur.WriteRune(r)
		case '}':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unexpected '}' with no matching '{'")
			}
			cur.WriteRune(r)
		case ';', '\n':
			if depth == 0 {
				flush()
			} else {
				cur.WriteRune(r)
			}
		default:
			cur.WriteRune(r)
		}
	}
	if quoteRune != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced braces (depth=%d at end of input)", depth)
	}
	flush()
	return out, nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

Run: `go test ./cmd/tools/play_as/ -run TestParseStatements -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/loop_block.go cmd/tools/play_as/loop_block_test.go
git commit -m "feat(play_as): add statement splitter for loop block bodies"
```

---

## Task 3: Loop Header Parser

Given a `Statement` whose first token is `loop`, extract `count`, `force`, and the body — either a single-command tail (existing form) or the content between outermost braces (new block form).

**Files:**
- Modify: `cmd/tools/play_as/loop_block.go`
- Modify: `cmd/tools/play_as/loop_block_test.go`

- [ ] **Step 1: Add failing tests**

Append to `cmd/tools/play_as/loop_block_test.go`:

```go
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
		{"block", "loop 20 { mine; refuel }", 20, false, "mine; refuel", true, false},
		{"force block", "loop -f 5 { mine }", 5, true, "mine", true, false},
		{"block newline body", "loop 3 {\n  mine\n  refuel\n}", 3, false, "\n  mine\n  refuel\n", true, false},
		{"no count", "loop mine", 0, false, "", false, true},
		{"bad count", "loop xx mine", 0, false, "", false, true},
		{"zero count", "loop 0 mine", 0, false, "", false, true},
		{"missing body", "loop 5", 0, false, "", false, true},
		{"missing body after -f", "loop -f 5", 0, false, "", false, true},
		{"unclosed block", "loop 5 { mine", 0, false, "", false, true},
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
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./cmd/tools/play_as/ -run TestParseLoopHeader -v`
Expected: compile error — `parseLoopHeader` undefined.

- [ ] **Step 3: Implement `parseLoopHeader`**

Append to `cmd/tools/play_as/loop_block.go`:

```go
import "strconv"

// parseLoopHeader parses the header of a "loop" statement. It accepts
// the existing single-command form (e.g. "loop 5 mine") and the new
// block form (e.g. "loop 5 { mine; refuel }"). For the block form the
// returned body is the content between the outermost matching braces;
// for the single form it is the concatenation of remaining args.
func parseLoopHeader(stmt Statement) (count int, force bool, body string, isBlock bool, err error) {
	tokens := stmt.Tokens
	if len(tokens) == 0 || strings.ToLower(tokens[0]) != "loop" {
		return 0, false, "", false, fmt.Errorf("not a loop statement")
	}
	idx := 1
	if idx < len(tokens) && tokens[idx] == "-f" {
		force = true
		idx++
	}
	if idx >= len(tokens) {
		return 0, false, "", false, fmt.Errorf("loop: missing count")
	}
	n, cerr := strconv.Atoi(tokens[idx])
	if cerr != nil || n < 1 {
		return 0, false, "", false, fmt.Errorf("loop: invalid count %q (must be a positive integer)", tokens[idx])
	}
	count = n
	idx++
	if idx >= len(tokens) {
		return 0, false, "", false, fmt.Errorf("loop: missing command(s)")
	}

	// Determine where the header ends in stmt.Raw so we can inspect the
	// remainder for a block vs single-command tail. Find the position
	// after the count token in Raw by advancing token-by-token.
	rest, ferr := afterTokens(stmt.Raw, tokens[:idx])
	if ferr != nil {
		return 0, false, "", false, ferr
	}
	rest = strings.TrimLeft(rest, " \t")

	if strings.HasPrefix(rest, "{") {
		inner, ok := extractBracedBody(rest)
		if !ok {
			return 0, false, "", false, fmt.Errorf("loop: unclosed '{' in block body")
		}
		return count, force, inner, true, nil
	}
	return count, force, rest, false, nil
}

// afterTokens returns the substring of raw that begins after the last
// character of the final token in tokens (token boundaries determined
// by splitArgs-compatible whitespace scanning with quote awareness).
func afterTokens(raw string, tokens []string) (string, error) {
	pos := 0
	for _, want := range tokens {
		// skip whitespace
		for pos < len(raw) && (raw[pos] == ' ' || raw[pos] == '\t') {
			pos++
		}
		// read one token with quote awareness; must equal want
		start := pos
		var quoteRune byte
		var got strings.Builder
		for pos < len(raw) {
			c := raw[pos]
			if quoteRune != 0 {
				if c == quoteRune {
					quoteRune = 0
				} else {
					got.WriteByte(c)
				}
				pos++
				continue
			}
			if c == '"' || c == '\'' {
				quoteRune = c
				pos++
				continue
			}
			if c == ' ' || c == '\t' {
				break
			}
			got.WriteByte(c)
			pos++
		}
		if got.String() != want {
			return "", fmt.Errorf("afterTokens: expected %q at %d, got %q", want, start, got.String())
		}
	}
	return raw[pos:], nil
}

// extractBracedBody assumes s starts with '{' and returns the content
// between that '{' and its matching '}', respecting quotes and nesting.
// Returns ok=false if no matching '}' exists.
func extractBracedBody(s string) (body string, ok bool) {
	if len(s) == 0 || s[0] != '{' {
		return "", false
	}
	depth := 0
	var quoteRune rune
	for i, r := range s {
		if quoteRune != 0 {
			if r == quoteRune {
				quoteRune = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quoteRune = r
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run tests — verify they pass**

Run: `go test ./cmd/tools/play_as/ -run TestParseLoopHeader -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/loop_block.go cmd/tools/play_as/loop_block_test.go
git commit -m "feat(play_as): add loop header parser with block support"
```

---

## Task 4: Recursive Loop Executor

Executes a parsed body N times, recursing into nested `loop` statements. Uses an injected dispatcher so tests run without a game client.

**Files:**
- Modify: `cmd/tools/play_as/loop_block.go`
- Modify: `cmd/tools/play_as/loop_block_test.go`

- [ ] **Step 1: Add failing tests**

Append to `cmd/tools/play_as/loop_block_test.go`:

```go
import (
	"context"
	"errors"
	"io"
)

// recordingDispatcher returns a dispatch func that records each call and
// returns the next scripted error. Script index wraps with nil if
// exhausted.
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
	// First call should be "mine", second "refuel", etc.
	expected := []string{"mine", "refuel", "mine", "refuel", "mine", "refuel"}
	for i, got := range *calls {
		if got[0] != expected[i] {
			t.Errorf("call %d: got %q, want %q", i, got[0], expected[i])
		}
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
	// Verify sequence in first iteration.
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
	// Fail on the 2nd call (refuel in iter 1).
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
	// Fail every call.
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
	// Inner loop has -f so its errors are swallowed; outer (no -f) never sees them.
	body := mustParseStmts(t, "loop -f 3 mine; dock")
	boom := errors.New("boom")
	// Every mine fails; dock should still run.
	script := []error{boom, boom, boom, nil, boom, boom, boom, nil}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 2, false, body, 0, dispatch)
	if err != nil {
		t.Fatalf("outer should complete, got %v", err)
	}
	// 2 outer × (3 mine + 1 dock) = 8 calls.
	if len(*calls) != 8 {
		t.Errorf("expected 8 calls, got %d", len(*calls))
	}
}

func TestExecuteLoop_InnerNoForceAbortsInnerPropagates(t *testing.T) {
	// Inner loop without -f; first mine fails; inner returns error;
	// outer (no -f) then aborts.
	body := mustParseStmts(t, "loop 3 mine; dock")
	boom := errors.New("boom")
	script := []error{boom}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 2, false, body, 0, dispatch)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	// Only the first mine should have run.
	if len(*calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(*calls))
	}
}

func TestExecuteLoop_OuterForceCatchesInnerError(t *testing.T) {
	// Inner loop without -f raises an error; outer has -f and continues.
	body := mustParseStmts(t, "loop 3 mine; dock")
	boom := errors.New("boom")
	// Iter 1: mine fails (inner aborts). Iter 2: mine fails again.
	script := []error{boom, boom}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 2, true, body, 0, dispatch)
	if err != nil {
		t.Fatalf("outer -f should swallow, got %v", err)
	}
	// Iter 1: mine (fails → inner aborts → outer catches, skips dock, next iter).
	// Iter 2: mine (fails → same).
	if len(*calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(*calls))
	}
}
```

Also add these imports to the top of `loop_block_test.go` (merge with existing):

```go
import (
	"context"
	"errors"
	"io"
	"testing"
)
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./cmd/tools/play_as/ -run TestExecuteLoop -v`
Expected: compile error — `executeLoop` undefined.

- [ ] **Step 3: Implement `executeLoop`**

Append to `cmd/tools/play_as/loop_block.go`:

```go
import (
	"context"
	"io"
)

// executeLoop runs count iterations of body. For each statement whose
// first token is "loop", parseLoopHeader + parseStatements is applied
// and executeLoop recurses; otherwise runStatement is called. Each loop
// enforces errors according to its own force flag: a loop with force
// continues past errors and returns nil; a loop without force returns
// the first error. depth controls indentation of status lines (0 = no
// indent). out receives all human-readable status output.
func executeLoop(
	ctx context.Context,
	out io.Writer,
	count int,
	force bool,
	body []Statement,
	depth int,
	runStatement func(tokens []string) error,
) error {
	indent := strings.Repeat("  ", depth)
	var firstErr error
	errors := 0

	for i := 0; i < count; i++ {
		fmt.Fprintf(out, "%s── [%d/%d]\n", indent, i+1, count)
		for _, stmt := range body {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var err error
			if len(stmt.Tokens) > 0 && strings.ToLower(stmt.Tokens[0]) == "loop" {
				innerCount, innerForce, innerBody, isBlock, perr := parseLoopHeader(stmt)
				if perr != nil {
					err = perr
				} else {
					var innerStmts []Statement
					if isBlock {
						innerStmts, err = parseStatements(innerBody)
					} else {
						innerStmts = []Statement{{Raw: innerBody, Tokens: splitArgs(innerBody)}}
					}
					if err == nil {
						err = executeLoop(ctx, out, innerCount, innerForce, innerStmts, depth+1, runStatement)
					}
				}
			} else {
				err = runStatement(stmt.Tokens)
			}
			if err != nil {
				errors++
				fmt.Fprintf(out, "%s❌ %v\n", indent, err)
				if !force {
					fmt.Fprintf(out, "%sStopping loop after %d/%d iterations\n", indent, i+1, count)
					return err
				}
				if firstErr == nil {
					firstErr = err
				}
				// continue with next statement
			}
		}
		fmt.Fprintf(out, "%s✓ [%d/%d]\n", indent, i+1, count)
	}
	if force && errors > 0 {
		fmt.Fprintf(out, "%s🔁 Loop finished with %d error(s) out of %d iterations\n", indent, errors, count)
	}
	// With force, swallow errors.
	if force {
		return nil
	}
	return firstErr
}
```

Note: if the `strings` import hasn't been added at the top of `loop_block.go` yet (from Tasks 1–3), merge all needed imports into a single `import ( ... )` block now. After this task, the top of the file should read:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run tests — verify they pass**

Run: `go test ./cmd/tools/play_as/ -run TestExecuteLoop -v`
Expected: all PASS.

- [ ] **Step 5: Run full test suite for the package**

Run: `go test ./cmd/tools/play_as/ -v`
Expected: PASS for all tests (both new and existing).

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/loop_block.go cmd/tools/play_as/loop_block_test.go
git commit -m "feat(play_as): add recursive loop executor with nested-error semantics"
```

---

## Task 5: REPL Integration

Wire the block form into the existing REPL. Add multi-line reading with `... ` prompt, collapse blocks into single history entries, and dispatch to `executeLoop` when a `{` is present. Update help text.

**Files:**
- Modify: `cmd/tools/play_as/main.go` (specifically the `runREPL` function around line 263 and the `loop` branch around line 358, and `printHelp()` around line 4249)
- Modify: `cmd/tools/play_as/loop_block.go` (add `readLogicalCommand`)

- [ ] **Step 1: Add `readLogicalCommand` to `loop_block.go`**

Append to `cmd/tools/play_as/loop_block.go`:

```go
import "github.com/peterh/liner"

// readLogicalCommand reads a command from liner. If the first line has
// unbalanced '{' at top level (outside quotes), it continues reading
// additional lines with a "... " prompt until brace depth returns to 0,
// joining lines with '\n'. Returns the assembled script.
//
// A liner.ErrPromptAborted during continuation discards the partial
// block and returns ("", liner.ErrPromptAborted) so the caller can
// distinguish "user cancelled the block" from "EOF"; the caller should
// treat it as "return to $ prompt".
func readLogicalCommand(line *liner.State) (string, error) {
	first, err := line.Prompt("$ ")
	if err != nil {
		return "", err
	}
	depth, inQuote, _ := scanBraceDepth(first)
	if depth <= 0 && !inQuote {
		return first, nil
	}
	combined := first
	for depth > 0 || inQuote {
		more, perr := line.Prompt("... ")
		if perr != nil {
			// Ctrl-C (ErrPromptAborted) or other error — discard block.
			return "", perr
		}
		combined += "\n" + more
		depth, inQuote, _ = scanBraceDepth(combined)
		if depth < 0 {
			return combined, fmt.Errorf("unbalanced braces")
		}
	}
	return combined, nil
}
```

- [ ] **Step 2: Replace the main REPL read with `readLogicalCommand`**

In `cmd/tools/play_as/main.go`, locate the prompt read in `runREPL` (around line 264-273):

```go
		// Read input with history support (up/down arrows)
		input, err := line.Prompt("$ ")
		if err != nil {
			if err == liner.ErrPromptAborted {
				fmt.Println("Goodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}
```

Replace with:

```go
		// Read input with history support. A line ending with an
		// unbalanced '{' causes readLogicalCommand to continue with a
		// "... " prompt until braces balance.
		input, err := readLogicalCommand(line)
		if err != nil {
			if err == liner.ErrPromptAborted {
				// Ctrl-C at the main prompt exits; during a block
				// continuation, it returns here with an empty input,
				// so we distinguish by the input contents.
				if input == "" {
					fmt.Println("Goodbye!")
					return
				}
				fmt.Println("^C (block discarded)")
				continue
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}
```

- [ ] **Step 3: Collapse multi-line input for history and parsing**

Still in `runREPL`, locate the trim + history append (around line 275-283):

```go
		// Trim whitespace
		cmd := strings.TrimSpace(input)
		if cmd == "" {
			continue
		}

		// Add to history for up/down arrow cycling and persist
		line.AppendHistory(cmd)
		saveHistory()

		// Parse command (supports quoted strings)
		parts := splitArgs(cmd)
```

Replace with:

```go
		// Trim whitespace
		cmd := strings.TrimSpace(input)
		if cmd == "" {
			continue
		}

		// For history, collapse newlines so multi-line blocks appear as
		// a single semicolon-joined entry that can be recalled with one
		// up-arrow.
		historyEntry := strings.ReplaceAll(cmd, "\n", "; ")
		line.AppendHistory(historyEntry)
		saveHistory()

		// For parsing, splitArgs handles only the first-line tokens;
		// the block path below uses parseStatements on the full input.
		firstLine := cmd
		if nl := strings.IndexByte(firstLine, '\n'); nl >= 0 {
			firstLine = firstLine[:nl]
		}
		parts := splitArgs(firstLine)
```

- [ ] **Step 4: Replace the `loop` branch to dispatch block form**

In `cmd/tools/play_as/main.go`, locate the entire `if command == "loop" { ... }` block (currently lines 358-418). Replace with:

```go
		// Handle loop meta-command.
		// Single form:  loop [-f] <count> <command...>
		// Block form:   loop [-f] <count> { stmt; stmt; ... }
		// Block bodies may contain newlines or semicolons as
		// separators and may nest "loop" recursively.
		if command == "loop" {
			// Detect block form by looking for a top-level '{' in the
			// full (possibly multi-line) input.
			if depth, _, _ := scanBraceDepth(cmd); depth == 0 && !strings.Contains(cmd, "{") {
				// Fall through to the legacy single-command path.
				if err := runLoopSingle(client, ctx, parts, format); err != nil {
					// runLoopSingle already printed its own errors.
					_ = err
				}
				if sl := renderStatusline(client, cfg, agentID); sl != "" {
					fmt.Println(sl)
				}
				fmt.Println()
				continue
			}
			// Block form. Wrap the full input as a single Statement and
			// parse the header to get count/force/body.
			stmt := Statement{Raw: cmd, Tokens: splitArgs(firstLine)}
			count, force, body, isBlock, err := parseLoopHeader(stmt)
			if err != nil {
				fmt.Printf("❌ %v\n\n", err)
				continue
			}
			if !isBlock {
				// Shouldn't happen — we detected '{' above.
				fmt.Printf("❌ loop: expected block body\n\n")
				continue
			}
			stmts, err := parseStatements(body)
			if err != nil {
				fmt.Printf("❌ %v\n\n", err)
				continue
			}
			if len(stmts) == 0 {
				fmt.Println("❌ loop: empty block")
				fmt.Println()
				continue
			}
			if force {
				fmt.Printf("🔁 Repeating block %d time(s) (force mode)...\n", count)
			} else {
				fmt.Printf("🔁 Repeating block %d time(s)...\n", count)
			}
			runStatement := func(tokens []string) error {
				return executeCommand(client, ctx, tokens, format)
			}
			_ = executeLoop(ctx, os.Stdout, count, force, stmts, 0, runStatement)
			if sl := renderStatusline(client, cfg, agentID); sl != "" {
				fmt.Println(sl)
			}
			fmt.Println()
			continue
		}
```

- [ ] **Step 5: Extract the legacy single-command loop into `runLoopSingle`**

Still in `main.go`, add this function just below `runREPL` (or anywhere convenient at package scope). It contains the existing single-command loop logic verbatim, extracted for reuse:

```go
// runLoopSingle handles the legacy "loop [-f] <count> <command...>"
// form with a single command. Returns nil on success; errors are
// printed to stdout but not returned to the caller since the REPL
// already handles continuation.
func runLoopSingle(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	if len(parts) < 3 {
		fmt.Println("Usage: loop [-f] <count> <command...>")
		fmt.Println("       loop [-f] <count> { stmt; stmt; ... }")
		fmt.Println("  -f  Force: continue on errors instead of stopping")
		fmt.Println("Examples: loop 5 mine")
		fmt.Println("          loop -f 20 mine")
		fmt.Println("          loop 10 sell iron_ore 5")
		fmt.Println("          loop 3 { travel sol_belt; mine; mine; dock }")
		fmt.Println()
		return nil
	}
	forceLoop := false
	argIdx := 1
	if parts[argIdx] == "-f" {
		forceLoop = true
		argIdx++
		if argIdx >= len(parts)-1 {
			fmt.Println("Usage: loop [-f] <count> <command...>")
			fmt.Println()
			return nil
		}
	}
	count, err := strconv.Atoi(parts[argIdx])
	if err != nil || count < 1 {
		fmt.Printf("❌ Invalid count: %s (must be a positive integer)\n\n", parts[argIdx])
		return nil
	}
	loopParts := parts[argIdx+1:]
	loopCmd := strings.Join(loopParts, " ")
	if forceLoop {
		fmt.Printf("🔁 Repeating %q %d time(s) (force mode)...\n", loopCmd, count)
	} else {
		fmt.Printf("🔁 Repeating %q %d time(s)...\n", loopCmd, count)
	}
	errors := 0
	for i := range count {
		fmt.Printf("── [%d/%d] %s\n", i+1, count, loopCmd)
		startTime := time.Now()
		if cerr := executeCommand(client, ctx, loopParts, format); cerr != nil {
			errors++
			fmt.Printf("❌ %s\n", formatError(cerr, loopParts[0], format))
			if !forceLoop {
				fmt.Printf("Stopping loop after %d/%d iterations\n", i+1, count)
				break
			}
			fmt.Printf("⚠️  Error %d (continuing due to -f)...\n", errors)
			continue
		}
		duration := time.Since(startTime)
		fmt.Printf("✓ [%d/%d] Completed in %v\n", i+1, count, duration)
	}
	if forceLoop && errors > 0 {
		fmt.Printf("🔁 Loop finished with %d error(s) out of %d iterations\n", errors, count)
	}
	return nil
}
```

- [ ] **Step 6: Update `printHelp`**

In `cmd/tools/play_as/main.go`, locate the `loop` line in `printHelp()` (line 4249):

```go
	fmt.Println("  loop [-f] <count> <command> - Repeat a command N times (-f to continue on errors)")
```

Replace with:

```go
	fmt.Println("  loop [-f] <count> <command>        - Repeat a command N times (-f continues on errors)")
	fmt.Println("  loop [-f] <count> { stmt; stmt; ... } - Repeat a block; stmts may nest and use newlines or ';'")
```

- [ ] **Step 7: Build the binary**

Run: `go build ./cmd/tools/play_as/`
Expected: no errors, no output.

- [ ] **Step 8: Run the package test suite**

Run: `go test ./cmd/tools/play_as/ -v`
Expected: all tests PASS (both new `loop_block` tests and the pre-existing ones).

- [ ] **Step 9: Run a repo-wide build and test**

Run: `go build ./...`
Expected: no errors.

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 10: Run linter**

Run: `golangci-lint run ./cmd/tools/play_as/...`
Expected: no new findings.

If golangci-lint reports issues, fix them before committing.

- [ ] **Step 11: Commit**

```bash
git add cmd/tools/play_as/loop_block.go cmd/tools/play_as/main.go
git commit -m "feat(play_as): wire loop block syntax into REPL"
```

---

## Task 6: Manual Smoke Test

Verify the REPL UX end-to-end with a real agent connection. This cannot be automated because it exercises `liner` prompting and WebSocket round-trips.

**Files:** None modified.

- [ ] **Step 1: Start `play_as` against an available agent**

Pick any agent with credentials configured. Run:

```bash
go run ./cmd/tools/play_as/ <agent-id>
```

- [ ] **Step 2: Confirm single-command form still works**

At the `$ ` prompt type:

```
loop 3 get_status
```

Expected: 3 iterations of `get_status`, each printing the response, then returning to `$ `.

- [ ] **Step 3: Confirm one-line block form works**

At the `$ ` prompt type:

```
loop 2 { get_status; get_ship }
```

Expected: 2 iterations, each running `get_status` then `get_ship`.

- [ ] **Step 4: Confirm multi-line block form works**

At the `$ ` prompt type (note no `}` on first line):

```
loop 2 {
```

Expected: prompt changes to `... `. Then type:

```
  get_status
  get_ship
}
```

Expected: after the closing `}`, prompt returns to `$ ` and the block executes twice.

- [ ] **Step 5: Confirm nested loops work**

```
loop 2 {
  get_status
  loop 3 get_ship
}
```

Expected: 2 outer iterations. Each prints `get_status` once then `get_ship` three times. Nested iteration status lines are indented.

- [ ] **Step 6: Confirm Ctrl-C during `... ` discards the block**

Type `loop 5 {` then press Enter, then Ctrl-C at the `... ` prompt.

Expected: `^C (block discarded)` message, return to `$ ` prompt, REPL does not exit.

- [ ] **Step 7: Confirm history recall**

Press up-arrow at the `$ ` prompt. The most recent multi-line block should appear as a semicolon-joined single-line entry.

- [ ] **Step 8: Confirm `help` shows block form**

Type `help`. Expected: two lines describing `loop` forms (single and block).

- [ ] **Step 9: Exit**

Type `exit`.

- [ ] **Step 10: Record any observations**

If any smoke-test step fails, note the behavior and return to Task 5 to fix before proceeding. If all pass, proceed to Task 7.

---

## Task 7: Final Commit Cleanup

- [ ] **Step 1: Verify there are no uncommitted changes**

Run: `git status`
Expected: working tree clean (or only unrelated prior changes that were already `M` at the start).

- [ ] **Step 2: View the new commits for this feature**

Run: `git log --oneline main..HEAD` (or equivalent range for the current branch)
Expected: 5 commits — brace scanner, statement splitter, header parser, executor, REPL integration. Plus the earlier spec commit.

The feature is complete.

---

## Self-Review Notes

**Spec coverage** — verified:
- Three user-facing syntax forms → Tasks 1–5 (parsing + REPL).
- Multi-line reading with `... ` prompt → Task 5 (`readLogicalCommand`).
- Block parsing (newlines/`;`, comments, quote awareness) → Task 2.
- Arbitrary nesting → Task 4 (recursive `executeLoop`).
- Error semantics per-loop `-f` flag → Task 4 tests cover all four quadrants.
- Statusline rendered once at outermost loop → Task 5.
- History collapse to single entry → Task 5 Step 3.
- Ctrl-C during block discards → Task 5 Step 2 + Task 6 smoke test.
- `printHelp()` update → Task 5 Step 6.
- Nested indentation of status lines → Task 4 `depth` parameter.

**Parser consistency deviation noted:** spec section "State Machine" mentioned `\"` escapes inside double quotes; plan omits them to match existing `splitArgs` behavior exactly. Called out at the top of this plan.

**Type consistency** — `Statement`, `parseStatements`, `parseLoopHeader`, `executeLoop`, `scanBraceDepth`, `readLogicalCommand`, `runLoopSingle`, `extractBracedBody`, `afterTokens` — all names match across tasks.
