# play_as Loop Variable Tokens & Saved Scripts — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `$TOKEN$` variable substitution (POI-type + state tokens) and file-based saved scripts to the `play_as` REPL so loops are portable across agents and systems.

**Architecture:** A pure `resolveTokens` function reads live `*game.State` and is invoked at the single dispatch chokepoint (`executeCommand`); unresolved tokens raise a `*tokenError` that aborts loops fatally (bypassing `-f`). The per-command REPL dispatch is extracted into `executeLogicalCommand`, reused by a new `run` command that loads `.smolt` script files from a per-agent dir (shadowing) and a shared dir.

**Tech Stack:** Go 1.25, package `main` in `cmd/tools/play_as`, `github.com/rsned/spacemolt/pkg/game`.

**Spec:** `docs/superpowers/specs/2026-05-22-play-as-tokens-and-scripts-design.md`

---

## File Structure

- **Create `cmd/tools/play_as/tokens.go`** — `resolveTokens`, `tokenError`, token resolution helpers, `knownPOITypes`.
- **Create `cmd/tools/play_as/tokens_test.go`** — unit tests for the resolver.
- **Create `cmd/tools/play_as/scripts.go`** — script path resolution, listing, splitting, saving.
- **Create `cmd/tools/play_as/scripts_test.go`** — unit tests for the script helpers.
- **Modify `cmd/tools/play_as/main.go`** — call `resolveTokens` in `executeCommand`; abort on `*tokenError` in `runLoopSingle`; extract `executeLogicalCommand` from `runREPL`; add `run`/`scripts`/`save` command handlers and `lastCommand` tracking.
- **Modify `cmd/tools/play_as/loop_block.go`** — abort on `*tokenError` in `executeLoop`.
- **Modify `cmd/tools/play_as/loop_block_test.go`** — test fatal-abort propagation.
- **Modify `cmd/tools/play_as/README.md`** — document tokens and scripts.

---

## Task 1: Token resolver

**Files:**
- Create: `cmd/tools/play_as/tokens.go`
- Test: `cmd/tools/play_as/tokens_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/play_as/tokens_test.go`:

```go
package main

import (
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func testState() *game.State {
	return &game.State{
		Credits: 12345.67,
		Ship:    game.Ship{ID: "ship-xyz"},
		System: game.SystemData{
			ID:   "sys-001",
			Name: "Sol",
			POIs: []game.POI{
				{ID: "belt-b", Type: "asteroid_belt"},
				{ID: "belt-a", Type: "asteroid_belt"},
				{ID: "station-1", Type: "station"},
				{ID: "gas-1", Type: "gas_cloud"},
			},
		},
	}
}

func TestResolveTokens(t *testing.T) {
	st := testState()
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"no tokens", []string{"travel", "belt-a"}, []string{"travel", "belt-a"}},
		{"station", []string{"travel", "$STATION$"}, []string{"travel", "station-1"}},
		{"belt id tiebreak", []string{"travel", "$ASTEROID_BELT$"}, []string{"travel", "belt-a"}},
		{"gas cloud", []string{"travel", "$GAS_CLOUD$"}, []string{"travel", "gas-1"}},
		{"lowercase token name", []string{"travel", "$station$"}, []string{"travel", "station-1"}},
		{"state system", []string{"jump", "$SYSTEM$"}, []string{"jump", "sys-001"}},
		{"state ship", []string{"switch_ship", "$SHIP$"}, []string{"switch_ship", "ship-xyz"}},
		{"state credits", []string{"deposit_credits", "$CREDITS$"}, []string{"deposit_credits", "12345"}},
		{"token inside quoted arg", []string{"chat", "go to $STATION$ now"}, []string{"chat", "go to station-1 now"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveTokens(tc.in, st)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("arg %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestResolveTokensErrors(t *testing.T) {
	st := testState()
	cases := []struct {
		name string
		in   []string
	}{
		{"no matching poi", []string{"travel", "$ICE_FIELD$"}},
		{"unknown token", []string{"travel", "$STATON$"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveTokens(tc.in, st)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var te *tokenError
			if !errors.As(err, &te) {
				t.Fatalf("expected *tokenError, got %T: %v", err, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestResolveTokens -v`
