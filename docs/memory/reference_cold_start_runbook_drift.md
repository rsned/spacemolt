---
name: reference_cold_start_runbook_drift
description: docs/COLD_START.md fleet set re-synced 2026-11-03 to the 11 live overminds + fleet-watch; shuttle is NOT retired; login gate stays at 60/min by user choice
metadata:
  node_type: memory
  type: reference
  originSessionId: 9289c357-1e14-42dd-bb98-603f6ae8f559
  modified: 2026-11-03T10:51:08.556Z
---

`docs/COLD_START.md` is the cold-start runbook. It drifted badly (it listed 7 fleets and
called shuttle "retired" from 2026-08-14 until 2026-11-03). **Re-synced 2026-11-03** to the
fleet set that was running before the 11-02 CVE-patch reboot:

mb · assist · hunt · **shuttle** · **banker** · craft · unlock · mission-learn · **mining** ·
**crft-mining** · haul — plus `bin/fleet-watch` (new step 7, launched LAST because it freezes
its watch list at startup from the live overminds).

**Shuttle is NOT retired (user, 2026-11-03):** johnny_cab did the pirate-rep unlock in August
to reach more stations, then went back to hauling passengers under `shuttle-fleet.yaml`. Never
write it up as retired again.

**Login gate stays at 60 dials/min (user, 2026-11-03)** — deliberate headroom under the
server's 100/min; do not raise `--connect-budget`.

**How to re-check drift:** list `data/overmind/*-fleet.yaml` and compare each
`<fleet>-overmind.log` mtime to the last full stop; any live log not in the runbook is drift.
Retired: `idle`, `mission` (log last written 07-16).

Roster sizes drift constantly — compute as yaml count − `<fleet>-overrides.json .removed`.
Healthy `arbitrage_opportunities` is ~90, not the runbook's old 320–400; 30 = starved.

See [[reference_clerk_token_login_and_connection_gate]] · [[reference_fleet_watch]] ·
[[reference_pgrep_self_match_and_sole_member]] · [[reference_secondment_overrides_are_removed_sets]]
