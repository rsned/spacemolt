---
name: reference_login_vs_reconnect_gating
description: "Fresh logins AND reconnects both go through the host-wide gate (since 2026-08-26 dialWithGate; rolling 60/min budget since 2026-10-23) — the old 'fresh logins are ungated' claim is obsolete; never mass-restart faster than the gate anyway"
metadata:
  node_type: memory
  type: reference
  originSessionId: ae0c813d-9d8e-4b7b-a04f-87f7158b2502
  modified: 2026-10-23T02:00:40.784Z
---

**Current (2026-10-23):** every dial on the host — fresh start (`InitializeAgent` → `dialWithGate`, 3 gated
tries), reconnect (`Client.Reconnect`, one gated try), MCP agents, `play_as` — claims one slot from the
file-locked gate (`pkg/game/reconnect_gate.go`, `/tmp/spacemolt-reconnect.gate`, override
`SPACEMOLT_RECONNECT_GATE`). The gate enforces a rolling 60 s window: 60 dials/min, 20 password logins/min,
≥1 s spacing, and a fleet-wide `blocked_until` learned from any `retry_after` (HTTP 429, close 4003,
`rate_limited`, `ip_timed_out`) or 5 min when none is stated. Details: [[reference_clerk_token_login_and_connection_gate]].

**History, so old notes make sense:**
- Before 2026-08-26 fresh logins were NOT gated — only `attemptReconnection` called `gate.Acquire()` — so a fast
  mass worker-restart was a burst of ungated logins (onset ~25/min) that tripped the per-IP block. That is what
  the 2026-06-28 incident (a bash loop ran 466 batches instead of 7) and the 2026-08-26 "16 miners restarted →
  429s across three fleets" were. `dialWithGate` fixed it.
- Until 2026-10-23 the gate was a fixed 5 s cooldown (~12/min) tuned to the shared `session_auth` password
  budget, which is why COLD_START demanded `--stagger 10s` and one fleet at a time (15–30 min cold starts).
  Clerk tokens moved auth to a per-player budget, so only the 100/min connection cap remains shared.

**Still true:** a supervisor relaunch is a login. Stop overminds BEFORE workers during a block
([[reference_sigstop_preserves_game_sessions]]); a disconnected worker is the gate's to reconnect, never the
supervisor's ([[reference_never_restart_a_disconnected_worker]]). Verify any restart script's loop bounds before
running it on the fleet.