Expected: FAIL — `undefined: resolveTokens`, `undefined: tokenError`.

- [ ] **Step 3: Write the implementation**

Create `cmd/tools/play_as/tokens.go`:

```go
package main

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// tokenError indicates a $TOKEN$ in a command could not be resolved from game
// state. Inside a loop it is fatal: the loop aborts immediately, even under -f.
type tokenError struct{ msg string }

func (e *tokenError) Error() string { return e.msg }

// knownPOITypes is the set of POI types a $TYPE$ token may name. It lets the
// resolver tell an unresolvable-but-valid type ("no station in system") apart
// from a typo ("unknown token"). Mirrors the types enumerated in
// pkg/game/constants.go POIFreshnessThreshold.
var knownPOITypes = map[string]bool{
	"asteroid_belt": true, "asteroid": true, "asteroid_field": true,
	"gas_cloud": true, "ice_field": true, "nebula": true,
	"station": true, "base": true, "planet": true, "moon": true,
	"sun": true, "relic": true, "jump_gate": true, "wreck": true,
}

// tokenRe matches $NAME$ where NAME starts with a letter/underscore.
var tokenRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)\$`)

// resolveTokens replaces every $TOKEN$ occurrence in each argument with a value
// derived from live game state. It returns the substituted arguments, or a
// *tokenError if any token cannot be resolved.
func resolveTokens(args []string, state *game.State) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		var rerr error
		out[i] = tokenRe.ReplaceAllStringFunc(a, func(m string) string {
			name := m[1 : len(m)-1] // strip surrounding '$'
			val, err := resolveOneToken(name, state)
			if err != nil {
				if rerr == nil {
					rerr = err
				}
				return m
			}
			return val
		})
		if rerr != nil {
			return nil, rerr
		}
	}
	return out, nil
}

