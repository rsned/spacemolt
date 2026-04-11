# XP Observation Tracking — Design

**Date:** 2026-04-11
**Status:** Approved

## Problem

The server API only returns skill XP values in a few places, making it hard to
understand which commands produce XP gains and for which skills. We want to
build a database of command-to-skill-XP mappings by observing changes before
and after every mutation command across all agents.

## Goals

- Capture skill XP deltas for every mutation command executed by agents
- Store action + target for granularity (e.g., does travel distance or crafting
  complexity affect XP?)
- Tag mission completion rewards separately so they can be filtered out
- Handle level-ups correctly by recording level and XP deltas independently
- Make the tracking opt-in (nil-safe KB check) so non-KB callers aren't broken

## Design

### Database Schema — Migration v27

New table `xp_observations` with one row per skill that changed per command:

```sql
CREATE TABLE xp_observations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'action',
    skill_id TEXT NOT NULL,
    xp_delta REAL NOT NULL,
    level_delta INTEGER NOT NULL DEFAULT 0,
    level_before INTEGER NOT NULL,
    level_after INTEGER NOT NULL,
    game_tick INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    mission_id TEXT DEFAULT NULL
);

CREATE INDEX idx_xp_obs_action ON xp_observations(action);
CREATE INDEX idx_xp_obs_skill ON xp_observations(skill_id);
CREATE INDEX idx_xp_obs_source ON xp_observations(source);
CREATE INDEX idx_xp_obs_agent ON xp_observations(agent_id);
```

Commands that produce no XP change produce no rows. A command that gives XP to
multiple skills produces multiple rows.

### Knowledge Base Types

```go
type XPObservation struct {
    ID          int64
    AgentID     string
    Action      string
    Target      string
    Source      string  // "action", "mission_reward"
    SkillID     string
    XPDelta     float64
    LevelDelta  int
    LevelBefore int
    LevelAfter  int
    GameTick    int64
    CreatedAt   time.Time
    MissionID   string
}

type XPSummaryRow struct {
    Action     string
    SkillID    string
    Source     string
    AvgXPDelta float64
    Count      int
}
```

### Knowledge Base Interface Additions

```go
RecordXPObservation(ctx context.Context, obs XPObservation) error
GetXPObservations(ctx context.Context, action string, limit int) ([]XPObservation, error)
GetXPSummary(ctx context.Context) ([]XPSummaryRow, error)
```

Both SQLiteKB and MemoryKB get implementations.

### Runner Integration

**New field** on `Runner`: `kb knowledge.Base` (nil-safe).

**Wiring**: `Manager` passes its existing `kb` through to Runner construction.

**Hook location** in `executeCycle()`:

1. Before `executeDecision()`: snapshot `Player.Skills` map if `isAction`.
2. After successful execution: compare skills after vs before, call
   `recordXPChanges()` for each skill with a delta.

**`recordXPChanges` method**:
- Iterates `skillsAfter`, compares against `skillsBefore`
- Builds `XPObservation` for each skill where level or XP changed
- Source: `"action"` by default; `"mission_reward"` if action is
  `"complete_mission"`
- Mission ID: set from `decision.Target` for mission completions
- Checks for skills in `after` but not `before` (new skill unlocked)
- Nil-checks `r.kb` and silently returns if no KB available

### Source Column Values

| Source           | When                        |
|------------------|-----------------------------|
| `action`         | Default for all commands    |
| `mission_reward` | `complete_mission` action   |

Extensible for future sources (e.g., `event_reward`, `level_bonus`).

## Files Changed

| File | Change |
|------|--------|
| `pkg/knowledge/base.go` | Add `XPObservation`, `XPSummaryRow` types; add 3 interface methods |
| `pkg/knowledge/sqlite_migrations.go` | Add migration v27 |
| `pkg/knowledge/sqlite.go` | Implement 3 new methods |
| `pkg/knowledge/memory.go` | Implement 3 new methods |
| `pkg/agent/runner.go` | Add `kb` field, snapshot/compare logic, `recordXPChanges` method |
| `pkg/agent/manager.go` | Pass `kb` to Runner construction |

## Non-Goals

- Computing absolute total XP (requires full xp_per_level curves)
- Real-time XP tracking UI (future work)
- Tracking XP from non-agent command sources (MCP bridge, debug tools)
