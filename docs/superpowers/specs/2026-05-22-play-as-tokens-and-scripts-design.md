# play_as: Loop Variable Tokens & Saved Scripts — Design

**Date:** 2026-05-22
**Component:** `cmd/tools/play_as`
**Status:** Approved (pending spec review)

## Problem

The interactive `play_as` REPL supports loops (`loop`, see
`2026-04-20-loop-block-design.md`), but every agent must hard-code the
system-specific POI IDs it operates on: the asteroid/gas/ice belt it gathers
from, and the station it unloads at. This makes loops non-portable — a mining
loop written for one agent in one system can't be reused by another agent in a
different system. There is also no way to save a loop for reuse or sharing.

Two related features address this:

- **A. Variable tokens** — `$TOKEN$` placeholders in command strings that
  resolve from live game state (e.g. `$ASTEROID_BELT$`, `$STATION$`), so the
  same command adapts to whatever system the agent is currently in.
- **B. Saved scripts** — load/save command scripts to files so a robust loop
  (written with tokens) can be reused and shared across all agents.

## Key Observation

The game `Client` already keeps `State.System` (including `.POIs`) continuously
up to date via `handleResponse()` on every login, jump, and travel. Token
resolution therefore needs no separate bookkeeping — it reads
`client.GetState()` at execution time.

---

## Feature A — Variable Tokens

### Syntax

`$NAME$` — dollar-delimited on both sides (matches the user's `$ASTEROID_BELT$`
example). Resolution operates on each command's argument tokens *after*
`splitArgs`, replacing any `$NAME$` occurrence within a token via string
substitution. This means tokens work everywhere uniformly:

- inside loops: `loop 5 { travel $ASTEROID_BELT$; mine; travel $STATION$ }`
- in bare commands: `travel $STATION$`
- inside quoted args: `chat "heading to $STATION$"`
- in scripts (Feature B)

### Token families

**1. POI-type tokens.** The token name, lowercased, *is* the POI type to look
up in the current system. No hardcoded mapping table — `$ASTEROID_BELT$` →
type `asteroid_belt`, `$STATION$` → `station`, and likewise `$GAS_CLOUD$`,
`$ICE_FIELD$`, `$ASTEROID_FIELD$`, `$NEBULA$`, `$ASTEROID$`, `$BASE$`,
`$PLANET$`, etc.

Resolution: filter `State.System.POIs` by `Type == lower(name)`, **sort the
matches by POI `ID` ascending, and resolve to the first match's `ID`** (the
value `travel`/`dock` expect).

To produce a friendly "unknown token" error rather than a confusing "no X in
system" for typos, POI-type token names are validated against a known POI-type
set:

```
asteroid_belt, asteroid, asteroid_field, gas_cloud, ice_field, nebula,
station, base, planet, moon, sun, relic, jump_gate, wreck
```

(derived from the types enumerated in `pkg/game/constants.go`
`POIFreshnessThreshold`). A token that is neither a state token nor a known
POI type errors as `unknown token $FOO$`.

**2. State tokens.** A small fixed set, resolved from `State`:

| Token | Resolves to | Source |
|-------|-------------|--------|
| `$SYSTEM$` | current system ID | `State.System.ID` |
| `$SHIP$` | active ship ID | `State.Ship.ID` |
| `$CREDITS$` | integer credits | `int64(State.Credits)` |

### Resolution timing

**Live, per-statement.** Each statement re-resolves its tokens from
`client.GetState()` immediately before it executes. A loop that jumps to a new
system automatically picks up that system's POIs on the next statement.

### Error handling — fatal abort

If any token in a statement cannot be resolved (no POI of that type in the
current system; unknown token name), resolution returns a sentinel
`*tokenError` carrying a clear message, e.g.:

```
no asteroid_belt POI in system Sol (sys-001)
unknown token $STATON$
```

- **Bare command:** the command reports the error and stops.
- **Inside a loop:** the **entire loop aborts immediately, even under `-f`**.

Mechanism: in `executeLoop`, add a check for `*tokenError` via `errors.As`
adjacent to the existing `*game.GoalReachedError` special case. Unlike a normal
error (which `-f` swallows) and unlike `GoalReachedError` (which returns `nil`),
a `*tokenError` is **returned up** through every nesting level, bypassing the
force-swallow logic, so nested loops abort too.

### Integration point

A new resolver:

```go
// resolveTokens replaces $TOKEN$ occurrences in each argument with values
// from live game state. Returns *tokenError if any token is unresolvable.
func resolveTokens(tokens []string, client game.GameClient) ([]string, error)
```