// resolveOneToken resolves a single token name (without the surrounding '$').
// State tokens (SYSTEM, SHIP, CREDITS) take precedence; any other name is
// treated as a POI type and looked up in the current system.
func resolveOneToken(name string, state *game.State) (string, error) {
	switch strings.ToUpper(name) {
	case "SYSTEM":
		if state == nil || state.System.ID == "" {
			return "", &tokenError{"$SYSTEM$: no current system in state"}
		}
		return state.System.ID, nil
	case "SHIP":
		if state == nil || state.Ship.ID == "" {
			return "", &tokenError{"$SHIP$: no active ship in state"}
		}
		return state.Ship.ID, nil
	case "CREDITS":
		if state == nil {
			return "", &tokenError{"$CREDITS$: no state available"}
		}
		return strconv.FormatInt(int64(state.Credits), 10), nil
	}

	poiType := strings.ToLower(name)
	if !knownPOITypes[poiType] {
		return "", &tokenError{fmt.Sprintf("unknown token $%s$", name)}
	}
	if state == nil {
		return "", &tokenError{fmt.Sprintf("$%s$: no state available", name)}
	}
	var matches []string
	for _, p := range state.System.POIs {
		if p.Type == poiType {
			matches = append(matches, p.ID)
		}
	}
	if len(matches) == 0 {
		return "", &tokenError{fmt.Sprintf("no %s POI in system %s (%s)",
			poiType, state.System.Name, state.System.ID)}
	}
	slices.Sort(matches)
	return matches[0], nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/tools/play_as/ -run TestResolveTokens -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Lint**

Run: `golangci-lint run ./cmd/tools/play_as/`
Expected: no new findings.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/tokens.go cmd/tools/play_as/tokens_test.go
git commit -m "feat(play_as): add \$TOKEN\$ resolver for loop variables

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Wire token resolution + fatal loop abort

Resolve tokens at the single dispatch chokepoint (`executeCommand`) and make a
`*tokenError` abort both loop forms even under `-f`.

**Files:**
- Modify: `cmd/tools/play_as/main.go` (`executeCommand` ~line 3385; `runLoopSingle` ~line 5651)
- Modify: `cmd/tools/play_as/loop_block.go` (`executeLoop` ~line 299)
- Test: `cmd/tools/play_as/loop_block_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/tools/play_as/loop_block_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestExecuteLoopTokenError -v`
Expected: FAIL — current `executeLoop` swallows the error under `force=true` and returns `nil`.

- [ ] **Step 3: Add the fatal-abort check in `executeLoop`**

In `cmd/tools/play_as/loop_block.go`, locate the error-handling block inside the
statement loop (the `if err != nil {` block that begins with the
`*game.GoalReachedError` handling around line 299). Immediately **after** the
`GoalReachedError` `errors.As` block and **before** `errCount++`, insert:

```go
			// A *tokenError is fatal: an unresolved $TOKEN$ aborts the entire
			// loop immediately, even under -f (which only tolerates ordinary
			// errors). Return it so every enclosing loop level aborts too.
			var tokErr *tokenError
			if errors.As(err, &tokErr) {
				fmt.Fprintf(out, "%s❌ %v → aborting loop\n", indent, tokErr) //nolint:errcheck
				return err
			}
```

- [ ] **Step 4: Resolve tokens in `executeCommand`**

In `cmd/tools/play_as/main.go`, at the top of `executeCommand` (currently):

```go
func executeCommand(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	cmd := strings.ToLower(parts[0])

	fmt.Printf("▶ Executing: %s %s\n", cmd, strings.Join(parts[1:], " "))
```

replace with:

```go
func executeCommand(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	// Resolve $TOKEN$ variables against live state before dispatch. This is the
	// single chokepoint for all command paths (bare commands, single-form loops
	// via runLoopSingle, and block loops via the runStatement closure), so token
	// substitution works uniformly everywhere. An unresolved token returns a
	// *tokenError, which loops treat as a fatal abort.
	resolved, rerr := resolveTokens(parts, client.GetState())
	if rerr != nil {
		return rerr
	}
	parts = resolved
	cmd := strings.ToLower(parts[0])

	fmt.Printf("▶ Executing: %s %s\n", cmd, strings.Join(parts[1:], " "))
```

- [ ] **Step 5: Add the fatal-abort check in `runLoopSingle`**

In `cmd/tools/play_as/main.go`, inside `runLoopSingle`, locate the
`if cerr := executeCommand(...); cerr != nil {` block and its
`*game.GoalReachedError` handling. Immediately **after** the `GoalReachedError`
`errors.As` block and **before** `errs++`, insert:

```go
			var tokErr *tokenError
			if errors.As(cerr, &tokErr) {
				fmt.Printf("❌ %s → aborting loop\n", tokErr)
				break
			}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./cmd/tools/play_as/ -run 'TestExecuteLoopTokenError|TestResolveTokens' -v`
Expected: PASS.

- [ ] **Step 7: Build, full package test, lint**

Run: `go build ./... && go test ./cmd/tools/play_as/ && golangci-lint run ./cmd/tools/play_as/`
Expected: build OK, tests PASS, no new lint findings.

- [ ] **Step 8: Commit**

```bash
git add cmd/tools/play_as/main.go cmd/tools/play_as/loop_block.go cmd/tools/play_as/loop_block_test.go
git commit -m "feat(play_as): resolve tokens in executeCommand; abort loops on unresolved token

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Extract `executeLogicalCommand` (refactor)

Pure structural refactor: move the per-command dispatch (loop-block vs
`executeCommand`, plus statusline render) out of `runREPL` into a reusable
helper. No behavior change.

**Files:**
- Modify: `cmd/tools/play_as/main.go` (`runREPL` ~lines 426-501)

- [ ] **Step 1: Add the helper function**

In `cmd/tools/play_as/main.go`, add this function (e.g. directly above `runLoopSingle`):

```go
// executeLogicalCommand dispatches one logical command string — a bare command
// or a loop (single or block form) — and renders the statusline afterward. It
// returns a non-nil error only for conditions that should stop a running
// script: a non-force loop failure, a fatal *tokenError, or a bare-command
// error. Ordinary per-command errors are printed here; the REPL ignores the
// return value, while `run` uses it to stop a script.
func executeLogicalCommand(client game.GameClient, ctx context.Context, cmd string, format outputFormat, cfg PlayAsConfig, agentID string) error {
	firstLine := cmd
	if nl := strings.IndexByte(firstLine, '\n'); nl >= 0 {
		firstLine = firstLine[:nl]
	}
	parts := splitArgs(firstLine)
	if len(parts) == 0 {
		return nil
	}
	command := strings.ToLower(parts[0])

	var resultErr error
	if command == "loop" {
		if !hasTopLevelOpenBrace(cmd) {
			runLoopSingle(client, ctx, parts, format)
		} else {
			stmt := Statement{Raw: cmd, Tokens: splitArgs(firstLine)}
			count, force, body, isBlock, perr := parseLoopHeader(stmt)
			switch {
			case perr != nil:
				fmt.Printf("❌ %v\n", perr)
				resultErr = perr
			case !isBlock:
				resultErr = fmt.Errorf("loop: expected block body")
				fmt.Printf("❌ %v\n", resultErr)
			default:
				stmts, serr := parseStatements(body)
				switch {
				case serr != nil:
					fmt.Printf("❌ %v\n", serr)
					resultErr = serr
				case len(stmts) == 0:
					resultErr = fmt.Errorf("loop: empty block")
					fmt.Printf("❌ %v\n", resultErr)
				default:
					preview := blockPreview(stmts)
					if force {
						fmt.Printf("🔁 Repeating { %s } %d time(s) (force mode)...\n", preview, count)
					} else {
						fmt.Printf("🔁 Repeating { %s } %d time(s)...\n", preview, count)
					}
					runStatement := func(tokens []string) error {
						return executeCommand(client, ctx, tokens, format)
					}
					resultErr = executeLoop(ctx, os.Stdout, count, force, stmts, 0, runStatement)
				}
			}
		}
	} else {
		startTime := time.Now()
		if err := executeCommand(client, ctx, parts, format); err != nil {
			var goal *game.GoalReachedError
			if errors.As(err, &goal) {
				fmt.Printf("✓ goal reached: %s\n", goal.Message)
			} else {
				fmt.Printf("❌ %s\n", formatError(err, command, format))
				resultErr = err
			}
		} else {
			fmt.Printf("✓ Completed in %v\n", time.Since(startTime))
		}
	}

	if sl := renderStatusline(client, cfg, agentID); sl != "" {
		fmt.Println(sl)
	}
	fmt.Println()
	return resultErr
}
```

- [ ] **Step 2: Replace the inlined dispatch in `runREPL`**

In `runREPL`, delete the entire `if command == "loop" { ... }` block (currently
~lines 426-477) **and** the trailing bare-command "Execute command" block
(currently ~lines 479-501, from `// Execute command` through the final
`fmt.Println()` of the loop body). Both were the tail of the for-loop body;
replace both with this single dispatch as the new tail:

