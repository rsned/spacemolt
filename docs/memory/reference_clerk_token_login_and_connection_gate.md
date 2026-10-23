---
name: reference_clerk_token_login_and_connection_gate
description: "SHIPPED 2026-10-23 (merge 1112f7de): fleet logs in with single-use Clerk ws-tokens (one API key, player_id in credentials.json via clerk-sync) behind a host-wide rolling 60 dials/min gate; --stagger is dead; play_as writes a lease. Key regenerated 10-23. KNOWN GAP: 4001 does NOT park a running worker — it reconnects in 2 s and displaces play_as."
metadata:
  node_type: memory
  type: reference
  originSessionId: ae0c813d-9d8e-4b7b-a04f-87f7158b2502
  modified: 2026-10-23T07:06:47.770Z
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

**ROLLED OUT 2026-10-22 19:48–19:55 PDT — RESULT: ~150 workers across 10 fleets logged in with ZERO 429s,
ZERO `gate block` lines, every worker `auth=clerk`.** Sequence that worked: `clerk-sync` (215 written) →
`login-probe --mode clerk --count 9 --interval 1s` against the live IP (9/9 ok) → full stop → rebuild all 8 bins →
mb (54) first, 15 Ready in seconds → the other 8 overminds launched back-to-back 1 s apart → 149 workers running
~2 min after the first launch; haul after the next marketbot capture. Workers come up ~1 s apart (gate min
spacing) and `Ready!` ≈1 s after connect. A `play_as craftsman-1` session on the OLD binary ran throughout as
the human-side 429 canary: nothing. **Cold start is now ~5 min for the whole fleet, down from 15–30.**
Still to do: regenerate the Clerk key on the website (it is in a transcript), update
`~/.config/spacemolt/clerk_api_key`, restart every fleet; restart the two dashboards + market-prune, which are
still on pre-merge binaries (read-only, harmless).

**Server-operator confirmation (SpaceMolt Admin, 2026-10-22):** the model above is the intended one — mint per
agent via `POST /api/player/{id}/ws-token` (5 min), `login_token` on the socket, honor HTTP 429 `Retry-After` /
`rate_limited retry_after` / close 4003, one persistent connection per agent, staggered ramp-up. Two endpoints
we did not know: **`GET /api/me`** checks the key (200 good, 401 bad, **503 + Retry-After = wait, do NOT rotate**)
and **`GET /api/players`** lists owned characters (equivalent to our `/api/registration-code`). **Fixed 10-23:**
a mint 503 now records the wait on the gate (`mint503`, Retry-After else one window) and re-mints — no password
fallback; `clerk-sync` and `login-probe --mode clerk` call `/api/me` first and print the operator's guidance
(`clerk.KeyCheckMessage`); a 401 on mint logs "clerk key rejected". bin/worker + bin/overmind rebuilt with this —
**the running fleet picks it up at the key-regeneration restart.**

**Known follow-ups (parked at merge):** tools/observer/`pkg/agent/manager.go` still dial outside the gate
(pre-existing); a `clerk_login_token` error with no retry_after re-mints immediately; fresh-start worker ignores
the lease; stale-lease detection by dead pid. Design: `docs/superpowers/specs/2026-10-22-clerk-connection-pacer-design.md`.
See [[reference_login_vs_reconnect_gating]], [[reference_rate_limit_buckets_and_escalation]], [[reference_login_rate_limits]].

**2026-10-23 follow-ups.** Clerk key REGENERATED (operator, 10-23) — the key-in-transcript item is closed; `/api/me` → 200.
19 h of production: 0×429, 0×`rate_limited`, 0×`session_replaced` from our own dials; five server deploy-503 windows
produced 74 `gate block` lines (15 s `retry_after`, one bare 5 m) and every worker reconnected cleanly.
**⭐🔴 The 4001 park does NOT work in the live worker.** Three times running, `play_as the_banker` logged in, the
banker worker got `4001 session_replaced` and logged `✓ Reconnected successfully` **2 s later** (no
"session_replaced: parking" WARN at all), displacing play_as mid-script. The park code is in `pkg/game/client.go:920`
but the worker's reconnect path evidently bypasses it. Until fixed, to drive an agent whose worker is running:
**SIGSTOP its overmind FIRST, then the worker**, run play_as, `rm` the lease (play_as leaves it behind when it is
displaced), then SIGCONT both. A worker frozen without its overmind frozen is killed and respawned by the stall
watchdog within ~90 s, and the respawn displaces you again. [[reference_sigstop_preserves_game_sessions]]