is called at the single chokepoint where argument tokens are dispatched —
inside the `runStatement` closure (which wraps `executeCommand`) and on the
bare-command path in `runREPL`. Because both loop statements and bare commands
flow through token-list dispatch, one call site each covers all cases.

---

## Feature B — Saved Scripts

### Storage & lookup

Two directories; per-agent shadows shared:

- Shared: `data/scripts/<name>.smolt`
- Per-agent: `data/agents/<id>/scripts/<name>.smolt`

`run <name>` resolves a **bare name** (no extension, no path) by checking the
per-agent dir first, then the shared dir; first hit wins. If not found, error
listing both search paths.

`run` also accepts an **explicit path**: if the argument contains a `/` or ends
in `.smolt` (e.g. `run ./test.smolt`, `run /tmp/x.smolt`), it is treated as a
literal file path and loaded directly, bypassing name resolution.

### REPL commands

| Command | Behavior |
|---------|----------|
| `run <name\|path>` | Load and execute a script. |
| `scripts` | List available scripts from both dirs; mark per-agent entries that override a shared name. |
| `save <name>` | Write the **last logical command** (raw, multi-line preserved) to `data/scripts/<name>.smolt`. |

`save` writes to the **shared** dir by default — the intended workflow is to
build a loop interactively, then save it so every agent can `run` it. The "last
logical command" is the previous command string entered at the prompt, captured
before its newlines are collapsed for history, so a saved multi-line loop block
stays readable.

### File format

Plain text, identical to what is typed at the prompt:

- multi-line `loop` blocks with `{ ... }`
- `;` or newline statement separators
- `#` line comments (already handled by `parseStatements`)
- `$TOKEN$` variables (Feature A)

A file may contain multiple logical commands, executed in order.

Example `data/scripts/mining-loop.smolt`:

```
# Portable mining loop — works in any system with a belt and a station
loop 10 {
    travel $ASTEROID_BELT$
    mine
    travel $STATION$
    sell_all
}
```

### Execution model + refactor

Today `runREPL` inlines the per-command dispatch: loop-block detection
(`hasTopLevelOpenBrace` → `parseLoopHeader`/`parseStatements`/`executeLoop`)
vs. `executeCommand`, plus the trailing statusline render.

Extract this into a single helper, reused by both the REPL loop and `run`:

```go
// executeLogicalCommand dispatches one logical command string (a bare command
// or a loop block) and renders the statusline. Returns an error only for fatal
// conditions that should stop a running script.
func executeLogicalCommand(client game.GameClient, ctx context.Context,
    cmd string, format outputFormat, cfg PlayAsConfig, agentID string) error
```

`run` reads the target file and splits it into logical commands using the
existing brace-aware `scanBraceDepth` (the same rule `readLogicalCommand` uses
for multi-line prompt input): accumulate lines until brace depth returns to 0
and not inside a quote, emit that chunk as one logical command, repeat. Each
chunk is passed to `executeLogicalCommand`. A fatal token error or a non-force
loop failure stops the script; remaining commands are not run.

Meta commands (`exit`, `help`, `set_format`, `mbox`, `history`) are **not**
script-executable — scripts are limited to game commands, `sleep`, and `loop`.
`executeLogicalCommand` covers exactly that surface; the REPL retains its own
handling of meta commands ahead of the shared dispatch.

---

## Testing

- **Token resolver** (`resolveTokens`): table tests over a synthetic `State` —
  single POI match; multiple matches (ID-ascending tiebreak); no match (error
  message); each state token; unknown token error; substitution inside a quoted
  token; a token-free arg list passes through unchanged.
- **Fatal abort** (`executeLoop`): a `*tokenError` aborts even with
  `force=true`, and propagates out of a nested loop.
- **Script split**: brace-aware splitting of a multi-command file that includes
  a multi-line loop block yields the expected logical commands.
- **Lookup precedence**: per-agent script shadows a same-named shared script;
  explicit `.smolt`/`/`-containing arg bypasses name resolution; missing name
  errors with both search paths.

## Out of Scope (YAGNI)

- User-defined variables (`set NAME value`).
- Script management commands beyond `run`/`scripts`/`save` (`cat`, `rm`,
  `edit`).
- Escaping a literal `$` in command text.
- `save --local` targeting the per-agent dir (the per-agent dir is still read
  on `run`; only `save`'s default target is fixed to shared).