```go
		// Game command or loop: dispatch through the shared helper (also used
		// by `run`).
		_ = executeLogicalCommand(client, ctx, cmd, format, cfg, agentID)
```

(`lastCommand` tracking for `save` is added in Task 5, together with its reader,
so this task compiles on its own.)

- [ ] **Step 3: Build and verify existing behavior unchanged**

Run: `go build ./... && go test ./cmd/tools/play_as/ && golangci-lint run ./cmd/tools/play_as/`
Expected: build OK, tests PASS, no new lint findings.

- [ ] **Step 4: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "refactor(play_as): extract executeLogicalCommand from runREPL

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Script helpers (paths, listing, splitting, saving)

Pure/file helpers for the script feature, fully unit-tested with temp dirs.

**Files:**
- Create: `cmd/tools/play_as/scripts.go`
- Test: `cmd/tools/play_as/scripts_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/play_as/scripts_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitScriptCommands(t *testing.T) {
	content := `# portable mining loop
loop 3 {
    travel $ASTEROID_BELT$
    mine
}

mine

# trailing comment
dock
`
	got, err := splitScriptCommands(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"loop 3 {\n    travel $ASTEROID_BELT$\n    mine\n}",
		"mine",
		"dock",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestSplitScriptCommandsUnbalanced(t *testing.T) {
	if _, err := splitScriptCommands("loop 3 { mine\n"); err == nil {
		t.Fatal("expected error for unbalanced braces")
	}
}

func TestIsExplicitScriptPath(t *testing.T) {
	cases := map[string]bool{
		"mining-loop":   false,
		"mining.smolt":  true,
		"./x.smolt":     true,
		"/tmp/x.smolt":  true,
		"sub/dir/name":  true,
	}
	for in, want := range cases {
		if got := isExplicitScriptPath(in); got != want {
			t.Errorf("isExplicitScriptPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResolveScriptArgPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	agentID := "miner-1"
	agentDir := filepath.Join("data", "agents", agentID, "scripts")
	sharedDir := filepath.Join("data", "scripts")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// "loop" exists in both; per-agent must win. "shared-only" only in shared.
	mustWrite(t, filepath.Join(agentDir, "loop.smolt"), "mine")
	mustWrite(t, filepath.Join(sharedDir, "loop.smolt"), "dock")
	mustWrite(t, filepath.Join(sharedDir, "shared-only.smolt"), "scan")

	if got, ok := resolveScriptArg("loop", agentID); !ok || got != filepath.Join(agentDir, "loop.smolt") {
		t.Errorf("loop resolved to %q (ok=%v); want per-agent path", got, ok)
	}
	if got, ok := resolveScriptArg("shared-only", agentID); !ok || got != filepath.Join(sharedDir, "shared-only.smolt") {
		t.Errorf("shared-only resolved to %q (ok=%v); want shared path", got, ok)
	}
	if _, ok := resolveScriptArg("missing", agentID); ok {
		t.Error("missing script unexpectedly resolved")
	}

	// Explicit path bypasses name resolution.
	mustWrite(t, "adhoc.smolt", "refuel")
	if got, ok := resolveScriptArg("adhoc.smolt", agentID); !ok || got != "adhoc.smolt" {
		t.Errorf("explicit path resolved to %q (ok=%v)", got, ok)
	}
}

func TestSaveAndListScripts(t *testing.T) {
	t.Chdir(t.TempDir())
	agentID := "miner-1"
	if err := saveScript("my-loop", "loop 3 { mine }"); err != nil {
		t.Fatalf("saveScript: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("data", "scripts", "my-loop.smolt"))
	if err != nil {
		t.Fatalf("reading saved script: %v", err)
	}
	if string(data) != "loop 3 { mine }\n" {
		t.Errorf("saved content = %q", string(data))
	}
	perAgent, shared := listScripts(agentID)
	if len(perAgent) != 0 {
		t.Errorf("perAgent = %v, want empty", perAgent)
	}
	if !reflect.DeepEqual(shared, []string{"my-loop"}) {
		t.Errorf("shared = %v, want [my-loop]", shared)
	}
}

func TestSaveScriptInvalidName(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{"", "a/b", "../escape"} {
		if err := saveScript(name, "mine"); err == nil {
			t.Errorf("saveScript(%q) expected error", name)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run 'TestSplitScriptCommands|TestIsExplicitScriptPath|TestResolveScriptArg|TestSaveAndListScripts|TestSaveScriptInvalidName' -v`
