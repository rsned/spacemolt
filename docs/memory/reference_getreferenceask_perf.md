---
name: reference_getreferenceask_perf
description: "GetReferenceAsk ~3.3s/call on 190M-row market_orders (no (item_id,side,station_id,captured_at) index) — mission-runner price-gate loops burned a full core per hot worker; SIGQUIT goroutine dump is the diagnosis tool"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 8c9098dc-7c7c-4768-bb10-b65a2ce84115
  modified: 2026-07-21T04:12:21.946Z
---

**Symptom (2026-07-20):** a handful of mission-runner workers (fighter-8 etc.) pinned ~90-100% of a core each while looking behaviorally normal in logs. `ps` %CPU is a LIFETIME average — use `pidstat -p <pids> 5 2` for instantaneous truth; only 1 of the 5 "hot-looking" workers was actually spinning.

**Diagnosis technique:** `strace` is blocked (ptrace restricted); workers expose no pprof. `kill -QUIT <pid>` makes the Go runtime dump all goroutine stacks to stderr → captured in the fleet's overmind log (grep "SIGQUIT: quit"); the supervisor auto-restarts the worker (single fresh login is not rate-gated). The running/runnable goroutine stack names the culprit.

**Root cause:** `pkg/market.(*Collector).GetReferenceAsk` (pkg/market/query.go:285) called from `pkg/worker.Missions` price-gate — per-station-latest CTE over `market_orders` (190M rows, ~3.3s/call) with no covering index for `(item_id, side) → station_id, captured_at`. Mission runners at busy boards call it in a loop → continuous 3s queries = a full core.

**Fix:** `idx_orders_item_side_station_time ON market_orders(item_id, side, station_id, captured_at)` on branch `fix/kb-empire-authority-and-market-index`. Index creation on 190M rows takes minutes and blocks writers — it runs at next collector restart (CREATE INDEX IF NOT EXISTS in schema init), never against the live DB mid-operation. Don't COUNT(*) market_orders casually — that alone is ~2min.

**Why:** future "worker burning CPU" reports: pidstat first, then SIGQUIT dump; and any new pkg/market query needs an EXPLAIN QUERY PLAN check against the real index set. [[reference_trading_missions_not_market_validated]] [[reference_market_db_prune]]
