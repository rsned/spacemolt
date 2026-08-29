---
name: project_overmind_stall_kill_connect_loop
description: "FUTURE TASK — overmind stall-watchdog kills workers DURING initial connect (before first heartbeat), so game slowness burns MaxRestarts=100 in ~8min → permanent abandonment. Fix the timeout discrepancy."
metadata: 
  node_type: memory
  type: project
  originSessionId: 049a9d00-8d00-48d9-98e0-76c43645cbd9
---

**Requested 2026-07-09 (user): explore & fix the underlying timeout discrepancies. NOT STARTED — investigate later.**

**UPDATE 2026-07-19 — the DISCONNECT variant is FIXED (`c71ffed`, origin/main).** Sibling failure mode, hit live: a server DEPLOY (~16:12 UTC / 12:15 EDT) dropped ALL game connections at once. Workers kept heartbeating over the control socket but froze (no game progress); 15min later the **stall watchdog (15min, undocked+no-progress) restarted them** → 21 haul + 41 mission fresh logins clustered → tripped the per-IP `/login` block → reconnects failed → re-culled every 15min = self-sustaining outage + a ~280 releases/min book-claim spin. The recovery mechanism (restart) sustained the outage; the reconnect gate would have healed it if left alone. **Fix:** `control.Status +Disconnected` (set from `client.IsConnected()`, `omitempty`→absent means connected so old binaries read as connected); supervisor `DisconnectGrace=30m` — a new watchdog case BETWEEN the silence and stall cases leaves a disconnected-but-heartbeating worker to the reconnect gate for 30min before falling through to a restart; `Haul`+`Missions` skip the pass when `!IsConnected()` (stops the claim→transport-error→release churn). Tests: `TestDisconnectedWorkerNotStallRestartedWithinGrace` / `...RestartedAfterGrace`, `TestBuildStatusCarriesDisconnected`, `TestHaulSkipsPassWhenDisconnected`. Files: `pkg/overmind/control/messages.go`, `pkg/overmind/supervisor/{fleet,supervisor}.go`, `cmd/worker/main.go`, `pkg/worker/{haul,mission}.go`. **STILL OPEN:** the ORIGINAL connect-phase variant below (kill during INITIAL connect before first heartbeat → MaxRestarts=100 burn → permanent abandonment). DisconnectGrace only covers established-then-disconnected workers, NOT never-connected ones.

**Incident that surfaced it:** 2026-07-08 ~19:07 PDT (02:07 UTC Jul 9), `hauler-0` + `salvager-3` (haul overmind) and `johnny_cab` (shuttle overmind) all stalled at the same minute (transient game-server hiccup). They stayed **dead ~15h** until a manual overmind restart on 2026-07-09 (see recovery below). mb/assist fleets were unaffected.

**Root-cause chain (verified from logs):**
1. Stall watchdog kills a worker after ~5s of no heartbeat/progress (`killed=true` in overmind log). Kill cadence is a fixed ~5s (≈ `game.SleepMedium` / `KillGrace`), no backoff.
2. A worker killed **during initial connect** — log shows `connecting to game server as X` → `Connecting to game server...` → `received signal terminated` 5s later, before it ever heartbeats. During the game hiccup, connect took >5s, so **every respawn was SIGTERM'd mid-connect and never became healthy**.
3. The crash-loop counter (`s.restarts[agent]`) only resets when a worker becomes **healthy** (`supervisor.go:314`); these never did, so it climbed to `MaxRestarts=100` (`supervisor.go:121`) in ~8 min (100 × 5s). At the cap (`supervisor.go:340` `if s.restarts[spec.AgentID] >= s.MaxRestarts`) the supervisor **stops relaunching**.
4. Terminal state is **abandoned, NOT quarantined** (quarantine is the separate fuel-strand path, `fleet.go:223 Stranded`). So no rescue fires, no auto-retry — permanent death until the overmind process restarts (which resets the in-memory `restarts` map). There is **no runtime "reset one worker's counter"** control (`ReleaseQuarantine` only clears quarantine, not the cap).

**The discrepancy to fix:** the stall-kill timeout is shorter than a slow connect, connect-phase kills count against `MaxRestarts`, the cadence is fixed (no backoff), and hitting the cap is permanent. So any transient game slowness reliably converts to permanent worker death.

**Fix directions (pick/combine):**
- **Connect grace period:** don't stall-kill a worker still in connect/login phase (before its first hello/heartbeat) — give initial connect a longer, separate timeout.
- **Don't count connect-phase kills against `MaxRestarts`** (only count kills after a worker has heartbeated at least once).
- **Backoff instead of fixed 5s** so a transient outage can't burn 100 restarts in 8 min.
- **Slow-retry after cap** instead of permanent abandonment (e.g. retry every few min), and/or surface an explicit "abandoned at restart cap" state in the status page (currently just shows unhealthy/stale, indistinguishable from a brief blip).

**Key files:** `pkg/overmind/supervisor/supervisor.go` (`MaxRestarts` :121, cap check :340, `KillGrace` :119, `reapAndRestart` :240, counter-reset-on-healthy :314), `pkg/overmind/supervisor/fleet.go` (`StallRestarts`, `Stranded` :223, `stallRestartLimit`), the stall-watchdog / SilenceTimeout config, `cmd/overmind/main.go` (signals).

**Recovery used 2026-07-09 (for reference):** only lever was a full overmind process restart (resets `restarts` map). Stopped haul (pid was 3883665) + shuttle (3113526) overminds via SIGTERM (cancels root ctx → kills worker children cleanly), removed stale `.sock` files, relaunched with identical args (`./bin/overmind --socket data/overmind/haul.sock --fleet data/overmind/haul-fleet.yaml --stagger 10s` and the shuttle one with `--status-file`/`--history-file`), detached via `setsid nohup … >> <log> 2>&1 </dev/null &`. All 3 recovered healthy (RESTARTS=0, heartbeating).

Related: [[reference_login_rate_limits]], [[reference_login_vs_reconnect_gating]], [[project_overmind_graceful_drain]], [[reference_overmind_launch_commands]], [[project_overmind_fleet_manager]].
