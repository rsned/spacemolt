# Retire the `cmd/auto-*` Tools (Phase 1)

**Date:** 2026-06-22
**Status:** Approved design, ready for implementation plan
**Author:** Robert Snedegar (with Claude)

## Motivation

The `cmd/auto-*` family is a set of standalone single-purpose agent binaries, each
a wrapper around game-client init plus a behavior loop. Most of what they do — or
were *hoping* to do — is now better expressed by `cmd/overmind/` plus the
scripting / role-YAML support being built around `cmd/worker` and
`data/overmind/roles.yaml`. The overmind path is more flexible (one supervised
runtime, per-role standing behaviors, scheduled commands, idle scripts) and avoids
maintaining a fleet of near-duplicate `main.go` wrappers.

This spec covers **Phase 1 only**: retire the tools that are already redundant or
are unimplemented stubs, clean up the dead code they leave behind, and reframe the
docs toward overmind. The "fat" tools with substantial custom logic are
explicitly **deferred** (see Roadmap) — per decision, their behavior is dropped
from this pass rather than ported verbatim.

## Scope

### Retire now (delete)

| Tool | Why it's safe to delete |
|------|--------------------------|
| `cmd/auto-miner` | Logic lives in `game.MiningLoop`; the overmind resident role already runs `idle: idle_mine`. Fully redundant. |
| `cmd/auto-craftsman` | Logic lives in `game.CraftingLoop`, but crafting has churned heavily server-side and will be rebuilt from scratch. The current loop is stale; nothing worth preserving. |
| `cmd/auto-pirate` | Unimplemented stub (monitoring skeleton only). |
| `cmd/auto-salvager` | Unimplemented stub (monitoring skeleton only). |
| `cmd/auto-recall` | Small deterministic "navigate to capital and back" experiment. |
| `cmd/auto-llm-miner` | Thin LLM-guided mining experiment. |

### Keep (defer migration)

`cmd/auto-explorer`, `cmd/auto-trader`, `cmd/auto-prophet`, `cmd/auto-fighter`,
`cmd/auto-random` — these carry substantial custom logic that does **not** exist
in the role/strategy/scripting layer yet. They remain runnable as-is. Their
migration is roadmap work, not part of this pass.

## Design

### 1. Delete the six `cmd/` directories

`cmd/` packages are `package main` and are imported by nothing, so removing the
directories cannot break compilation of any other package. Delete:

```
cmd/auto-miner/
cmd/auto-craftsman/
cmd/auto-pirate/
cmd/auto-salvager/
cmd/auto-recall/
cmd/auto-llm-miner/
```

Git history preserves them; no archival copy is needed.

### 2. Remove orphaned `pkg` code

Only remove code that becomes **provably dead** after the deletions — verified by
`grep`, not assumed.

- **`game.CraftingLoop`** — its only non-test consumer is `cmd/auto-craftsman`.
  After deletion it is referenced only by `pkg/game/crafting_loop.go` itself and
  by `CraftingLoop*` tests in `pkg/game/crafting_test.go`. Remove
  `pkg/game/crafting_loop.go` and excise the `CraftingLoop`-specific tests from
  `crafting_test.go` (keep any tests in that file that cover other crafting
  helpers). Crafting will be rebuilt from scratch later (see Roadmap), so this
  stale loop should not linger.
- **`game.MiningLoop`** — **keep.** Still used by `pkg/strategy/mining.go`
  (`MiningStrategy`). Not dead.
- **Other helpers** — after deleting the six dirs, grep for any `pkg/` or shared
  helper symbols that were referenced *only* by the deleted binaries (e.g. any
  recall-navigation or llm-miner helpers that were promoted out of `main.go`).
  Remove what is truly orphaned; leave anything with a surviving consumer.

The acceptance gate for this step is mechanical: `go build ./...` and
`go test ./...` stay green, and `go vet ./...` reports no unused symbols
introduced by the change.

### 3. Makefile

Of the retire set, only `auto-miner` has a build target. Remove the line:

```
go build -o bin/auto-miner ./cmd/auto-miner
```

(`auto-craftsman`, `auto-pirate`, `auto-salvager`, `auto-recall`, `auto-llm-miner`
are not referenced in the Makefile — no change needed for them.) Leave the
`auto-explorer`, `auto-prophet`, `overmind`, and `worker` targets intact.

### 4. README

The README references the retired tools in four places:

1. Tool list (~line 199–207)
2. ASCII architecture diagram (~line 375–383)
3. Usage / "how to run an agent" section (~line 893–919)
4. Directory-tree listing (~line 1010–1019)

Update all four:

- Remove the six retired tools from every list, the diagram, and the dir tree.
- Reframe the agent-runtime narrative so **overmind + worker + `roles.yaml`** is
  the documented path for standing/recurring behaviors. The current "run a miner"
  instructions (`go build -o bin/auto-miner …`) should point instead at the
  overmind resident role (`idle: idle_mine`).
- List the five surviving `auto-*` tools as the not-yet-migrated specialized bots,
  with a one-line note that they are slated to fold into overmind roles/scripts
  over time.

Keep edits surgical — do not rewrite unrelated README sections.

### 5. Verification

After all cuts:

- `go build ./...` — clean.
- `go test ./...` — green.
- `golangci-lint` — no new findings.
- Grep sanity check: no remaining references to the six deleted binaries anywhere
  in tracked files except this spec and intentional historical mentions.

## Roadmap (deferred — not implemented in this pass)

Recorded here so the deferred work isn't lost:

- **auto-explorer** is the closest to retirement. Its DFS / POI-sweep behavior is
  largely already captured by the `play_as` metacommands `explore` (single-system
  distance-ordered POI sweep with `update_poi`/`update_all` + refuel, in
  `cmd/tools/play_as/explore.go`) and `auto-explore` (multi-system outward-drifting
  tour with `--max-hops`, in `cmd/tools/play_as/auto_explore.go`). Because these
  are `play_as` commands, they can be scheduled or set as an idle script under an
  overmind role — a future role (e.g. `idle: auto_explore`) likely retires
  `cmd/auto-explorer` with little new logic.
- **auto-trader** — 6-phase inter-empire trade state machine; needs a role/script
  expression before retirement.
- **auto-prophet** — specialized sermon/cult behavior; lowest priority to migrate.
- **auto-fighter** — simplistic combat loop; depends on combat behavior maturing.
- **auto-random** — test-chaos NPC; keep until an equivalent test affordance exists.
- **Crafting from scratch** — once server-side crafting churn settles, add an
  `idle_craft` script + role wiring (mirroring `idle_mine`) as the replacement for
  the deleted `auto-craftsman` / `game.CraftingLoop`.

## Non-goals

- Porting any fat-tool logic into roles/strategies/scripts (explicitly deferred).
- Touching `pkg/strategy` skeleton stubs for explorer/trader/fighter.
- Expanding the overmind fleet roster or adding new roles.
- Any change to `game.MiningLoop` or `MiningStrategy`.
