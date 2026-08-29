---
name: reference_fleet_status_fossil
description: A status file no overmind writes still parses, so tools read a 17-day-old snapshot as live — this is where "legacy binary workers" came from
metadata:
  type: reference
---

`data/overmind/fleet-status.json` was last written **2026-08-12 19:59** and no
`fleet.sock` overmind has run since. A workstation reboot does NOT clear it: it
is a file on disk, not process state.

**Symptoms it produced, all misleading:**

- **"legacy binary workers"** — 19 entries frozen at the version they reported
  on 08-12, all with the identical `last_seen 02:59:23`. Nothing was actually
  running an old binary. **A fleet whose 19 workers share one heartbeat second
  is a fossil, not a fleet.**
- `salvager-3`/`salvager-9` appeared never-started while a live salvager-9 ran
  in haul. Inflated the off-map count.
- Empty `version` renders as "legacy" on the dashboard — those are quarantined
  agents that never started, so never reported one.

**FIXED `3743e471`:** file deleted; `fleet.yaml` **emptied but kept**, because it
is the DEFAULT `--fleet` value and a bare `bin/overmind` would re-launch all 27
entries alongside the fleets already running them (two logins as one agent →
the server kicks one). `fleet-history.jsonl` KEPT — history, not a snapshot.
`fleet-report` / `haul-distances` / `haul-dashboard` defaulted `--status-file`
to the deleted file and now default to `haul-status.json`.

Five of its entries were never real: `hauler-1..3`, `resident-nebula-1..2` — no
`data/agents/` dir, no assets row, no game account.

**Generalise:** the dashboard has no staleness gate. Any `*-status.json` whose
newest heartbeat is old still renders as current — `mining-status.json` is in
the same state now (fleet deliberately stopped).
Part of [[reference_capture_loss_taxonomy]] mechanism 6.
