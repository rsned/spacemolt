---
name: reference_refuel_fallback_vs_measurement
description: Three call sites feed the refuel error to Access.RecordRefuel as a fuel-desk measurement — a cargo-cell fallback there reports a dry station as one that sells fuel
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T12:06:35.229Z
---

`RefuelAndSync` has a cargo-cell fallback (dry desk → burn a `fuel_cell`). That
is correct for callers that just need fuel — assist, missions, haul — and WRONG
for callers whose refuel error is a **measurement**:

- `pkg/worker/station_probe.go` — per-stop AND preflight, both `Access.RecordRefuel`
- `pkg/worker/shuttle.go` — drop-off, `Access.RecordRefuel`

These learn whether a station sells fuel at all and write it to
`station-access.json`. A fallback that succeeds makes the refuel return nil, so a
dry desk is recorded as a working one — and the probe also spends a scarce cell at
every dry station it surveys.

Use `RefuelStationAndSync` (station desk only, no fallback) wherever the result is
recorded. The shuttle records first, THEN falls back separately, because unlike
the probe it still has somewhere to be.

Caught 2026-08-13: commit `0a94b367` swapped in the fallback blind and broke
`TestProbeLearnsFuelDeskSeparatelyFromAccess` — which already encoded the rule.
`go build` passed; only `go test ./pkg/worker/` caught it. Repaired in `1df1b9d1`.

Related: [[reference_pinned_mission_workers_never_refuel]] · [[feedback_gameclient_interface_mocks]]
