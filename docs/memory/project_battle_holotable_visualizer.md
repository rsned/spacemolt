---
name: project_battle_holotable_visualizer
description: "Holotable battle visualizer: P0 + P1a + P1b ALL SHIPPED and merged to kb main (unpushed). Playback works. NEXT = P2 record sheet. Known limit: a real 373-participant battle plays at ~1.1 rendered frames/tick at 4x, so interpolation stops being delivered there."
metadata: 
  node_type: memory
  type: project
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-20T22:45:00.000Z
---

**Spec:** `kb/docs/superpowers/specs/2026-08-16-battle-holotable-visualizer-design.md`
(operator-reviewed as it went; ratified sections are marked). Read it first — it
holds the full asset contract and phase boundaries.

## Status 2026-08-20

- **P0 SHIPPED** (`34ef964d`, `382657ca`, `4509d772` in spacemolt):
  `pkg/battlereplay` (replay model + adapter) and `cmd/tools/battle-export` →
  `bin/battle-export`. Wire types in `pkg/game/serverapi/responses_battle_log.go`.
- **P1a SHIPPED 2026-08-19**, pushed. Static frame: `pkg/footprint`,
  `cmd/generate-battle-holotable`, `kb/battles/holotable.js`.
- **⭐ P1b SHIPPED 2026-08-20 — MERGED to kb `main` as `32bb0f4fa`, NOT PUSHED.**
  18 commits. Playback: transport (play/pause, step, scrub, 0.25x-4x,
  keyboard), linear interpolation with alpha fades for hulls arriving/dying,
  and a chatter rail. New file `kb/battles/holotable-player.js` (motion) beside
  `holotable.js` (drawing) — the seam is "draws a frame" vs "decides which
  frame, and when". 145 JS tests. Findings: `docs/holotable-p1b-findings.md`.
