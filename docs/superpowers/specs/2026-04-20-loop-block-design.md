# `loop` Block Syntax — Design

**Date:** 2026-04-20
**Component:** `cmd/tools/play_as/`
**Status:** Design approved; awaiting user review

## Motivation

The `play_as` REPL's `loop [-f] <count> <command...>` meta-command repeats a single command N times. Real play sessions often need a short scripted routine — travel, mine a few times, return, dock, deposit, refuel — which today requires wiring up a separate tool or macro. Extending `loop` to accept a braced block of commands (newline- or semicolon-separated, nestable) gives users a lightweight in-REPL scripting primitive without a new command surface.

## Goals

- Accept a block body: `loop <count> { cmd1; cmd2; ... }`.
- Allow newline and/or semicolon separators interchangeably inside the block.
- Support arbitrary nesting of `loop` inside a block.
- Preserve current single-command form: `loop 5 mine` continues to work unchanged.
- Preserve current `-f` (continue-on-error) flag and its semantics for the loop it's attached to.

## Non-Goals

- General-purpose scripting (no variables, conditionals, expressions).
- Completion inside a block.
- Loading scripts from a file (one-liners and pasted multi-line blocks only).
- Breaking out of a loop from within the body (no `break`/`continue` keywords).

## User-Facing Syntax

Three forms, all equivalent where the body is the same:

```
loop 20 {
  travel sol_belt
  mine
  mine
  travel sol_central
  dock
  deposit_all
  refuel
}
```

```
loop 20 { travel sol_belt; mine; mine; travel sol_central; dock; deposit_all; refuel }
```

Nested:

```
loop 20 {
  travel sol_belt
  loop 25 mine
  travel sol_central
  dock
  deposit_all
  refuel
}
```

Inside a block:

- Statements are separated by newlines or `;`.
- Blank lines are skipped.
- `#` starts a line comment to end of line (ignored inside quotes).
- Semicolons inside quoted strings do not split (`chat "hi; there"` → one statement).
- Nested `{...}` blocks are preserved verbatim and parsed recursively at execution time.

## REPL Integration

### Multi-line Reading

`runREPL` in `cmd/tools/play_as/main.go` currently reads one line via `line.Prompt("$ ")` per iteration. That is replaced with a helper:

```go
// readLogicalCommand reads from the prompt, continuing onto additional
// lines with a "... " prompt while brace depth > 0. Returns the joined
// script as a single string (newlines preserved).
func readLogicalCommand(line *liner.State) (string, error)
```

Behavior:

1. Read the first line with `$ `.
2. Scan for unbalanced `{` at top level (quote-aware).
3. If balanced, return the line.
4. Otherwise, keep reading with `... ` prompt, appending each additional line with `\n`, until brace depth returns to 0.
5. Ctrl-C while in `... ` mode discards the partial block and returns to the main `$ ` prompt (does not exit the REPL).

### History

Multi-line blocks are collapsed into a single history entry by replacing each `\n` with `; ` before appending to `liner`'s history and the on-disk history file. One up-arrow recalls the entire script as a one-liner, which the parser still accepts.

### Completer

No change. `loop` is already registered in `completer.go:14`. Completion inside block bodies is out of scope.

### Help

Update the `loop` line in `printHelp()` (around `main.go:4249`) to mention the block form and show a short example.

## Architecture

New file: `cmd/tools/play_as/loop_block.go`. Keeps `main.go` from growing further. `main.go`'s `loop` branch is shortened to dispatching either the existing single-command path or the new block path based on whether the input contains a top-level `{`.

### Types

```go
// Statement is one top-level command inside a loop body.
// Raw is the original string; Tokens is the splitArgs result.
// For nested loops, Raw preserves the full "loop ... {...}" text.
type Statement struct {
    Raw    string
    Tokens []string
}
```

### Functions

```go
// scanBraceDepth reports the net brace depth of s, whether the scan
// ended inside a quoted string, and any scan error (e.g., unterminated
// quote at EOF is not an error — the caller may still be reading).
// Used by readLogicalCommand to decide when input is complete.
func scanBraceDepth(s string) (depth int, inQuote bool, err error)

// parseStatements splits a block body into top-level statements.
// Splits on ';' and '\n' at brace-depth 0 outside quotes.
// Strips '#' line comments (to end-of-line, outside quotes).
// Nested '{...}' content is preserved verbatim inside the Statement's Raw.
func parseStatements(body string) ([]Statement, error)

// parseLoopHeader parses the "loop [-f] <count> " prefix of a statement
// whose first token is "loop", returning the count, force flag, remainder
// (either a single-command tail or a "{...}" block body extracted from
// between the outermost braces), and whether it's a block form.
func parseLoopHeader(stmt Statement) (count int, force bool, body string, isBlock bool, err error)

// executeLoop runs a parsed loop.
// For each iteration: for each statement, if the first token is "loop",
// recursively parse and execute; otherwise dispatch via the injected
// runStatement function. Error handling uses this loop's own force flag.
func executeLoop(
    ctx context.Context,
    count int, force bool,
    body []Statement,
    depth int,                             // for indentation of status lines
    runStatement func(tokens []string) error,
) error
```

`runStatement` is an injected dispatcher. In production it wraps
`executeCommand(client, ctx, tokens, format)`. In tests it records calls
and returns scripted errors.

### Control Flow

