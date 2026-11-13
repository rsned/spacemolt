---
name: feedback-mining-loop-hold-full-first
description: "Mining loops must start with \"if hold full → go unload everything, else mine\"; a snapshot-diff hand-off alone livelocks on a hold that starts full"
metadata:
  node_type: memory
  type: feedback
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-11T23:35:12.365Z
---

Every mining loop (pipeline miners, drones, mine_qty) starts with `if hold_full { travel to station and unload } else { mine }` — and the unload must take the WHOLE hold (minus reserved items like fuel_cell/energy_crystal), not only what this trip mined.

**Why:** 2026-11-11 miningbot_0023 sat at Timberline 180/180 with byproduct gas for days. MineQtyAt's hand-off shipped only the target plus lines that grew since a start-of-pass snapshot; a hold already full at pass start (after a restart or failed hand-off) has nothing that can grow, so it delivered nothing every 30s and got quarantined. The user pointed out this is exactly why drone loops check hold-full first. Fixed in commit "mining: hand off a hold that is already full when a pass starts".

**How to apply:** when writing or reviewing any mine/harvest loop, check the full-hold-at-start path explicitly and test it. Related: [[reference-pipeline-worker-verbs-and-flags]], depleted-belt migration (pkg/worker/mine_migrate.go).
