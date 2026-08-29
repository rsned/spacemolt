---
name: project_server_restart_warning_event
description: server_restart_warning is now handled and BuiltForAPIVersion is v0.495.1 (both DONE 2026-07-13); pre-0.473.0 drift remains unaudited
metadata: 
  node_type: memory
  type: project
  originSessionId: 82fc608b-c0b2-4c87-ad0b-296b44e4a4ff
---

**Both follow-ups from the 2026-07-09 restart are DONE (2026-07-13), uncommitted on `main`.**

1. **`server_restart_warning` — HANDLED.** `protocol.TypeServerRestartWarning` added to `internal/protocol/messages.go`; case in `handleResponse()` (`pkg/game/client.go`) plus an entry in `pushOnlyResponseTypes` so the push can't clobber play_as's `_last` slot. Payload (still undocumented — no schema in openapi.json or ws.md, so every field is read defensively):
   ```json
   {"message":"Server restarting in 60s for an update to v0.485.0. Expect a brief ~10-20s disconnect.","seconds_until_restart":60,"target_version":"0.485.0"}
   ```
   It only **logs**. The graceful **pre-restart drain** is NOT built — this case is the hook to hang it off.

2. **`BuiltForAPIVersion` — BUMPED to `v0.495.1`.** Per [[feedback_version_constant]] a bump asserts nothing on its own, so this one was driven by an actual path+field diff of the openapi snapshots rather than a snapshot match. See [[project_api_sync_v0495]] for what that sweep found and fixed.

**Why:** the unhandled-type log line was the server announcing a real protocol addition, and it fired on every restart until handled.

**How to apply:** drift *within* the diffed window (v0.472.4 → v0.495.1) is now closed. The **pre-v0.473.0 drift is still unaudited** — that older delta was never swept, and the version constant does not imply otherwise. See [[project_api_struct_drift_audit]] for the method if it ever needs doing.
