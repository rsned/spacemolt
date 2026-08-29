---
name: project_battle_visualization
description: "Longer-term goal — visualize battles (top-down radar view), after-the-fact then live"
metadata: 
  node_type: memory
  type: project
  originSessionId: d4e7b749-674c-4002-acee-efa14f16affb
---

Longer-term goal raised 2026-05-25: **visualize battles** — first after-the-fact
(replay), eventually live: a top-down "radar" view of ships and actions. The
tactical zones outer→mid→inner→engaged map naturally to concentric rings;
overlay stances, per-tick actions, and damage.

**Why:** the battle system is confusing; seeing fights spatially helps the user
learn to plan and win more often.

**How to apply:** the data substrate already exists — the per-tick telemetry from
the sparring testbed ([[project_spar_testbed]]) `tick | name | zone | hull% |
shield% | stance` plus `battle_damage` events decoded in `pkg/game`. Cheap first
step when picked up: add a machine-parsable telemetry sink (JSON-lines of
per-tick participant snapshots, e.g. `--telemetry-json <path>` on `spar`) — the
harness telemetry loop is the single place that already has every snapshot. The
frontend/replay-format/live-SSE visualizer is a separate project (likely in
`frontend/` + `pkg/observe`). Not built yet; noted in the spar spec's
"Future direction" section.