Expected: FAIL — undefined `splitScriptCommands`, `isExplicitScriptPath`, `resolveScriptArg`, `saveScript`, `listScripts`.

- [ ] **Step 3: Write the implementation**

Create `cmd/tools/play_as/scripts.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// scriptExt is the file extension for saved play_as scripts.
const scriptExt = ".smolt"

// scriptSearchPaths returns the script directories searched by bare-name
// resolution, in precedence order: per-agent first (shadows shared), then the
// shared library.
func scriptSearchPaths(agentID string) []string {
	return []string{
		filepath.Join("data", "agents", agentID, "scripts"),
		filepath.Join("data", "scripts"),
	}
}

// isExplicitScriptPath reports whether arg should be treated as a literal file
// path rather than a bare script name: it contains a '/' or ends in ".smolt".
func isExplicitScriptPath(arg string) bool {
	return strings.Contains(arg, "/") || strings.HasSuffix(arg, scriptExt)
}

// resolveScriptArg maps a `run` argument to a file path. An explicit path
// (see isExplicitScriptPath) is used verbatim if it exists. A bare name is
// resolved as "<dir>/<name>.smolt" against scriptSearchPaths in order.
func resolveScriptArg(arg, agentID string) (string, bool) {
	if isExplicitScriptPath(arg) {
		if st, err := os.Stat(arg); err == nil && !st.IsDir() {
			return arg, true
		}
		return "", false
	}
	for _, dir := range scriptSearchPaths(agentID) {
		p := filepath.Join(dir, arg+scriptExt)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// splitScriptCommands splits a script file's content into logical commands.
// Top-level blank lines and '#' comment lines are skipped; a multi-line block
// (e.g. a loop { ... }) is kept together until its braces balance, using the
// same brace/quote scanning the REPL uses for multi-line prompt input.
func splitScriptCommands(content string) ([]string, error) {
	var cmds []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s != "" {
			cmds = append(cmds, s)
		}
	}
	for _, ln := range strings.Split(content, "\n") {
		if cur.Len() == 0 {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(ln)
		depth, inQuote := scanBraceDepth(cur.String())
		if depth < 0 {
			return nil, fmt.Errorf("unbalanced braces in script")
		}
		if depth == 0 && !inQuote {
			flush()
		}
	}
	if cur.Len() > 0 {
		depth, inQuote := scanBraceDepth(cur.String())
		if depth != 0 || inQuote {
			return nil, fmt.Errorf("unbalanced braces in script")
		}
		flush()
	}
	return cmds, nil
}

// validateScriptName rejects empty names and names that could escape the
// shared scripts directory.
func validateScriptName(name string) error {
	if name == "" {
		return fmt.Errorf("save: empty script name")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("save: invalid script name %q", name)
	}
	return nil
}

// saveScript writes content to the shared scripts dir as "<name>.smolt",
// creating the directory if needed. A trailing newline is appended.
func saveScript(name, content string) error {
	if err := validateScriptName(name); err != nil {
		return err
	}
	dir := filepath.Join("data", "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	path := filepath.Join(dir, name+scriptExt)
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

// listScripts returns the sorted script names (without extension) in the
// per-agent and shared directories.
func listScripts(agentID string) (perAgent, shared []string) {
	read := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), scriptExt) {
				continue
			}
			names = append(names, strings.TrimSuffix(e.Name(), scriptExt))
		}
		slices.Sort(names)
		return names
	}
	paths := scriptSearchPaths(agentID)
	return read(paths[0]), read(paths[1])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/tools/play_as/ -run 'TestSplitScriptCommands|TestIsExplicitScriptPath|TestResolveScriptArg|TestSaveAndListScripts|TestSaveScriptInvalidName' -v`
