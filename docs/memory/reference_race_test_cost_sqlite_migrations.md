---
name: reference_race_test_cost_sqlite_migrations
description: "Why `go test -race ./...` took 11+ min (knowledge timed out, worker 491s) and how it got to 2 min on 2026-09-08: pure-Go SQLite migration replay (~3.5s per KB open under -race, fixed by template clone), real time.Sleep in worker verbs (fixed by the settle seam), and a Client.Close mutex bug that cost 5s per close."
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

**Cause 2 — real wall-clock sleeps (fixed 09-08, same day).**
worker: buy_directed, deliver, fit_drones, hunt_wildlife, capture, autopilot,
craft_node called `time.Sleep(game.SleepQuick/SleepTick)` directly — 215s of
pure sleeping per run. **Fix:** `pkg/worker/settle.go` — every settle wait is
`settle(ctx, d)` and every nil-fallback poll sleep (`craftPollSleepFunc`,
which backs `deps.sleep`, `craftPollSleep`, `minePollSleep`) routes through
the package var `sleepFunc`; `main_test.go`'s TestMain swaps it for an
instant stand-in. Never write `time.Sleep` in pkg/worker again — use
`settle(ctx, game.SleepX)`. Tests needing real cancel semantics still inject
their own deps sleep. worker: 224s → 2s plain, 491s → 12s under -race.

**Cause 3 — `Client.Close` held `c.mu` while waiting for goroutines** (a real
bug, not test design). The listen goroutine takes `c.mu` to record the
disconnect on its way out, so EVERY Close sat out its full 5s safety timeout
("Warning: Timeout waiting for goroutines to exit on Close" in every log).
Fixed by unlocking before the wait; `TestClose_ReturnsPromptly` guards it.
Also: `Client.authTimeout` (default 10s) and `MCPGameClient.pollInterval`
(default SleepTick) are now fields so tests shorten them. game: 124s → 22s.

**Result:** `go test -race -count=1 -timeout=5m ./...` = **2m01s wall**
(was 11m20s with knowledge killed by the timeout). Slowest now: market 72s
(the 51k-row prune test), knowledge 37s, assets 32s. The pre-commit hook's
300s worker gate is no longer an issue (worker = 12s under -race).

Unrelated failure seen in the run: `TestServerCommandsCoveredByClient` fails
because server command `arena` has no client coverage.
