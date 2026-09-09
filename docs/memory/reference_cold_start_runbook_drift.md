---
name: reference_cold_start_runbook_drift
description: docs/COLD_START.md is the proven cold-start runbook but documents 7 fleets — the live set is 9 (mining + shuttle) plus fleet-watch; corrections verified 2026-09-08
metadata:
  type: reference
---

`docs/COLD_START.md` is the right runbook to follow after a host crash or a full
stop — the ordering (dashboards → scanner → fleets one stagger window at a time
→ haul last, gated) held up cleanly on **2026-09-08**, 170 workers, 0 restarts.

**But it is stale on the fleet set, and the doc is not self-correcting.** It
documents seven fleets and states shuttle is retired. The actual live set:

| runbook says | reality (09-08) |
|---|---|
| haul, mb, assist, hunt, craft, unlock, mission-learn | + **mining** (since 08-21) + **shuttle** (johnny_cab, never retired) |
| — | + **`bin/fleet-watch`** monitor (`fleet-watch-status.json`) |

Launch flags for the two undocumented fleets follow the same formula as the
rest (`--socket`, `--fleet`, `--worker-bin`, `--status-file`, `--history-file`,
`--assets-db-path data/assets.db`, `--stagger 10s`); the status/history file
names already exist on disk (`mining-*`, `shuttle-*`).

**Other numbers that have drifted:**

- Roster sizes: mb **64** (not 54), unlock 23, mission-learn 23, mining 23,
  haul 15, craft 9, assist 5, hunt 5, shuttle 1 — compute them as
  `roster yaml count − len(<fleet>-overrides.json .removed)`.
- The runbook's "healthy `arbitrage_opportunities` ≈ 320–400" is an
  **2026-08-14 figure and no longer holds**. On 09-08 the pool sat steady at
  **87–111** across four scans with **61** stations reporting — i.e. a fully
  healthy market. Do not read ~90 as starved; the runbook's 30 = starved line is
  still the one that matters.

**What the runbook gets right and must not be shortcut:** one fleet's
`--stagger 10s` window at a time (per-IP `/login` ≈ 10 logins/min), haul held
until a marketbot capture cycle has landed *and* the scanner has scanned against
it, and `--assets-db-path` on every fleet (omitting it fails **silently** —
captures still log, a nil store makes them no-ops).

See [[reference_login_rate_limits]] · [[reference_rescue_queue_blocks_launch]] ·
[[reference_capture_cadence_retune]] · [[reference_fleet_status_fossil]]
