# Explorer Renumbering Housekeeping — Design

**Date:** 2026-06-09
**Status:** Approved (design)

## Problem

Agents are named `${ROLE}-${N}`. The trailing number is meant to encode empire
alignment by band:

| Band | Empire |
|------|--------|
| 1, 2 | nebula |
| 3, 4 | solarian |
| 5, 6 | voidborn |
| 7, 8 | crimson |
| 9, 10 | outerrim |

During initial creation the numbers were jumbled for some agents. An audit of
every agent's `personality.json` `empire` field against this band map shows:

- **6 of 7 empire-range roles are already perfect:** craftsman, engineer,
  fighter, miner, salvager, trader — all 10 of each match their band.
- **`explorer-*` is the only mismatched role** — fully scrambled.
- Special categories are consistent and out of scope:
  - `prophet-1/2` → independent (the two cult leaders)
  - `spark-1..5`, `architect-1..5` → independent (the two cults' acolytes)
  - `pirate-1..15` → three internally-unified squads (1-5 crimson, 6-10
    outerrim, 11-15 voidborn)
  - `random-1..9` → intentionally unaligned (their empire fields are ignored)

The user's recollection that "−7/−8 should be crimson but not all are" traces
entirely to `explorer-7` (nebula) and `explorer-8` (solarian); every other
role's −7/−8 is correctly crimson.

### The explorer composition wrinkle

Current explorer empires:

| # | has | should be |
|---|-----|-----------|
| 1 | solarian | nebula |
| 2 | solarian | nebula |
| 3 | voidborn | solarian |
| 4 | voidborn | solarian |
| 5 | crimson | voidborn |
| 6 | crimson | voidborn |
| 7 | nebula | crimson |
| 8 | solarian | crimson |
| 9 | solarian | outerrim |
| 10 | nebula | outerrim |

Composition: **4 solarian, 2 nebula, 2 voidborn, 2 crimson, 0 outerrim.** A clean
2-per-band layout needs 2 of each, so renumbering alone cannot balance it — there
are two surplus solarian explorers and zero outerrim explorers.

## Decisions

1. **Renumber only** — keep every agent's personality, display name, empire, and
   game-server account intact; only the trailing number (and all local
   references to it) move. No content/empire rewriting.
2. **Slots 9 & 10 become empty outerrim placeholders** — stub configs for new
   outerrim explorer agents to be created later.
3. **Surplus solarian parked at 11 & 12** — the two extra solarian explorers move
   to `explorer-11` / `explorer-12` (overflow slots outside the band scheme),
   kept solarian until repurposed.
4. **Rewrite all reports** — including historical dated daily-summaries, so all
   report files consistently use the new numbering.

## The renumber map

| Source (current) | Empire | → Target | Reason |
|------------------|--------|----------|--------|
| explorer-7  | nebula   | **explorer-1**  | nebula band 1,2 |
| explorer-10 | nebula   | **explorer-2**  | |
| explorer-1  | solarian | **explorer-3**  | solarian band 3,4 |
| explorer-2  | solarian | **explorer-4**  | |
| explorer-3  | voidborn | **explorer-5**  | voidborn band 5,6 |
| explorer-4  | voidborn | **explorer-6**  | |
| explorer-5  | crimson  | **explorer-7**  | crimson band 7,8 |
| explorer-6  | crimson  | **explorer-8**  | |
| *(none)*    | outerrim | **explorer-9**  | NEW placeholder stub |
| *(none)*    | outerrim | **explorer-10** | NEW placeholder stub |
| explorer-8  | solarian | **explorer-11** | parked surplus |
| explorer-9  | solarian | **explorer-12** | parked surplus |

The two real solarian band slots (3, 4) are filled by the lowest-numbered
solarian sources (old -1, -2); the highest-numbered solarian sources (old -8, -9)
are parked at 11, 12.

This is a full permutation — every existing explorer moves, and slots 9/10 are
vacated (old -9 → -12, old -10 → -2). Execution **must stage through temporary
names/values** (e.g. `explorer-tmp-N`) to avoid mid-operation collisions (e.g.
old -7 → -1 while old -1 → -3).

Target id set among existing agents: {1,2,3,4,5,6,7,8,11,12} — all distinct,
verified a clean bijection.

## Constraints discovered

- **Renumbering is local-only bookkeeping.** Each agent's game-server identity
  (`credentials.json` `username` + session `player_id`) is fixed server-side and
  travels with the agent. Renumbering never touches server accounts.
- **`credentials.json` carries its own `empire` field** that already matches each
  agent's personality empire, so it travels along untouched (no edits needed).
- **Known cosmetic wart (no action):** old `explorer-7`'s server username
  literally contains the string `"Explorer-7"` (`"Eugene 'Explorer-7' Edwa"`),
  and it is moving to slot 1. Server usernames cannot be renamed, so its in-game
  display name stays cosmetically stale. Documented, not fixed. (No other
  explorer username embeds its number.)

## Relabel surface

All four layers are keyed on the renumber map above.

1. **Filesystem** — rename `data/agents/explorer-N/` directories. The whole dir
   travels (mbox.db, play_as_history.txt, .spacemolt-session.json,
   credentials.json, personality.json).
2. **personality.json** — update the `id` field inside each renamed agent
   (display `name`, `empire`, biography, motivations untouched).
3. **Databases** — `UPDATE` agent-id columns, staged through temp values to avoid
   permutation collisions:
   - `data/spacemolt-knowledge.db`: `experiences.agent_id`, `agents.id`,
     `market_snapshots.agent_id`, `ship_listings.agent_id`,
     `anomalies.detected_by`, `pois.detected_by`, `poi_resources.detected_by`,
     `storage_snapshots.agent_id`, `change_snapshots.detected_by`,
     `xp_observations.agent_id`
   - `data/daily-summary.db`: `snapshots.agent_id`,
     `faction_snapshots.founder_agent_id`
   - The migration should **discover** agent-id-bearing columns programmatically
     (scan every table's text columns for `explorer-%` values) rather than rely
     solely on this hardcoded list, in case rows exist elsewhere.
4. **Reports** — find-and-replace old→new ids across **all** files in
   `data/reports/`, including historical dated daily-summaries (`.md` + `.html`).

## New placeholders (slots 9 & 10)

Created as **stub `personality.json` only**:

```json
{
  "id": "explorer-9",
  "role": "Explorer",
  "empire": "outerrim",
  "placeholder": true
}
```

No `credentials.json`, no server account — clearly empty slots awaiting real
outerrim agents to be created later.

## Safety

A single migration script (or well-scoped tool) with:

- **DB backups first** — copy each target `.db` before any UPDATE.
- **Temp-staging** for both directory renames and SQL UPDATEs (two-phase:
  source → temp → target).
- **Dry-run mode** that prints the full plan (every rename, every UPDATE count,
  every report substitution) without mutating anything.
- **Verification pass** — after execution, assert no unmapped `explorer-*`
  remains, target id set is exactly {1..12} minus nothing unexpected, and pre/post
  row counts per table are conserved.

## Out of scope

- Any role other than explorer.
- Creating the actual outerrim agents for slots 9/10 (only stub placeholders).
- Renaming game-server accounts/usernames (not supported server-side).
- The `data/spacemolt-knowledge.pre-import-20260607.db` backup and any other
  archival DB snapshots.