Expected: PASS.

- [ ] **Step 5: Lint**

Run: `golangci-lint run ./cmd/tools/play_as/`
Expected: no new findings.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/scripts.go cmd/tools/play_as/scripts_test.go
git commit -m "feat(play_as): add script path/listing/split/save helpers

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Wire `run`, `scripts`, `save` into the REPL

**Files:**
- Modify: `cmd/tools/play_as/main.go` (`runREPL` meta-command handlers; add `runScript`)

- [ ] **Step 1: Add the `runScript` driver**

In `cmd/tools/play_as/main.go`, add near `executeLogicalCommand`:

```go
// runScript loads a script (by name or explicit path) and executes its logical
// commands in order. Execution stops at the first command that returns a
// stopping error (non-force loop failure or fatal *tokenError).
func runScript(client game.GameClient, ctx context.Context, arg string, format outputFormat, cfg PlayAsConfig, agentID string) {
	path, ok := resolveScriptArg(arg, agentID)
	if !ok {
		fmt.Printf("❌ script %q not found (searched %s)\n",
			arg, strings.Join(scriptSearchPaths(agentID), ", "))
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("❌ run: %v\n", err)
		return
	}
	cmds, err := splitScriptCommands(string(data))
	if err != nil {
		fmt.Printf("❌ run %s: %v\n", path, err)
		return
	}
	fmt.Printf("▶ Running script %s (%d command(s))\n", path, len(cmds))
	for _, c := range cmds {
		if ctx.Err() != nil {
			return
		}
		if err := executeLogicalCommand(client, ctx, c, format, cfg, agentID); err != nil {
			fmt.Printf("⏹ script stopped: %v\n", err)
			return
		}
	}
	fmt.Printf("✓ script %s complete\n", path)
}
```

