# Passive XP Detection — Design

**Status:** Design approved, pending implementation plan
**Date:** 2026-04-29

## Problem

Some skills accumulate XP passively, independent of any action the agent takes. The first known example is **Engineering**: while the agent's ship is fully power-loaded, the server grants +1 XP per game tick. Other passive sources may exist and emerge over time.

Today, the only mechanism that captures these gains is the startup skill snapshot taken at login or reconnect. As a result, `xp_observations` rows for passive skills appear only as login-time bursts (e.g. action=`login`, +405 XP), with no resolution into per-tick or per-period data.

We need a lightweight, generic way to detect and record passive XP between logins, with attribution that makes it distinguishable from action-driven XP in reports.

## Constraints and non-goals

- **No new server protocol.** The `state_update` and `tick` events do not carry per-skill XP; only commands that return skill data (e.g. `get_skills`, `login`) refresh the snapshot.
- **`get_skills` is free of tick cost** and lightweight on the WebSocket — polling is acceptable.
- **Perfection is not required.** Action responses that include skill data (e.g. `mine`, `travel`) will continue to absorb passive deltas into action attribution. The reporting layer can compensate by knowing the passive baseline rate per skill.
- **No new goroutines per agent** if avoidable. The agent runner already runs a periodic loop; reuse it.

## Approach

Approach **B** from brainstorming: piggyback the agent runner's existing decision loop. On each cycle, check a wall-clock timer; when it elapses, inject a `get_skills` query. The existing `XPCallback` → `XPTracker` path captures any non-zero deltas and writes them to `xp_observations`.

Approach C (server pushes XP via tick events) was ruled out — the server does not carry XP on those events.

## Components

### 1. New constant

In `pkg/game/constants.go`:

```go
// PassiveSkillCheckInterval is how often the agent runner injects a
// get_skills query to capture passive XP gains (e.g. Engineering when
// the ship is fully power-loaded). Wall-clock based, not tick-based.
PassiveSkillCheckInterval = 20 * time.Minute
```

20 minutes is the midpoint of the 15–30 min range discussed. It is a wall-clock duration so that reports can normalise to per-real-second or per-tick rates regardless of server tick-rate variance.

### 2. Runner changes — `pkg/agent/runner.go`

**New field on `Runner`:**
```go
lastPassiveSkillCheck time.Time
```

**Initialisation** (in `NewRunner`): set to `time.Now()` so the first check fires roughly one interval after startup.

**Logic** (added near the top of `executeCycle`, before the canAct gate):

- Skip if paused.
- If `time.Since(r.lastPassiveSkillCheck) >= game.PassiveSkillCheckInterval`:
  1. Call `r.gameClient.GetSkills(actionCtx)`.
  2. Update `r.lastPassiveSkillCheck = time.Now()` (under lock) regardless of error, so a transient failure does not cause every subsequent cycle to re-issue the call.
  3. Log at the same level as other runner-injected queries.

The check is independent of tick advancement (`get_skills` has no tick cost) and independent of whether an action just ran. If a recent action response refreshed the skill snapshot, the resulting delta will be ~0 and `XPTracker` already skips zero deltas (`if xpDelta == 0 { continue }` at `pkg/knowledge/xp_tracker.go:74`), so the only cost is one cheap WS round-trip per interval per agent.

### 3. XPTracker source attribution — `pkg/knowledge/xp_tracker.go`

Today, the source-mapping logic in `onXPChange` is:

```go
source := "action"
var missionID string
if action == "complete_mission" {
    source = "mission_reward"
    missionID = target
}
```

Extend to:

```go
source := "action"
var missionID string
switch action {
case "complete_mission":
    source = "mission_reward"
    missionID = target
case "get_skills", "login":
    source = "passive_skill"
}
```

**Rationale:** a `get_skills` response carries no XP grant, so any non-zero delta observed in its callback is, by definition, passive accumulation since the last snapshot. The same applies to `login` — its delta represents passive XP earned since the last logout. Treating both as `passive_skill` cleanly unifies their semantics and supports the existing login-time entries, which the user data sample shows already exist (`4790|explorer-1|login|...|engineering|405.0|...`).

This is a behavioural change for the historical `login` rows going forward. To make historical data consistent rather than splitting reports across two source values, **a one-shot backfill migration** rewrites pre-existing engineering login rows (see §4 below).

