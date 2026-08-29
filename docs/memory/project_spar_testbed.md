---
name: project_spar_testbed
description: Sparring testbed — controlled PvP harness (cmd/tools/spar + pkg/spar) to make battle mechanics testable
metadata: 
  node_type: memory
  type: project
  originSessionId: d4e7b749-674c-4002-acee-efa14f16affb
---

Designed 2026-05-25. Spec: `docs/superpowers/specs/2026-05-25-sparring-testbed-design.md`;
plan: `docs/superpowers/plans/2026-05-25-sparring-testbed.md`.
Goal: log in 2+ of our own agents and have them fight in a non-empire arena so
battle mechanics are observable/learnable. **BUILT & merged to main 2026-05-25**
(ff-merged, ending at commit 30a7d91). Code lives in `pkg/spar/`
(policy/arena/combatant/match/telemetry) + `cmd/tools/spar/` + README. Final
opus review was Approved-with-minor-notes; all findings addressed.

Build/run: `go build -o bin/spar ./cmd/tools/spar` then
`bin/spar --arena ross_128 fighter-1 fighter-2` (NOT a root `go build` — that
drops a `spar` binary at repo root; it's now gitignored as `/spar`, but build to
bin/). Ctrl-C is handled: bots flee + partial summary prints.

Still deferred (documented in spec out-of-scope): `--rebuild` (auto-commission a
replacement ship between matches), strict co-location pre-flight, arena
auto-discovery, faction-war-in-empire arenas, `.smolt` scripts, 3+ sided fights.
Cheap next step toward [[project_battle_visualization]]: a `--telemetry-json`
JSON-lines sink in the telemetry loop.

Key design facts (verified against the codebase):
- PvP: `attack <username>` creates a system-scale zone battle; `battle`
  advance/retreat/stance/target; zones outer→mid→inner→engaged; stances
  fire/evade/brace/flee (flee auto-retreats over 3 ticks at 100% dmg taken).
  `get_battle_status` is a free (no-tick) poll.
- PvP is legal **outside empire space** ("not empire space = anything goes");
  faction-war enables it inside empire space (deferred). Example arena:
  `ross_128` (lawless, one jump from station system `treasure_cache`).
- **Foundation fix needed**: `state.BattleState` is declared but never populated
  (`parseMapData` only nils it); plan Task 1-2 parse `get_battle_status` into it.
  serverapi participant carries `hull_pct`/`shield_pct` (ints) — plan adds those
  fields to `game.BattleParticipant`.
- No client-side full-map cache (`parseMapData` only refreshes current system's
  connections) → arena auto-discovery deferred; `--arena` required, validated on
  arrival via `IsNonEmpireArena`.
- Stakes: accept losses, cheap ships. Setup is full-auto (equip-at-station THEN
  travel to arena, since lawless systems lack stations). Bot behavior = named
  policy presets (aggressor/skirmisher/retreater/dummy); `.smolt` scripts later.
- Modes: bot-vs-bot (all args are bots) and partner (human runs their own
  `play_as` and attacks a harness-driven bot — no double login).

This is the prerequisite for the deferred
[[project_play_as_smart_battle_handler]] (shares the BattleState foundation) and
feeds [[project_battle_visualization]].
