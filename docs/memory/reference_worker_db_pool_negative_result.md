---
name: reference_worker_db_pool_negative_result
description: "NEGATIVE RESULT — capping worker SQLite pools 25/5 -> 4/2 did NOT reduce worker CPU; measured higher after. Don't re-try this as a perf fix."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 8c9098dc-7c7c-4768-bb10-b65a2ce84115
  modified: 2026-07-20T16:29:13.990Z
---

2026-07-20. Shipped on main as `f7646cd` and deployed to all non-hauler fleets. Keep the change
(a worker querying on a ~10s cadence genuinely cannot use 25 connections), but **do not describe
it as a performance fix, and do not repeat the experiment expecting a win.**

**Hypothesis:** each worker opened BOTH `knowledge.NewSQLiteKB` and `market.Open` at the package
default pool (25 open / 5 idle) — `cmd/worker/main.go`. With ~110 workers on one box against the
same two SQLite files that is up to ~2,750 potential connections per DB, so capping the pools
should cut CPU and WAL lock contention.

**Measured (ps aggregate over all `worker` procs):**

| | before | after |
|---|---|---|
| worker CPU | 381.9% / 111 procs | 463.5% / 112 procs |
| mean per worker | 3.44% | 4.14% |
| load average | 16.42 | 10.56 |

Worker CPU went **up**. Load fell, but that is confounded: `arbitrage-scanner` alone was 56.8%
in the baseline sample, and load includes IO wait from everything on the box. Caveat not leaned
on: the "after" sample was ~15 min post-restart, so catch-up captures may inflate it — proving
it either way needs a controlled A/B nobody has run.

**Why the reasoning was wrong:** idle pooled connections are cheap — `database/sql` does not spin
them. The "hundreds of `database/sql` opener goroutines" observed when the `pkg/worker` race suite
timed out were a *symptom of contention under test load*, not the steady-state CPU driver.

**Where the load actually is:** the single `arbitrage-scanner` process out-consumes any worker,
and the dominant structural cost is process count itself (~110 Go runtimes, each re-parsing the
same read-only catalogs and recomputing the same reference prices). That is the shared-process /
sharding question in [[project_generalist_agent_selector]]'s neighborhood — see the queued
write-up idea rather than another pool tweak.