- **The worktree `/home/robert/spacemolt/kb-p1b` and branch
  `battle-holotable-p1b` are DELIBERATELY KEPT** (operator: "in case we have
  more we end up doing until it's ready to release"). It holds ~67MB that
  exists nowhere else: `.superpowers/sdd/` review artifacts + ledger with 20
  rulings, and the 22MB real battle export. Do not clean it up without asking.

## P1b's measured results — the ones that matter

- **`bench()` MEASURES THE WRONG THING.** It times `render()` alone. The real
  loop also runs `syncControls()` (4 DOM writes), `appendRail()`, and a full
  canvas composite EVERY animation frame. Bench says 88.47 ms/frame on the real
  373-participant battle; live it plays at **8.3 fps at 1x (~4 rendered frames
  per tick) and 9.2 fps at 4x (~1.1 frames/tick)**. So **at 4x on a large
  battle, interpolation stops being delivered** — pacing holds by drawing fewer
  frames, the exact thing interpolation exists to prevent. Clock stays exact,
  no tick dropped from the rail. DOCUMENTED, NOT FIXED.
  **First thing to try: `syncControls()` only needs to run on tick boundaries.**
- **The synthetic stress fixture is PESSIMISTIC, not optimistic**: 114.75
  ms/frame at 420 synthetic hulls vs 88.47 at 373 real. Regenerate it with
  `node scripts/make-stress-replay.js` (44MB, gitignored).
- Bench numbers carry **±5-10%**: independent re-runs read 88.47 and 94.54.
- **The chatter rail is 20.7 lines/tick** on Node Beta, not the 6.7 grouping
  predicts: 201 grouped reasons + **405 ungrouped zone moves** + 14 kills = 620
  over 30 ticks. Zone moves dominate and are never grouped by design.
- **`?tick=busiest` needs `findIndex(f => f.tick === busiestTick(replay))`** —
  `busiestTick` returns a TICK NUMBER, `pickFrame` returns a FRAME OBJECT.
  Conflating them silently falls back to frame 0 forever.

## Run it

    bin/battle-export --agent craftsman-boss --battle <id> [--limit N] [--gzip] --out f.json

**Pick the export agent from `ps aux`, never from a static list.** A login
collides with that agent's running worker and dies `session_replaced`; two
such deaths inside 30s trip the contention guard and abort the run.
Measured 2026-08-19: **`explorer-7` is NOT idle** (live `bin/worker --role
missionrunner`, mission-learn fleet) and **`databot` is NOT idle**
(interactive `play_as` on a pts). **`craftsman-boss` was genuinely idle and
is what works.** Battle logs are readable by ANY logged-in agent, so any idle
one will do — you do not need a participant.

## Hard-won facts

- **The table is RADIAL.** Concentric rings OUTER/MID/INNER/ENGAGED around a
  centre; each side holds a spoke and advance/retreat runs inward/outward along
  it. An earlier reading as a 1-D x axis was an artifact of a 2-side battle.
  Centre = **midpoint of position bounds** (the centroid does NOT work — the
  bigger side drags it). Verified monotonic: engaged 0.58 → outer 1.08.
- **Sides are NOT limited to two** — 3 and 4 occur (`b131fd5aae68…`: four sides
  at bearings 82/121/152/271°). Upper bound unknown, so never assume a count.
  Heading is a ROTATION toward target or centre, never a mirror. Average
  bearings as unit vectors or a side straddling 0°/360° lands at 180°.
- **Scale:** 620-tick battles exist (`de4452d5a8…`); `c79f7810a5…` is 264 ticks ×
  **373 participants**, 85 kills, **24.3MB → 3.2MB gzipped**. Ship states are
  19MB of that (86,985 rows), chatter 4.6MB, all else <0.7MB. Next lever if a
  browser struggles: sparse ship states (dense frames are deliberate so the
  renderer never rehydrates carry-forward).
- **WebSocket caps a page at 10MB** (`SetReadLimit`, pkg/game/client.go). ~370
  participants ≈ 90KB/tick, so use `--limit 10`; each oversized frame costs a
  reconnect and **two inside 30s trip the client's session-contention guard**
  and abort. Exporter halves on failure with a 35s backoff. Global limit was NOT
  raised — it would inflate buffers for ~160 fleet workers.
- **SVG assets:** `kb/data/footprints/hy3d-svg/<ship_class>.svg`, 395 files.
  **filename == data-ship == catalog id == battle-log `ship_class`** — a single
  lookup, no faction-prefix resolver. Bow-right, length-normalized to 1000 units
  (viewBox `0 0 1020 H`, 10-unit margin, hull bbox (10,10)→(W−10,H−10)), one
  path with `fill-rule="evenodd"`, plus `data-aspect` / `data-frame-ambiguous` /
  `data-adjustments` / `data-art-stem` / `data-kb-match`.
  Draw transform: translate −(510, H/2), scale L/1000, rotate to heading.
  Coverage: 81% of the catalog, but **99.5% of hulls our fleet flies** (only
  `rubble`, `scrutiny`). `data-kb-match=fuzzy` (4 files) are the only inferred
  joins — suspect them first if a hull looks wrong.
- **Stations have no SVG.** The official viewer draws a filled hexagon with a
  circle at each corner inside two concentric rings. They also carry an EMPTY
  `ship_class`, so they are the first real consumer of the fallback.
- **`hull` reads 0 for some participants including on tick 1** (38 of 840 states
  in the reference battle). Render 0/max as UNKNOWN, not an empty bar.
- **API drift:** `WeaponFireDetail.ammo_mod` is a FLOAT (-0.2, -0.15), not the
  string openapi.json declares. Do not "fix" it back.

## Filed / to file

Server bug report drafted at
`<scratchpad>/float-formatting-bug.md` (raw evidence beside it in
`raw_one_tick.json`): `get_battle_log` emits full float64 precision —
`hit_chance: 0.09999999999999999` should be `0.1`, and `x`/`y` carry 16-17
decimals × 362 each per tick. Includes the `ammo_mod` spec mismatch.

Related: [[reference_battle_log_api_replay_data]] (data inventory) ·
[[reference_battle_replay_viewer]] · [[reference_legacy_ship_classes_erased_by_refresh]]