- [ ] **Step 2: Add `lastCommand` tracking**

In `runREPL`, just before the `for {` REPL loop (e.g. after
`format := outputFormat(cfg.OutputFormat)`), declare:

```go
	// lastCommand holds the most recent game/loop command, for `save <name>`.
	var lastCommand string
```

Then change the dispatch line added in Task 3 from:

```go
		_ = executeLogicalCommand(client, ctx, cmd, format, cfg, agentID)
```

to:

```go
		lastCommand = cmd
		_ = executeLogicalCommand(client, ctx, cmd, format, cfg, agentID)
```

- [ ] **Step 3: Add the meta-command handlers in `runREPL`**

In `runREPL`, alongside the other meta-command handlers (after the `set_format`
block, before the dispatch tail), add:

```go
		// Handle scripts (list saved scripts)
		if command == "scripts" {
			perAgent, shared := listScripts(agentID)
			if len(perAgent) == 0 && len(shared) == 0 {
				fmt.Println("No scripts found.")
			} else {
				overridden := make(map[string]bool, len(perAgent))
				for _, n := range perAgent {
					overridden[n] = true
				}
				fmt.Println("Scripts:")
				for _, n := range perAgent {
					fmt.Printf("  %s (agent)\n", n)
				}
				for _, n := range shared {
					if overridden[n] {
						fmt.Printf("  %s (shared, overridden)\n", n)
					} else {
						fmt.Printf("  %s (shared)\n", n)
					}
				}
			}
			fmt.Println()
			continue
		}

		// Handle save (persist the last command to the shared scripts dir)
		if command == "save" {
			switch {
			case len(parts) < 2:
				fmt.Println("Usage: save <name>")
			case lastCommand == "":
				fmt.Println("❌ save: no previous command to save")
			default:
				if err := saveScript(parts[1], lastCommand); err != nil {
					fmt.Printf("❌ %v\n", err)
				} else {
					fmt.Printf("✓ saved script %q\n", parts[1])
				}
			}
			fmt.Println()
			continue
		}

		// Handle run (load and execute a script)
		if command == "run" {
			if len(parts) < 2 {
				fmt.Println("Usage: run <name|path>")
			} else {
				runScript(client, ctx, parts[1], format, cfg, agentID)
			}
			fmt.Println()
			continue
		}
```