### 4. Backfill migration — `pkg/knowledge/sqlite_migrations.go`

Add migration **version 32** (numbering follows the post-collapse convention used by version 31; see comment in `sqlite_migrations.go:50–53`):

```go
{
    version: 32,
    name:    "relabel_engineering_login_xp_as_passive_skill",
    sql: `
        UPDATE xp_observations
        SET source = 'passive_skill'
        WHERE source = 'action'
          AND action = 'login'
          AND skill_id = 'engineering';
    `,
},
```

Scope of the backfill is intentionally narrow — only `skill_id = 'engineering'` — because Engineering is the only skill we currently know to accumulate passively. Login rows for other skills could in theory also be passive, but until we observe a non-zero passive rate for them we leave them as `action` to avoid retroactively reclassifying rows whose semantics are not yet confirmed.

If additional passive skills are identified in future, a follow-up migration can broaden the scope (e.g. `WHERE skill_id IN (...)`). The forward-going code change in §3 is already broad — it labels *all* future `login` and `get_skills` deltas as `passive_skill` regardless of skill — so this narrowness only applies to historical data.

## Source field semantic table (after change)

| `source` value     | Meaning                                                          | Triggering action labels        |
|--------------------|------------------------------------------------------------------|---------------------------------|
| `action`           | XP earned from a deliberate game action                          | `mine`, `travel`, `craft`, …    |
| `mission_reward`   | XP from completing a mission                                     | `complete_mission`              |
| `passive_skill`    | XP detected via a skill snapshot poll, attributable to passive accumulation since previous snapshot | `get_skills`, `login`           |

## Data flow

```
Runner cycle
   │
   ├─ time.Since(lastPassiveSkillCheck) >= 20m ?
   │     │
   │     └─ yes → GameClient.GetSkills(ctx)
   │                  │
   │                  └─ server response → XPCallback fires with action="get_skills"
   │                          │
   │                          └─ XPTracker.onXPChange
   │                                  └─ source = "passive_skill"
   │                                  └─ kb.RecordXPObservation(...)
   │
   └─ normal action / decision / tick checks
```

## Reporting implications

- Per-skill passive rate per agent can be computed by selecting
  `xp_observations WHERE source='passive_skill' AND skill_id=?` ordered by `game_tick`,
  and dividing total `xp_delta` by `(max_game_tick - min_game_tick)`.
- Active-action attribution noise (e.g. `mine` rows that secretly contain a small Engineering passive component) can be netted out in reports by subtracting the known per-tick passive rate from action rows for skills with a known passive source.
- This is acceptance of the imperfection the user explicitly approved during brainstorming: "other actions that run (like 'mine' or 'travel') will also start to collect the passive changes as well, but knowing they exist we should be able to handle those cases when generating reports on it going forward."

## Tests

- **`pkg/knowledge/xp_tracker_test.go`** — add cases asserting `source == "passive_skill"` when `action` is `get_skills` or `login`, and `source == "action"` for unrelated actions.
- **`pkg/agent/runner_test.go`** — with a mock `GameClient` and an injectable clock (or by setting `lastPassiveSkillCheck` directly to a time in the past), assert that one `executeCycle` call results in exactly one `GetSkills` invocation, and that subsequent cycles within the interval do not re-issue it.
- Verify that the runner does not call `GetSkills` while `paused == true`.

## Tests (additions for §4)

- **`pkg/knowledge/sqlite_migrations_test.go`** — seed an in-memory DB with sample rows matching the pre-migration shape (`source='action'`, `action='login'`, `skill_id='engineering'`) and a control row (e.g. `skill_id='mining'`), run migrations to head, and assert engineering rows have `source='passive_skill'` while the control row is untouched.

## Out of scope

- Splitting passive XP out of action-attributed rows (e.g. attributing 1 of the +5 XP from a `mine` to passive Engineering). Reports handle this analytically.
- Per-skill configuration of passive rate or polling interval. Single global interval for now.
- Detection of *new* passive skills beyond Engineering — they will surface naturally in `xp_observations` once this lands and can be analysed retroactively.
- Broadening the backfill in §4 to non-engineering skills. Deferred until a second passive skill is confirmed.
