---
name: reference-repair-kits-from-cargo
description: repair from cargo works undocked and uses one repair_kit per call (user-confirmed 11-18)
metadata:
  node_type: memory
  type: reference
  originSessionId: 8c15f693-8825-4572-8f22-51141db1e328
  modified: 2026-11-18T17:41:45.258Z
---

`repair` away from a station consumes repair kits from cargo: it works undocked and uses ONE kit per call (confirmed by the user 2026-11-18, answering an open question in the frontier full-explore review). `pkg/explore/hull.go` assumes one kit = 50 hull (`KitHull`) — the 50 figure is the spec's value, not something the user confirmed here.

**How to apply:** loop `repair` once per kit and re-read hull between calls; do not expect one call to drain several kits. Related: [[reference-environmental-hazards]].