- [ ] **Step 4: Build, test, lint**

Run: `go build ./... && go test ./cmd/tools/play_as/ && golangci-lint run ./cmd/tools/play_as/`
Expected: build OK, tests PASS, no new lint findings (`lastCommand` is now read by the `save` handler).

- [ ] **Step 5: Manual smoke test**

Run (requires a configured agent; substitute a real agent-id):

```bash
go run ./cmd/tools/play_as <agent-id>
```

In the REPL:
1. `travel $STATION$` — confirm the `▶ Executing` echo shows the resolved station ID.
2. `loop 2 { travel $ASTEROID_BELT$; mine }` — confirm tokens resolve each iteration.
3. `save mining-test` — confirm `data/scripts/mining-test.smolt` is written with the loop.
4. `scripts` — confirm `mining-test (shared)` is listed.
5. `run mining-test` — confirm it re-executes.
6. `travel $ICE_FIELD$` in a system with no ice field — confirm it errors with `no ice_field POI in system ...` and (inside a `loop -f`) aborts the loop.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat(play_as): add run, scripts, and save REPL commands

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Documentation

**Files:**
- Modify: `cmd/tools/play_as/README.md`

- [ ] **Step 1: Document tokens and scripts**

In `cmd/tools/play_as/README.md`, after the "REPL Commands" table, add a new section:

````markdown
## Variable Tokens

Commands may contain `$TOKEN$` placeholders that resolve from live game state
right before each command runs. This makes loops portable across systems and
agents.

**POI-type tokens** resolve to a POI in the current system whose type matches
the (lowercased) token name. When more than one POI of that type exists, the one
whose ID sorts first alphabetically is used.

| Token | Resolves to |
|-------|-------------|
| `$STATION$` | first `station` POI ID |
| `$ASTEROID_BELT$` | first `asteroid_belt` POI ID |
| `$GAS_CLOUD$` | first `gas_cloud` POI ID |
| `$ICE_FIELD$` | first `ice_field` POI ID |
| `$<TYPE>$` | first POI of type `<type>` (any known POI type) |

**State tokens:**

| Token | Resolves to |
|-------|-------------|
| `$SYSTEM$` | current system ID |
| `$SHIP$` | active ship ID |
| `$CREDITS$` | current credit balance (integer) |

If a token can't be resolved (no matching POI, or an unknown name), the command
fails — and inside a loop the **entire loop aborts immediately**, even under
`-f`.

Example:

```
loop 10 { travel $ASTEROID_BELT$; mine; travel $STATION$; sell_all }
```

## Scripts

Save reusable command scripts and run them later — the same script works for any
agent because tokens resolve to each agent's own system.

| Command | Description |
|---------|-------------|
| `run <name\|path>` | Run a script. A bare name is looked up in the per-agent dir then the shared dir; an argument with a `/` or ending in `.smolt` is loaded as a literal path. |
| `scripts` | List available scripts (per-agent entries shadow same-named shared ones). |
| `save <name>` | Save the **last command** to the shared scripts dir as `<name>.smolt`. |

Script files are plain command text — multi-line `loop` blocks, `;`/newline
separators, `#` comments, and `$TOKEN$` variables all work. Locations:

- Shared: `data/scripts/<name>.smolt`
- Per-agent override: `data/agents/<id>/scripts/<name>.smolt`
````

- [ ] **Step 2: Commit**

```bash
git add cmd/tools/play_as/README.md
git commit -m "docs(play_as): document variable tokens and scripts

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final Verification

- [ ] `go build ./...` — clean
- [ ] `go test ./cmd/tools/play_as/` — all PASS
- [ ] `golangci-lint run ./cmd/tools/play_as/` — no new findings
- [ ] Manual smoke test (Task 5 Step 4) exercised end-to-end