```
runREPL loop iteration:
  input := readLogicalCommand(liner)
  append collapsed input to history
  parts := splitArgs(firstLine(input))
  if parts[0] == "loop" and input contains top-level '{':
      stmts := parseStatements(inner body)
      executeLoop(count, force, stmts, depth=0, runStatement)
  else if parts[0] == "loop":
      (existing single-command path, unchanged)
  else:
      (existing dispatch, unchanged)
```

## Execution Semantics

### Per-Iteration Output

Each iteration prints the same `── [i/count]` / `✓ [i/count]` lines the
current implementation does. For nested loops, lines are indented two
spaces per nesting level so outer/inner iterations are visually distinct.

### Error Handling

Each `loop` handles errors per its own `-f` flag:

- An inner `loop -f ...` swallows its own errors and returns `nil` to the outer loop.
- An inner `loop ...` (no `-f`) returns the first error it sees; the outer loop then consults *its own* `-f`.

Concretely, in `executeLoop`:

```
for i := 1..count:
    print header
    for stmt in body:
        err := (stmt is loop ? recurse : runStatement(stmt.Tokens))
        if err != nil:
            errors++
            print formatted error
            if !force:
                print "Stopping loop after i/count iterations"
                return err
    print "✓ [i/count]"
if force && errors > 0:
    print summary
return nil
```

### Statusline

Rendered once at the end of the outermost loop, as today. Not after nested loops (would be too noisy).

### Parse Errors

Reported with a clear message and the loop is not executed. Cases:

- Unbalanced braces at EOF (shouldn't normally happen — `readLogicalCommand` keeps reading — but guard anyway).
- Empty body (`loop 5 { }`).
- Malformed inner `loop` header (e.g. non-integer count).
- `{` used outside a `loop` header context.

## Parsing Details

### Brace / Quote State Machine

`scanBraceDepth` and `parseStatements` share the same state machine:

- Modes: `normal`, `dquote`, `squote`, `linecomment`.
- In `normal`:
  - `"` → `dquote`; `'` → `squote`; `#` → `linecomment`.
  - `{` → `depth++`; `}` → `depth--` (error if < 0).
  - `;` or `\n` at depth 0 → statement terminator.
- In `dquote`: `\` escapes the next char; `"` returns to `normal`.
- In `squote`: no escapes; `'` returns to `normal` (matches `splitArgs` behavior — confirm during implementation).
- In `linecomment`: consume until `\n`, then return to `normal`.

At depth > 0 (inside a nested block), separators are ignored — the entire nested block becomes part of the parent statement's `Raw`.

### Relationship to `splitArgs`

`parseStatements` splits into statements; `splitArgs` then tokenizes each statement's `Raw` into argv. The quote/escape handling in `parseStatements` must be consistent with `splitArgs` so that a semicolon inside a quoted string is neither a statement separator nor tokenized as a separate arg. The implementation verifies this with a targeted test.

## Testing

Plan in `cmd/tools/play_as/loop_block_test.go`:

### Pure Parser Tests (no game client)

`parseStatements`:

- Newline-separated body splits correctly.
- Semicolon-separated body splits correctly.
- Mixed newlines and semicolons.
- Quoted strings containing `;` or `\n` are not split.
- `#` comments stripped; `#` inside quotes preserved.
- Nested `{...}` kept as single statement, inner `;`/`\n` not split.
- Empty and whitespace-only lines skipped.
- Unbalanced braces at EOF → error.

`scanBraceDepth`:

- Tracks `{`/`}` correctly.
- Ignores braces inside `"..."` and `'...'`.
- Honors `\"` escapes inside double quotes.

`parseLoopHeader`:

- `loop 5 mine` → count=5, body="mine", isBlock=false.
- `loop -f 10 mine` → count=10, force=true, isBlock=false.
- `loop 3 { a; b }` → count=3, body="a; b", isBlock=true.
- `loop -f 3 { a; b }` → force=true, isBlock=true.
- Non-integer count → error.

### `executeLoop` Tests (recording dispatcher)

A `runStatement` stub records each `tokens []string` call and returns scripted errors by index.

- Executes body `count` times in order.
- Nested loop runs inner body `outer*inner` times; records interleave correctly.
- `-f` swallows errors; iteration continues; final `errors > 0` produces summary line.
- Without `-f`, stops at first error and returns it.
- Inner `-f` with outer not-`-f`: inner swallows, outer sees no error, outer completes.
- Inner not-`-f` with outer `-f`: inner aborts, returns error; outer catches and continues.

### Not Covered by Automated Tests

- REPL prompt switching (`... ` continuation). Covered by manual smoke test: paste a multi-line script, verify prompts and execution.
- Ctrl-C abort during `... `. Manual smoke test.

## Open Questions / Future Work

- **Completion inside blocks.** Could be added later; requires the completer to be aware of brace context. Out of scope here.
- **Named scripts / macros.** A natural follow-on: `define routine { ... }` then `loop 20 routine`. Also out of scope.
- **`break` / `continue`.** Not requested; would significantly expand scope.

## File Summary

| File | Change |
|------|--------|
| `cmd/tools/play_as/loop_block.go` | New — types, parser, executor. |
| `cmd/tools/play_as/loop_block_test.go` | New — parser & executor tests. |
| `cmd/tools/play_as/main.go` | Refactor `loop` branch to dispatch block form; replace inline `line.Prompt` with `readLogicalCommand`; update `printHelp()`. |
