---
name: reference_line_dashboard
description: "Line dashboard (plan 3, shipped 2026-10-25): SCADA-style web view of the contraband production lines on :8093 — how to run it, read its colours, force a refresh, add a line, and the staleness rule that makes it honest."
metadata:
  node_type: memory
  type: reference
  originSessionId: 0188c4c3-7b1f-459b-bf39-6c78fe769ae2
  modified: 2026-10-25T17:08:53.294Z
---

**What:** `cmd/tools/line-dashboard` + `pkg/linedash` (merged 10-25, main 06cd562b). Read-only: reads fleet yamls/overrides/secondments, `*-status.json`, assets.db (storage snapshots, action-log events, cursors), market.db (orders, `source<>'worker'` for demand), `data/agents/<id>/shopkeep.json`. Never calls the game, never writes.

**Run (COLD_START.md has the block):** `go build -o bin/line-dashboard ./cmd/tools/line-dashboard && setsid nohup ./bin/line-dashboard --addr ":8093" >> data/overmind/line-dashboard.log 2>&1 < /dev/null &`. Flags: `--lines data/overmind/lines.yaml --overmind-dir data/overmind --agents-dir data/agents --assets-db data/assets.db --market-db data/market.db --refresh 60`.

**Pages:** `/` board (one row per line: state, /day produced, /day sold, alerts, "data stale" badge); `/line/<id>` the node graph (legend, Pause, **Refresh** = `?refresh=1` forces a rebuild; otherwise the cache rebuilds at most every refresh/2 s); `/api/lines.json`, `/api/line/<id>.json` (same data; use it for metrics); `/healthz`.

**Colours:** green optimal · yellow starved (input below one batch / nothing to move / no stock) · red choked (tank ≥ 1,000 with no outflow in 4 h, inputs present but no craft, ask stale at the floor) · grey unknown = **data stale**. Alert texts: output stalled waiting on inputs, market saturated, hauler starved, source depleted, agent broke/stranded, no bots on node, output not drawn, data stale ("facility busy" is reserved; no capture signal exists).

**Staleness rule (the thing to remember):** every node/edge rule records the sources it read — the action-log cursors of the agents whose logs carry its events (a Mine node uses the HOLDER agents' cursors, e.g. trader-9, because miners' logs are never walked), each tank's `captured_at`, the ask's `SeenAt`. The OLDEST decides: any source older than `stale_data` (2 h) → grey, alerts = [data stale], the old verdict moves to `last known: …` and `stale: <source> <age>` detail lines. Heartbeats (`Live.LastSeen`) never make a node fresh. "No X within W" windows are anchored at the oldest cursor, not at Now. So: **if a node is grey, fix the capture cadence, not the rule** — line agents need hourly `capture_action_log` (and crafters hourly `capture_faction`), which plan 3 Task 8 set on the six moonshine agents.

**Add a line:** a block in `data/overmind/lines.yaml` (nodes: mine/purchase/store/convert/produce/sell with `holders` = where the tank sits, agents, station, recipe/units_per_run/inputs_per_run/batch_runs; edges: haul/consign/craft with `agents` = the senders whose `gift_sent` is the link's movement). Unknown keys and unknown edge kinds are load errors (typos cannot silently default). Thresholds live in the same file. `energy_crystal`/starshine are rejected at load.

**Why:** [[project_contraband_pipeline_resume]] · design D7 in docs/superpowers/specs/2026-10-24-contraband-production-pipeline-design.md · plan docs/superpowers/plans/2026-10-24-contraband-pipeline-dashboard.md.
