# Passive XP Detection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect and record passive skill XP gains (e.g. Engineering when a ship is fully power-loaded) by polling `get_skills` on a wall-clock timer in the agent runner, and relabel both new and existing such observations with `source = "passive_skill"`.

**Architecture:** Three small code changes plus one SQLite migration: (1) a new `PassiveSkillCheckInterval` constant; (2) updated source-mapping in `XPTracker.onXPChange` so `action ∈ {get_skills, login}` produces `source = "passive_skill"`; (3) a `lastPassiveSkillCheck` field on `Runner` and a periodic `GetSkills` call in `executeCycle`; (4) a one-shot migration that rewrites historical `xp_observations` rows where `action='login' AND skill_id='engineering'` to `source='passive_skill'`.

**Tech Stack:** Go 1.24+, SQLite (modernc.org/sqlite), existing `pkg/game`, `pkg/agent`, `pkg/knowledge` packages.

**Spec:** `docs/superpowers/specs/2026-04-29-passive-xp-detection-design.md`

---

## File map

| Path | Operation | Purpose |
|---|---|---|
| `pkg/game/constants.go` | modify | add `PassiveSkillCheckInterval = 20 * time.Minute` |
| `pkg/knowledge/xp_tracker.go` | modify | extend `source` switch in `onXPChange` |
| `pkg/knowledge/xp_tracker_test.go` | create | unit tests for the source mapping |
| `pkg/agent/runner.go` | modify | new `lastPassiveSkillCheck` field, init in `NewRunner`, periodic poll in `executeCycle` |
| `pkg/agent/runner_test.go` | modify | new test asserting `GetSkills` is invoked once per interval |
| `pkg/knowledge/sqlite_migrations.go` | modify | add migration version 32 |
| `pkg/knowledge/sqlite_test.go` | modify | new test for the migration-32 backfill |

---

## Task 1: Add `PassiveSkillCheckInterval` constant

**Files:**
- Modify: `pkg/game/constants.go` (add to the "Timing constants" block near line 97)

- [ ] **Step 1: Add the constant**

Edit `pkg/game/constants.go`. Inside the existing `// Timing constants` block (currently containing `MiningCycleTime`, `GameTickRate`, `ModuleInstallDelay`), add a new entry:

```go
// PassiveSkillCheckInterval is how often the agent runner injects a
// get_skills query to capture passive XP gains (e.g. Engineering when
// the ship is fully power-loaded). Wall-clock based, not tick-based,
// so reports can normalise to per-real-second or per-tick rates
// regardless of server tick-rate variance.
PassiveSkillCheckInterval = 20 * time.Minute
```

The block now reads:

```go
// Timing constants
const (
    MiningCycleTime    = 11 * time.Second // Time between mining operations
    GameTickRate       = 1 * time.Second  // Basic game tick rate
    ModuleInstallDelay = 10 * time.Second // Delay between module installations

    // PassiveSkillCheckInterval is how often the agent runner injects a
    // get_skills query to capture passive XP gains (e.g. Engineering when
    // the ship is fully power-loaded). Wall-clock based, not tick-based,
    // so reports can normalise to per-real-second or per-tick rates
    // regardless of server tick-rate variance.
    PassiveSkillCheckInterval = 20 * time.Minute
)
```

- [ ] **Step 2: Build to confirm it compiles**

Run: `go build ./pkg/game/...`
Expected: exits 0 with no output.

- [ ] **Step 3: Commit**

```bash
git add pkg/game/constants.go
git commit -m "feat(game): add PassiveSkillCheckInterval constant

20-minute wall-clock interval used by the agent runner to inject
periodic get_skills queries that capture passive XP gains (e.g.
Engineering when the ship is fully power-loaded)."
```

---

## Task 2: Update `XPTracker` source mapping for passive labels

**Files:**
- Test: `pkg/knowledge/xp_tracker_test.go` (create)
- Modify: `pkg/knowledge/xp_tracker.go` (lines 44–48 inside `onXPChange`)

The current logic at `pkg/knowledge/xp_tracker.go:44–48`:

```go
source := "action"
var missionID string
if action == "complete_mission" {
    source = "mission_reward"
    missionID = target
}
```

We extend it so `get_skills` and `login` map to `source = "passive_skill"`.

- [ ] **Step 1: Write the failing test file**

Create `pkg/knowledge/xp_tracker_test.go` with this content:

