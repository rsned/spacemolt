---
name: reference_login_vs_reconnect_gating
description: "Fresh logins are NOT rate-gated, only reconnects are — never mass-restart workers fast"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 7053c7c0-1791-4b1f-aee8-661ce3d67f1d
---

The fleet-wide **ReconnectGate** (`pkg/game/reconnect_gate.go`, file `/tmp/spacemolt-reconnect.gate`, override `SPACEMOLT_RECONNECT_GATE`) paces only **reconnect dials** — `attemptReconnection` calls `gate.Acquire()` (≥5s fleet-wide spacing via `reconnectGateCooldown`) and `gate.RecordBlock(d)` on a 429, so all clients wait out a recorded block. **Fresh process logins (`InitializeAgent`) are NOT gated.**

**Consequence:** a fast mass worker-restart = a burst of ungated fresh logins → trips the server-side per-IP rate-limit block (onset ~25/min). Once blocked, all active workers' commands fail + retry, which can re-hit and sustain the block.

**Always stagger fresh fleet logins.** Overmind `--stagger` paces INITIAL launches; `--restart-batch` (default 1/reap-tick) paces supervisor RESTARTS. Safe relaunch after an incident: full-stop both overminds → wait for the block to expire (no traffic) → **canary** launch (start one overmind at `--stagger 20s`, watch the first worker's `Ready! Credits` vs `blocked due to excessive`) → if clean, let it ride (~6 logins/min for two fleets at 20s). [[reference_login_rate_limits]]

**2026-06-28 incident:** a buggy rolling-restart bash loop (ran 466 batches instead of 7 — `total` from `mapfile` was wrong) mass-restarted the fleet, tripped the IP block ~09:40–09:47. The drain (SIGUSR1) quiesced but couldn't confirm clearance; recovery was full-stop → canary → `--stagger 20s` relaunch, 0 block hits. The `nudgeReconnect` idle-recovery fix ([[project_current_status]]) was NOT the cause (it fired 0×, and its reconnects are gated anyway).

**Lesson:** to redeploy a new worker binary to a live fleet, do it SLOWLY (small batches with waits, or stop+canary+staggered relaunch) — never a fast kill-all. Verify any restart-pacing script's loop bounds before running it on the fleet.
