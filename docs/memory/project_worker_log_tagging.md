---
name: project_worker_log_tagging
description: "Worker decision-stream logging now tagged [worker:<id>] (v0.2.3 DEPLOYED mission-learn) + per-worker carrier-tier logging (v0.2.4 COMMITTED, not deployed). How freight/haul/mission reasoning lines became attributable, and how to capture all carrier tiers on next relaunch."
metadata:
  node_type: memory
  type: project
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-25T08:43:11.960Z
---

**Problem:** `cmd/worker/main.go` built a prefixed `log.Logger` (`[worker:<id>] `) for lifecycle lines, but wired `MissionDeps.Out` (the decision stream — freight/haul/mission/autopilot/explore reasoning via `fmt.Fprintf`) to **raw `os.Stdout`**. So decision lines landed UNPREFIXED in the aggregated overmind log — impossible to attribute to an agent. This blind spot cost hours diagnosing engineer-4 (couldn't tell which `freight: no candidate (reason)` was whose).

**v0.2.3 fix (`2da86af`, tag v0.2.3, DEPLOYED to mission-learn 2026-07-25):** added `logLineWriter` (line-buffering `io.Writer` that emits each complete line via the prefixed logger) in `cmd/worker/main.go`; swapped the 3 `os.Stdout` decision-stream sites (`NewWorkerDispatch` Out, `StandingDeps.Out`, `PayRescueDebt`) to it. Now every reasoning line is `[worker:<id>] <ts> …`. No behavioral change. Deployed via graceful drain (SIGUSR1 → 37/40 idle, force-TERM) → `rm -f` sock → relaunch with `--stagger 10s` (40 workers, all v0.2.3). Also in v0.2.3: `5f5b1ee` serverapi `CraftQueueListing` decodes `kind`/`total_jobs` (kill `[SERVER API CHANGE]` spam).

**v0.2.4 (`1193e52`, tag v0.2.4, COMMITTED not deployed):** `missionRunState.logTierIfChanged` (freight.go) called after the shipping-profile decode in `freightCandidate`; writes `freight: carrier tier <t> (<done>/<req> deliveries, <val>/<req> value to <next>)` on first-seen + every promotion (on-change only, nil/empty-safe, `lastLoggedTier` field on missionRunState). Rides the NEXT mission-learn relaunch → grep `carrier tier` gives ALL freight-carrier tiers authoritatively, no per-agent query. Also logs promotions live.

**Authoritative one-off tier query:** `shipping --action=profile` in play_as (generic passthrough → game msg `Type=shipping, Payload={action:profile}`). Returns `progression.current_tier`, `successful_deliveries`/`required_*`, `next_tier`, `debt_blocks_acceptance`. I was WRONG that play_as lacked a shipping command — it sends any `<cmd> --flag=val` generically.

**Build/deploy mechanics:** `scripts/build.sh` stamps `git describe --tags` into buildinfo.version + `codeDirty` = any uncommitted tracked file OUTSIDE data/. Commit all code + tag before build for a clean (green) stamp. An unrelated `server_docs/openapi.json` symlink retarget (untracked 2026-07-23 dated file, incomplete docs sync) makes codeDirty=true — `git stash push` it around the build, don't commit/revert someone else's partial sync.

**STILL OPEN:** HAUL fleet (21 haulers) + overmind still on old binary — user wants the same graceful overmind+worker binary update to v0.2.4. Do separately (never ramp two fleets at once — login rate limit). Haul workers don't run freight so no tier lines there; the tier sweep is a mission-learn relaunch. [[reference_overmind_launch_commands]] [[project_overmind_graceful_drain]] [[project_freight_probationary_cargo_fence]]
