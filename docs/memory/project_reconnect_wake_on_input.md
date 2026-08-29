---
name: project_reconnect_wake_on_input
description: "ReconnectingHandler reconnection model — bounded burst then dormant, woken by REPL input"
metadata: 
  node_type: memory
  type: project
  originSessionId: d888eb9c-31e9-4e93-8e93-be4e146853a7
---

`ReconnectingHandler` (`pkg/game/client.go`) reconnection model, shipped 2026-06-03 (commit d2ae74c):

- A disconnect triggers a **bounded backoff burst** of `reconnectMaxAttempts` (3) tries: immediate first attempt, then context-aware exponential backoff capped at `SleepReconnect`. After the burst it logs "will retry on next command" and **goes dormant** — it does NOT retry forever in the background.
- `TriggerReconnect()` (guarded by the existing `reconnecting` CAS) starts a fresh burst; `Client.RequestReconnect()` delegates to it.
- play_as wakes it on input: `executeLogicalCommand` calls `RequestReconnect()` when disconnected, then `WaitForReady(ctx, SleepMedium)` so a now-restored connection lets the command run.

**Why this shape:** the old code gave up permanently after 5 attempts, so an internet blip left a long-running agent wedged forever while the REPL still falsely printed "reconnecting." Infinite background retry was the obvious fix but the user preferred bounded-then-wake-on-input (don't hammer the network during a long outage; recover the moment the player interacts).

**Testability seam:** the handler has `reconnectFn` (defaults to `client.Reconnect`) and `reconnectBackoffUnit` (defaults to 1s) so the retry loop is unit-tested without real network I/O — see `client_session_contention_test.go`.

**Re-auth on reconnect:** `Client.Reconnect` always Connect → WaitForReady → `Login` → drainPendingReplay (client.go ~803), so every reconnect re-authenticates; the client never relied on session persistence. Server-side (changelog ~2026-06-04): "HTTP API session now survives server restarts — no more sudden auth errors after a deploy." Our WS client re-logs-in regardless, so this just makes the post-deploy reconnect's Login more reliable; no client change needed.