```go
package knowledge

import (
    "context"
    "testing"

    "github.com/rsned/spacemolt/pkg/game"
)

// captureKB records every XPObservation passed to RecordXPObservation
// so tests can inspect the source-mapping behaviour of XPTracker.
type captureKB struct {
    MemoryKB
    captured []XPObservation
}

func (c *captureKB) RecordXPObservation(_ context.Context, obs XPObservation) error {
    c.captured = append(c.captured, obs)
    return nil
}

// fakeXPClient is a no-op stub for game.XPCallbackSetter; XPTracker only
// uses it to register a callback, which the test invokes directly.
type fakeXPClient struct{ cb game.XPCallbackFunc }

func (f *fakeXPClient) SetXPCallback(fn game.XPCallbackFunc) { f.cb = fn }

func TestXPTracker_SourceMapping(t *testing.T) {
    cases := []struct {
        name       string
        action     string
        wantSource string
    }{
        {"mining action", "mine", "action"},
        {"travel action", "travel", "action"},
        {"complete_mission", "complete_mission", "mission_reward"},
        {"get_skills is passive", "get_skills", "passive_skill"},
        {"login is passive", "login", "passive_skill"},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            kb := &captureKB{}
            client := &fakeXPClient{}
            tracker := NewXPTracker(client, kb, "agent-1", nil)
            if tracker == nil {
                t.Fatalf("NewXPTracker returned nil")
            }
            if client.cb == nil {
                t.Fatalf("XPCallback was not installed")
            }

            before := map[string]game.Skill{"engineering": {Level: 5, XP: 100}}
            after := map[string]game.Skill{"engineering": {Level: 5, XP: 200}}
            beforeXP := map[string]float64{"engineering": 100}
            afterXP := map[string]float64{"engineering": 200}

            client.cb(tc.action, "", 1, before, after, beforeXP, afterXP, 12345)

            if len(kb.captured) != 1 {
                t.Fatalf("expected 1 observation, got %d", len(kb.captured))
            }
            got := kb.captured[0].Source
            if got != tc.wantSource {
                t.Errorf("source = %q, want %q", got, tc.wantSource)
            }
        })
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestXPTracker_SourceMapping -v`
Expected: FAIL on the `get_skills is passive` and `login is passive` cases — they will report `source = "action"` instead of `"passive_skill"`. The other three subtests pass.

- [ ] **Step 3: Update `onXPChange` to make the test pass**

Edit `pkg/knowledge/xp_tracker.go`. Replace lines 43–48:

```go
func (t *XPTracker) onXPChange(action, target string, quantity int, beforeSkills, afterSkills map[string]game.Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
    source := "action"
    var missionID string
    if action == "complete_mission" {
        source = "mission_reward"
        missionID = target
    }
```

with:

```go
func (t *XPTracker) onXPChange(action, target string, quantity int, beforeSkills, afterSkills map[string]game.Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
    source := "action"
    var missionID string
    switch action {
    case "complete_mission":
        source = "mission_reward"
        missionID = target
    case "get_skills", "login":
        // get_skills carries no XP grant of its own, and login XP is
        // accumulated since last logout — both are passive deltas.
        source = "passive_skill"
    }
```

- [ ] **Step 4: Update the doc-comment for `XPObservation.Source`**

Edit `pkg/knowledge/base.go:367`. Replace:

```go
    Source      string // "action", "mission_reward"
```

with:

```go
    Source      string // "action", "mission_reward", "passive_skill"
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestXPTracker_SourceMapping -v`
Expected: PASS for all five subtests.

- [ ] **Step 6: Run the wider knowledge package tests to confirm no regressions**

Run: `go test ./pkg/knowledge/...`
Expected: PASS.

- [ ] **Step 7: Lint**

Run: `golangci-lint run ./pkg/knowledge/...`
Expected: no new findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/knowledge/xp_tracker.go pkg/knowledge/xp_tracker_test.go pkg/knowledge/base.go
git commit -m "feat(knowledge): label get_skills and login XP as passive_skill

