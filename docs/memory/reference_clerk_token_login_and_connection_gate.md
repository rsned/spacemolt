---
name: reference_clerk_token_login_and_connection_gate
description: "SHIPPED 2026-10-23 (merge 1112f7de): fleet logs in with single-use Clerk ws-tokens (one API key, player_id in credentials.json via clerk-sync) behind a host-wide rolling 60 dials/min gate; --stagger is dead; 4001 parks the worker; play_as writes a lease. Live rollout steps still pending."
metadata:
  node_type: memory
  type: reference
  originSessionId: ae0c813d-9d8e-4b7b-a04f-87f7158b2502
  modified: 2026-10-23T02:00:40.716Z
---

**What changed (branch `worktree-clerk-connection-pacer`, 16 commits, merged 2026-10-23):**

- **One secret.** The Clerk API key (`ak_…`, generated on spacemolt.com, regenerating replaces it) lives in
  `~/.config/spacemolt/clerk_api_key` (mode 600) or `SPACEMOLT_CLERK_API_KEY`. `pkg/game/clerk` mints a
  single-use ws-token per (re)connect (`POST /api/player/{id}/ws-token`, ~5 min TTL, ≥125 mints/min measured).
  **Key was pasted into a chat transcript on 2026-10-22 → regenerate it after the first fleet runs on the new
  binary, then restart every fleet (running processes hold the old key).**
- **`player_id` in `credentials.json`** — written by `go run ./cmd/tools/clerk-sync` (dry-run on 10-22: 215
  written, 6 unmatched = random-6/7/8/9 + empire-crimson(-real), 10 orphan owned players). Run it once after any
  new account is registered/claimed. Workers without it list players once and log `run clerk-sync`.
- **Server budgets that matter:** `connection` 100 WS upgrades/min per IP (HTTP 429 or close **4003**
  `rate_limited retry_after=N`); `session_auth` 30/min per IP shared by all password logins;
  `clerk_login_token` **10/min per player** (not per IP — this is the unlock). Blocks escalate 2→30 min.
- **The gate** (`pkg/game/reconnect_gate.go`, file `/tmp/spacemolt-reconnect.gate`, flock, host-wide): rolling
  60 s window, `ConnectDialBudget=60`, `ConnectPasswordBudget=20`, 1 s min spacing, `ConnectBareBlock=5m` for
  any rate-limit signal with no stated duration (ruled against the plan's 60 s — observed bare blocks ran
  444–1757 s). `SPACEMOLT_CONNECT_BUDGET` / overmind `--connect-budget N` override the dial budget — **use the
  same value on every overmind**. Every block logs `gate block source=<src> for=<d> until=<t>` (incident record).
- **One Acquire per dial, order Acquire → mint → connect → login.** `Connect`/`Reconnect` are single-try;
  `dialWithGate` does 3 gated tries at fresh start. Per-player `clerk_login_token` limits wait locally and
  re-mint; they never block the host.
- **Close codes:** 4001 `session_replaced` → worker **parks** (`displaced` in heartbeat; supervisor logs once,
  never restarts; one gated retry per `SleepDisplacedRetry`=10 min). 4002 → reconnect. 4003 → retry_after to gate.
- **play_as lease:** `play_as` writes `data/agents/<id>/play_as.lease` while running (removed on exit/SIGINT/
  SIGTERM); a parked worker never dials while it exists; `play_as` itself exits on 4001. **A SIGKILLed play_as
  leaves a stale lease → that worker stays parked until the file is deleted.** You no longer need to stop a
  worker before play_as.
- **Boot log:** `auth=clerk player_id=…` or `auth=password reason=no_key|not_owned|list_failed`.
- **`--stagger` defaults to 0** and COLD_START's "one fleet at a time / --stagger 10s" rule is gone.
- Probe: `bin/login-probe --mode clerk --account battle_bot1 --count 9 --interval 1s` (9, not 20: per-player 10/min).

**Live rollout still to do (docs/COLD_START.md has the sequence):** `clerk-sync` → probe (fleet stopped) →
**full stop of every fleet incl. mining/shuttle/fleet-watch, confirm no pre-merge binary** → rebuild bin/ →
relaunch fleet-by-fleet, watch first worker's `auth=clerk … Ready!` → regenerate key → restart fleets.
Expect ~90 live agents connected in 1–2 min (was 15–30).

**Known follow-ups (parked at merge):** tools/observer/`pkg/agent/manager.go` still dial outside the gate
(pre-existing); a `clerk_login_token` error with no retry_after re-mints immediately; fresh-start worker ignores
the lease; stale-lease detection by dead pid. Design: `docs/superpowers/specs/2026-10-22-clerk-connection-pacer-design.md`.
See [[reference_login_vs_reconnect_gating]], [[reference_rate_limit_buckets_and_escalation]], [[reference_login_rate_limits]].
