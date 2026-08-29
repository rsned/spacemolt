---
name: project_faction_events
description: "Faction event handling status — faction_promote done, faction_demote pending server-side"
metadata: 
  node_type: memory
  type: project
  originSessionId: 08a5ff18-24e1-4fed-b7eb-e7a5bf60f099
---

Handled as of 2026-05-22 (constant in `internal/protocol/messages.go`, `handleResponse` case, push-only registration, `eventExpectedFields` entry in `client_api_monitor.go`):
- `faction_promote` — updates `Player.FactionRank` to new_role. Payload: faction_name/new_role/old_role/promoted_by.
- `faction_invite` — log-only (an offer, not membership; no state mutation until accepted). Payload: faction_id/faction_name/invited_by.

`faction_demote` does **not** exist server-side yet — user filed a bug for it. When it ships, add it reusing the `faction_promote` handler shape (also updates FactionRank).

Likely future siblings still to expect: faction_kicked/faction_removed, faction_disbanded.