XPTracker.onXPChange now maps action='get_skills' and action='login'
to source='passive_skill'. A get_skills response carries no XP grant,
so any non-zero delta is by definition passive accumulation since the
previous snapshot; login deltas are passive accumulation since the
previous logout. Documents the new source value on XPObservation."
```

---

## Task 3: Periodic `GetSkills` poll in `Runner`

**Files:**
- Modify: `pkg/agent/runner.go` (Runner struct, NewRunner, executeCycle)
- Modify: `pkg/agent/runner_test.go` (add new test)

- [ ] **Step 1: Write the failing test**

Add to `pkg/agent/runner_test.go` at the bottom of the file (after existing tests):

```go
func TestRunner_PassiveSkillCheck_FiresOncePerInterval(t *testing.T) {
    agent := &mockAgent{
        id: "test-agent",
        decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
            return Decision{
                Action:     "wait",
                Reasoning:  "idle",
                Confidence: 1.0,
            }, nil
        },
    }

    client := newMockGameClient()
    client.state.CurrentTick = 100

    config := DefaultRunnerConfig()
    runner := NewRunner(agent, client, config)
    ctx := context.Background()

    // First cycle: not yet due — runner was just created, lastPassiveSkillCheck
    // is "now", so no get_skills should fire.
    if err := runner.executeCycle(ctx); err != nil {
        t.Fatalf("first executeCycle: %v", err)
    }
    for _, a := range client.actionsRecorded {
        if a == "get_skills" {
            t.Fatalf("get_skills fired prematurely on first cycle: %v", client.actionsRecorded)
        }
    }

    // Force the timer into the past so the next cycle is "due".
    runner.mu.Lock()
    runner.lastPassiveSkillCheck = time.Now().Add(-2 * game.PassiveSkillCheckInterval)
    runner.mu.Unlock()

    // Advance the game tick so the runner is allowed to act this cycle.
    client.state.CurrentTick = 200

    if err := runner.executeCycle(ctx); err != nil {
        t.Fatalf("second executeCycle: %v", err)
    }

    skillCalls := 0
    for _, a := range client.actionsRecorded {
        if a == "get_skills" {
            skillCalls++
        }
    }
    if skillCalls != 1 {
        t.Errorf("expected 1 get_skills call after interval elapsed, got %d (recorded: %v)",
            skillCalls, client.actionsRecorded)
    }

    // Third cycle: not due again because the previous cycle reset the timer.
    client.state.CurrentTick = 300
    before := len(client.actionsRecorded)
    if err := runner.executeCycle(ctx); err != nil {
        t.Fatalf("third executeCycle: %v", err)
    }
    for _, a := range client.actionsRecorded[before:] {
        if a == "get_skills" {
            t.Fatalf("get_skills fired again before next interval (recorded since prev: %v)",
                client.actionsRecorded[before:])
        }
    }
}

