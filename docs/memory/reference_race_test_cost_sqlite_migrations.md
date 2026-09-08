---
name: reference_race_test_cost_sqlite_migrations
description: "Why `go test -race ./...` took 11+ min (knowledge timed out, worker 491s): pure-Go SQLite migration replay costs ~3.5s per KB open under -race, plus real time.Sleep in six worker verbs. Template clone fix shipped 2026-09-08; sleep injection still pending."
metadata:
  type: reference
---

Measured 2026-09-08 on the 8-core dev box. `go test -race ./...` = 11m20s wall;
pkg/knowledge alone FAILED its 10-minute timeout (225 tests summing to 1580
test-seconds), pkg/worker took 491s. With the Makefile's `-timeout=5m` both
were killed every run, which is why so much code never got race-tested.

**Cause 1 — SQLite migrations under the race detector (was ~80% of it).**
modernc's pure-Go SQLite re-parses the whole schema after each of the 134
DDL statements in `runMigrations`; race instrumentation makes that ~35x
slower. One `NewSQLiteKB(":memory:")` = 0.1s plain, 3.1–3.7s under -race.
knowledge opened 191 fresh KBs per run, worker 74, play_as 15.
**Fix (shipped 09-08):** `knowledge.MigratedTemplate()` migrates ONCE per
process and caches the ~1 MB file; `knowledge.WriteMigratedDB(path)` clones
it (90ms under -race). Tests use `knowledgetest.Path(t)` / `NewKB(t)`
(other packages) or `testDBPath(t)` (inside pkg/knowledge). Never write
`knowledge.Config{DBPath: ":memory:"}` in a test again — also note that
with modernc every pool connection to `:memory:` is a SEPARATE empty DB,
so the old pattern only worked because the pool reused one idle conn.

Result under -race: knowledge 600s+ → 62s · play_as 139 → 20 · agent 28 → 19 ·
market 147 → 87 (the rest is `TestPruneOrders_BatchesLargeDeletes`, a real
51k-row test) · worker 491 → 310.

**Cause 2 — real wall-clock sleeps (NOT race-related, still open).**
worker: buy_directed, deliver, fit_drones, hunt_wildlife, capture, autopilot
call `time.Sleep(game.SleepQuick/SleepTick)` directly, and the freight tests
never set `deps.sleep` (30s test). 52 tests = 215s of pure sleeping either
way. game: 115s of its 124s is 15s fake-server delays in the login/register
timeout tests, 5s blocks, and a full SleepTick in the poller test.
**Next step:** extend the `deps.sleep` pattern (freight/craft/mine/mission
already have it) to those six verbs; make the game fake-server delay and
auth timeout configurable. Expected: worker ~95s, whole race run 2–3 min.
This is what the pre-commit hook's "300s worker gate" in
[[feedback_standing_rules]] was actually hitting.

Unrelated failure seen in the run: `TestServerCommandsCoveredByClient` fails
because server command `arena` has no client coverage.
