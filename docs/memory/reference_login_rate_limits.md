---
name: reference_login_rate_limits
description: Server rate-limits /login calls per-IP per-minute; few per-IP limits once logged in — shapes overmind fleet startup
metadata: 
  node_type: memory
  type: reference
  originSessionId: 696d1b09-e994-42b5-8a6c-d8be5aa101d8
---

The game server throttles new connections **per-IP**; too many logins from one IP in a short window get choked. Once a session is logged in, there are **few per-IP limits** on subsequent activity.

**Empirically measured 2026-06-23** (via `cmd/debug/login-probe`, builds to `bin/`, against live wss://game.spacemolt.com/ws):
- **Throttle onset ≈ 20–25 new connections per ~45–50s window (~25–30/min) from one IP.** 5s-paced logins (12/min) ran 18+ in a row with zero throttle (~2x headroom). 2s-paced (30/min) ran ~24 clean then started stalling. 10-concurrent tripped a hard choke after ~20 in ~16s.
- **The throttle hits the WS *connect/handshake* layer, NOT the login message.** It manifests as the WebSocket dial **hanging until the client's connect timeout**, NOT as the `code:"ip_timed_out"` error that `client.go` parseErrorState (~:3390) knows how to handle. So a throttled cold-start worker gets NO clean rate-limit signal — its connect just stalls and it **looks like a dead worker** to the supervisor (this is precisely the double-spawn trigger; the #1-fix `BootTimeout`=5min covers it).
- **Recovery is fast:** IP clears in well under a minute once you stop hammering. The hard choke from the concurrent burst cleared within ~30–60s.
- A single connect+login is ~0.8–1s of real work (conn ~400–600ms, login ~300–450ms). Note: `Client.Close()` blocks up to 5s draining goroutines (client.go:~4002) — a teardown artifact, unrelated to server latency.
- `StaggerInterval` default `SleepMedium`=5s ⇒ 12 logins/min ⇒ **VALIDATED safe** for fleet cold-start. Mass-restart burst (relaunches are immediate, not staggered) is the real risk: budget is ~25/min, so instantaneous mass restart WILL choke — stagger restarts too.

**Why it matters:** the [[project_overmind_fleet_manager]] overmind boots ~40 workers at once from one host/IP. A naive simultaneous launch hammers `/login`, gets throttled, and stretches each worker's cold-start out to minutes. This directly worsens the Plan A supervisor double-spawn race (a worker still completing a throttled login looks "dead" past `SilenceTimeout`, so a *second* process spawns — firing yet another throttled `/login`, a vicious cycle).

**How to apply:**
- Stagger worker spawns at startup (don't launch all ~40 at once) so `/login` stays under the per-IP/minute limit — deferred Plan B hardening.
- Keep the supervisor's `SilenceTimeout` comfortably larger than worst-case throttled fleet cold-start (Plan A interim raised it for this reason).
- The structural double-spawn fix (track live `*exec.Cmd`, never spawn while the old process lives, kill-before-respawn) matters precisely because duplicate processes amplify login throttling.

---

## ⭐🔴 2026-08-23 — THE POST-DEPLOY LOGIN BUDGET IS THE REAL CONSTRAINT, NOT FLEET SIZE

A second, harsher mechanism exists beyond the connect-layer stall above: an
explicit timed ban — `"Your IP has been temporarily blocked due to excessive
rate limit violations. Try again in N seconds"` — with durations seen today of
94s, 118s, and up to **960s**. The client logs it and adds its own jitter
(`IP rate limited for 94 seconds + 48s jitter`).

**The trap: a full fleet redeploy leaves the IP near its ceiling for a long time
afterwards.** A staggered 9-fleet redeploy (159 workers, `--stagger 10s`,
37 min) generated **721 block lines in one hour**. Ninety minutes later,
restarting a **7-worker** fleet — reasoned as "cheap, it's only 7 logins" —
tripped a fleet-wide block **6 seconds** after launch. Every one of the 9 fleets
logged its first block within a 5-second span.

The cascade is automatic: block -> workers lose connections -> overminds restart
them -> a fresh login cannot succeed during a block -> the block deepens. Within
10 minutes every worker in craft (9/9), hunt (5/5), unlock (17/17) and haul
(21/21) had restarted.

**Attribution caveat — separate the two failure modes before blaming yourself.**
A ~30-minute HOME INTERNET OUTAGE followed the same afternoon (16:30-17:29) and
produced far bigger restart numbers than the rate-limit event did (haul 201 ->
544). Bucketing the haul log by 10 minutes separated them cleanly: the
rate-limit window (15:40-15:59) had 40 `temporarily blocked` lines and ZERO
network errors; the outage window had 478 `no such host`/`dial tcp`/`EOF` lines
and ZERO blocks. **Grep both patterns by time bucket before concluding which
one you are looking at** — restart counts alone cannot tell them apart, and the
first read of the day wrongly charged the whole thing to the mining restart.

**A recovery gate that only checks for rate-limit blocks is not enough.** The
staged relaunch used `temporarily blocked` as its go/no-go and therefore
happily launched mb, mission-learn and mining INTO the dead network at
16:31/16:52/17:08. Gate on reachability too, not just on the limiter.

**How to apply:**
- **Size a restart by the IP's recent login history, never by the fleet's size.**
  After any full redeploy, treat the IP as spent and do **no** further logins for
  at least ~30-60 min — including "just one small fleet".
- A code fix that only lands in `bin/overmind` still costs a full fleet's logins
  to deploy. Batch such fixes into the next scheduled redeploy instead of rolling
  them immediately.
- When a block is already live, **do not restart anything** — freeze instead:
  [[reference_sigstop_preserves_game_sessions]] costs zero logins.
- Recovery that worked: freeze everything, let the block expire, resume workers
  then overminds, stop any fleet that enters a restart loop, then relaunch the
  downed fleets ONE at a time at `--stagger 20s` after a 15-minute quiet hold.