func TestRunner_PassiveSkillCheck_SkippedWhilePaused(t *testing.T) {
    agent := &mockAgent{id: "test-agent"}
    client := newMockGameClient()
    config := DefaultRunnerConfig()
    runner := NewRunner(agent, client, config)

    // Force the timer into the past, then pause.
    runner.mu.Lock()
    runner.lastPassiveSkillCheck = time.Now().Add(-2 * game.PassiveSkillCheckInterval)
    runner.paused = true
    runner.mu.Unlock()

    if err := runner.executeCycle(context.Background()); err != nil {
        t.Fatalf("executeCycle: %v", err)
    }
    for _, a := range client.actionsRecorded {
        if a == "get_skills" {
            t.Fatalf("get_skills fired while paused: %v", client.actionsRecorded)
        }
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/agent/ -run TestRunner_PassiveSkillCheck -v`
Expected: FAIL — the field `lastPassiveSkillCheck` does not exist yet, so the file will not compile. The error will be along the lines of `runner.lastPassiveSkillCheck undefined`.

- [ ] **Step 3: Add the `lastPassiveSkillCheck` field on `Runner`**

Edit `pkg/agent/runner.go`. In the `Runner` struct (currently at lines 32–68), add a new field inside the `// State` block right after `lastActionTime`:

```go
    // State
    mu             sync.RWMutex
    running        bool
    lastActionTick int64
    lastActionTime time.Time
    // lastPassiveSkillCheck is the wall-clock time of the most recent
    // runner-injected get_skills poll. Used to throttle passive XP
    // detection to game.PassiveSkillCheckInterval.
    lastPassiveSkillCheck time.Time
    crashCount     int
    stopCh         chan struct{}
    stopOnce       sync.Once
```

- [ ] **Step 4: Initialise the field in `NewRunner`**

Edit `pkg/agent/runner.go` at lines 97–111. Update `NewRunner` to set `lastPassiveSkillCheck` to `time.Now()` so the first poll fires roughly one interval after startup, not immediately:

```go
// NewRunner creates a new agent runner
func NewRunner(agent Agent, gameClient game.GameClient, config RunnerConfig) *Runner {
    if config.Logger == nil {
        config.Logger = log.Default()
    }

    return &Runner{
        agent:                 agent,
        gameClient:            gameClient,
        config:                config,
        stopCh:                make(chan struct{}),
        logger:                config.Logger,
        history:               NewHistory(1000), // Track last 1000 actions
        lastPassiveSkillCheck: time.Now(),
    }
}
```

- [ ] **Step 5: Add the periodic poll inside `executeCycle`**

Edit `pkg/agent/runner.go`. Insert the passive-skill check immediately after the paused-check block and before `state := r.gameClient.GetState()`. The current code at lines 220–233 reads:

```go
func (r *Runner) executeCycle(ctx context.Context) error {
    // Skip cycle entirely if paused
    r.mu.RLock()
    paused := r.paused
    r.mu.RUnlock()
    if paused {
        return nil
    }

    // Get current game state
    state := r.gameClient.GetState()
    if state == nil {
        return fmt.Errorf("game state is nil")
    }
```

Replace with:

```go
func (r *Runner) executeCycle(ctx context.Context) error {
    // Skip cycle entirely if paused
    r.mu.RLock()
    paused := r.paused
    r.mu.RUnlock()
    if paused {
        return nil
    }

    // Periodic passive-skill check. get_skills has no tick cost, so this
    // can run independent of tick advancement. The XPCallback wired into
    // the game client will record any non-zero deltas with
    // source="passive_skill" via XPTracker.
    r.mu.RLock()
    lastPassiveCheck := r.lastPassiveSkillCheck
    r.mu.RUnlock()
    if time.Since(lastPassiveCheck) >= game.PassiveSkillCheckInterval {
        r.logger.Printf("[%s] -> GetSkills() (passive XP check)", r.agent.ID())
        if err := r.gameClient.GetSkills(ctx); err != nil {
            r.logger.Printf("[%s] passive get_skills failed: %v", r.agent.ID(), err)
        }
        r.mu.Lock()
        r.lastPassiveSkillCheck = time.Now()
        r.mu.Unlock()
    }

    // Get current game state
    state := r.gameClient.GetState()
    if state == nil {
        return fmt.Errorf("game state is nil")
    }
```

The timestamp is updated regardless of error so that a transient failure doesn't cause every subsequent cycle to retry.

- [ ] **Step 6: Run the new tests to verify they pass**

Run: `go test ./pkg/agent/ -run TestRunner_PassiveSkillCheck -v`
Expected: PASS for both `TestRunner_PassiveSkillCheck_FiresOncePerInterval` and `TestRunner_PassiveSkillCheck_SkippedWhilePaused`.

- [ ] **Step 7: Run the rest of the agent tests to confirm no regressions**

Run: `go test ./pkg/agent/...`
Expected: PASS. Pay particular attention to `TestRunner_ExecuteCycle_ActionCommand` and `TestRunner_ExecuteCycle_QueryCommand` — these construct a runner and run `executeCycle` once. Because `NewRunner` now initialises `lastPassiveSkillCheck = time.Now()`, the passive check should not fire on the very first cycle, so existing assertions about `actionsRecorded` length should still hold. If a pre-existing test starts to fail because the runner adds a `get_skills` entry, use the existing `filterActions` helper at the top of `runner_test.go` to exclude `get_skills` from the comparison — that is its purpose.

- [ ] **Step 8: Lint**

Run: `golangci-lint run ./pkg/agent/...`
Expected: no new findings.

- [ ] **Step 9: Commit**

```bash
git add pkg/agent/runner.go pkg/agent/runner_test.go
git commit -m "feat(agent): periodic passive-skill XP poll in Runner

Adds a wall-clock timer (lastPassiveSkillCheck) on Runner that injects
a get_skills query every PassiveSkillCheckInterval (20m). The existing
XPCallback path captures any resulting delta and labels it as
source='passive_skill' via XPTracker. Skipped while paused; runs
independently of game-tick advancement since get_skills has no tick cost."
```

---

## Task 4: Backfill migration for historical engineering login rows

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` (append migration version 32)
- Modify: `pkg/knowledge/sqlite_test.go` (add backfill test)

- [ ] **Step 1: Write the failing test**

Add the following function at the end of `pkg/knowledge/sqlite_test.go` (after `TestSQLiteKB_Migration3_LastVisitedTickBackfill`):

```go
func TestSQLiteKB_Migration32_PassiveSkillBackfill(t *testing.T) {
    // Simulate the post-migration-31 state: schema_migrations rows for
    // versions 1, 2, and 31 are recorded, and an xp_observations table
    // exists (matching the shape produced by initial_schema + migration 2)
    // pre-populated with rows representing the pre-passive_skill world.
    dbPath := filepath.Join(t.TempDir(), "test.db")
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        t.Fatalf("open db: %v", err)
    }
    t.Cleanup(func() { _ = db.Close() })

    if _, err := db.Exec(`
        CREATE TABLE schema_migrations (
            version INTEGER PRIMARY KEY,
            applied_at TEXT NOT NULL
        );
        INSERT INTO schema_migrations (version, applied_at) VALUES
            (1, datetime('now')),
            (2, datetime('now')),
            (31, datetime('now'));

        CREATE TABLE xp_observations (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            agent_id TEXT NOT NULL,
            action TEXT NOT NULL,
            target TEXT NOT NULL DEFAULT '',
            source TEXT NOT NULL,
            skill_id TEXT NOT NULL,
            xp_delta REAL NOT NULL,
            level_delta INTEGER NOT NULL,
            level_before INTEGER NOT NULL,
            level_after INTEGER NOT NULL,
            game_tick INTEGER NOT NULL,
            created_at TEXT NOT NULL DEFAULT (datetime('now')),
            mission_id TEXT NOT NULL DEFAULT '',
            quantity INTEGER NOT NULL DEFAULT 1
        );

        -- Engineering login rows (should be relabelled to passive_skill).
        INSERT INTO xp_observations
            (agent_id, action, target, source, skill_id, xp_delta, level_delta, level_before, level_after, game_tick)
        VALUES
            ('explorer-1', 'login', '', 'action', 'engineering', 405.0, 0, 18, 18, 693917),
            ('fighter-5',  'login', '', 'action', 'engineering', 738.0, 0,  6,  6, 693309);

        -- Control rows (must be untouched):
        --  - login row for a non-engineering skill
        --  - non-login engineering row (action=mine)
        --  - already-passive engineering login row (idempotency check)
        INSERT INTO xp_observations
            (agent_id, action, target, source, skill_id, xp_delta, level_delta, level_before, level_after, game_tick)
        VALUES
            ('miner-1',    'login', '',           'action',         'mining',      50.0, 0, 12, 12, 600000),
            ('miner-1',    'mine',  'iron_ore',   'action',         'engineering',  3.0, 0, 10, 10, 600100),
            ('explorer-1', 'login', '',           'passive_skill',  'engineering', 100.0, 0, 18, 18, 693000);
    `); err != nil {
        t.Fatalf("build pre-migration fixture: %v", err)
    }

    // Run migrations — only migration 32 should be applied.
    if err := runMigrations(db); err != nil {
        t.Fatalf("runMigrations: %v", err)
    }

    // Assert migration 32 was recorded.
    var rows int
    if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 32`).Scan(&rows); err != nil {
        t.Fatalf("schema_migrations count: %v", err)
    }
    if rows != 1 {
        t.Errorf("schema_migrations rows for version 32 = %d, want 1", rows)
    }

    // Assert the two engineering login rows are now passive_skill.
    var got int
    if err := db.QueryRow(`
        SELECT COUNT(*) FROM xp_observations
        WHERE action = 'login' AND skill_id = 'engineering' AND source = 'passive_skill'
    `).Scan(&got); err != nil {
        t.Fatalf("count relabelled rows: %v", err)
    }
    if got != 3 { // 2 newly relabelled + 1 already-passive control
        t.Errorf("passive engineering login rows = %d, want 3", got)
    }

    // Assert no engineering login rows remain with source='action'.
    if err := db.QueryRow(`
        SELECT COUNT(*) FROM xp_observations
        WHERE action = 'login' AND skill_id = 'engineering' AND source = 'action'
    `).Scan(&got); err != nil {
        t.Fatalf("count residual action rows: %v", err)
    }
    if got != 0 {
        t.Errorf("residual action-source engineering login rows = %d, want 0", got)
    }

    // Assert control rows are untouched.
    var src string
    if err := db.QueryRow(`
        SELECT source FROM xp_observations
        WHERE agent_id = 'miner-1' AND action = 'login' AND skill_id = 'mining'
    `).Scan(&src); err != nil {
        t.Fatalf("read mining login source: %v", err)
    }
    if src != "action" {
        t.Errorf("non-engineering login row source = %q, want %q", src, "action")
    }
    if err := db.QueryRow(`
        SELECT source FROM xp_observations
        WHERE agent_id = 'miner-1' AND action = 'mine'
    `).Scan(&src); err != nil {
        t.Fatalf("read engineering mine source: %v", err)
    }
    if src != "action" {
        t.Errorf("non-login engineering row source = %q, want %q", src, "action")
    }

    // Idempotency: re-running migrations must be a no-op for version 32.
    if err := runMigrations(db); err != nil {
        t.Fatalf("second runMigrations: %v", err)
    }
    if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 32`).Scan(&rows); err != nil {
        t.Fatalf("schema_migrations count after re-run: %v", err)
    }
    if rows != 1 {
        t.Errorf("schema_migrations rows for version 32 after re-run = %d, want 1", rows)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestSQLiteKB_Migration32_PassiveSkillBackfill -v`
Expected: FAIL — migration 32 is not registered yet, so `schema_migrations` will have 0 rows for version 32 and the engineering login rows will still show `source='action'`.

- [ ] **Step 3: Add migration version 32**

Edit `pkg/knowledge/sqlite_migrations.go`. In the `migrations()` function (lines 34–75), append a new entry to the returned slice, immediately after the version-31 entry:

```go
        {
            // Backfill historical engineering login XP rows to use
            // source='passive_skill'. See spec
            // docs/superpowers/specs/2026-04-29-passive-xp-detection-design.md §4.
            // Scope is intentionally limited to skill_id='engineering' because
            // that is the only confirmed passive skill. Forward-going code in
            // XPTracker.onXPChange labels all future get_skills/login deltas
            // as passive_skill regardless of skill, so this narrowness only
            // applies to historical data.
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

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestSQLiteKB_Migration32_PassiveSkillBackfill -v`
Expected: PASS.

- [ ] **Step 5: Run the wider knowledge package tests to confirm no regressions**

Run: `go test ./pkg/knowledge/...`
Expected: PASS.

- [ ] **Step 6: Lint**

Run: `golangci-lint run ./pkg/knowledge/...`
Expected: no new findings.

- [ ] **Step 7: Commit**

```bash
git add pkg/knowledge/sqlite_migrations.go pkg/knowledge/sqlite_test.go
git commit -m "feat(knowledge): migration 32 — backfill engineering login XP as passive_skill

Rewrites historical xp_observations rows where source='action',
action='login', and skill_id='engineering' to source='passive_skill'.
Engineering is the only confirmed passive skill today; non-engineering
login rows are left untouched until additional passive sources are
identified."
```

---

## Task 5: Whole-tree verification

**Files:** none modified — this task is verification only.

- [ ] **Step 1: Build the entire module**

Run: `go build ./...`
Expected: exits 0 with no output.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Lint the whole tree**

Run: `golangci-lint run ./...`
Expected: no new findings introduced by these changes (pre-existing findings unrelated to this work are acceptable but should be diffable to the same set as on `main` before this branch).

- [ ] **Step 4: Sanity-check the design spec against the implemented code**

Confirm by reading:

- `pkg/game/constants.go` — `PassiveSkillCheckInterval = 20 * time.Minute` is present.
- `pkg/knowledge/xp_tracker.go` — the switch in `onXPChange` maps `get_skills` and `login` to `passive_skill`.
- `pkg/agent/runner.go` — `Runner` has `lastPassiveSkillCheck`, `NewRunner` initialises it to `time.Now()`, and `executeCycle` polls `GetSkills` when the interval has elapsed and updates the timestamp.
- `pkg/knowledge/sqlite_migrations.go` — migration 32 with the engineering-login-only `UPDATE` is present.

If anything is missing or drifted, fix it inline and re-run the previous steps.

---

## Spec coverage check

| Spec section | Implemented in |
|---|---|
| §1 New constant `PassiveSkillCheckInterval` | Task 1 |
| §2 Runner `lastPassiveSkillCheck` field, init, periodic poll | Task 3 |
| §3 XPTracker source attribution (`get_skills`, `login` → `passive_skill`) | Task 2 |
| §4 Backfill migration version 32 | Task 4 |
| Tests — `xp_tracker_test.go` source mapping | Task 2 |
| Tests — `runner_test.go` interval enforcement and pause-skip | Task 3 |
| Tests — migration backfill behaviour and idempotency | Task 4 |
