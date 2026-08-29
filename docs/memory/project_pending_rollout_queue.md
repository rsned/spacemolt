---
name: project_pending_rollout_queue
description: What is deployed vs still pending per fleet — haul is the one fleet left on the pre-2026-08-08 binary
metadata: 
  node_type: memory
  type: project
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-16T03:49:04.548Z
---

**2026-08-14 cold start after workstation crash: ALL SEVEN fleets now on HEAD
(`2150a282`) with `--assets-db-path data/assets.db`.** haul/mb/assist/hunt/craft/
unlock/mission-learn = 144 workers, restarts=0, deploy verified by worker start
time (13:21+) > bin/worker mtime (11:40). Nothing is pending. The fleet set is now
seven: shuttle RETIRED (johnny_cab lives in unlock), old idle fleet retired, hunt
(pirate-6..10) and unlock (25) added. haul now writes `haul-status.json` /
`haul-history.jsonl` (no longer the fleet-status defaults) and carries
`--secondment-ledger data/overmind/secondments.json`.

**2026-08-15 20:45 — HAUL FLEET ONLY rolled to `2fea237a`** (sell-leg dock fix,
[[reference_sell_leg_dock_gap]]). Drained in 8s, relaunched 20:45:43, staggered
up by 20:48:19: **16/21 healthy, restarts=0**, no panic / rate-limit /
session_replaced, no `not_docked` on the new binary. Deploy verified: oldest
worker 20:45:42 > `bin/worker` mtime 20:45:13. The other six fleets are still on
`eb02ac91` (the 08-15 05:00 fuel-capture roll) — they do not carry the dock fix,
but only the hauler role has the affected sell leg.

The five held out are all pre-existing rescue records restored at boot, NOT roll
casualties: craftsman-1 (Westmark/westmark_star, fuel 4/400), salvager-3
(Krynn/blood_arena, 3/270), trader-8 (Distant Light/mobile_capital),
explorer-1 (Xamidimura/kael_arsenal), trader-10 (Algol/dross_citadel).

(Historical: the 2026-08-08 rollout left haul on `f85a4ca` with no asset capture;
an 08-13 restart put it on `594ef241` WITH asset capture; the 08-14 cold start
closed the gap entirely.)

**How to apply:** verify a rollout by worker process start time vs `bin/worker`
mtime, never by worker count or `overmind_commit`
([[reference_deploy_verification]]). The drain poll is bounded ~5 min and REPORTS
rather than waits — mission-learn needed three `SIGUSR1` rounds to reach 38/38 idle,
and a `SIGTERM` after the first would have killed five workers mid-task. Re-sending
`SIGUSR1` re-opens the window harmlessly.

Related: [[project_agent_capability_ledger_storage_faction]] ·
[[reference_overmind_launch_commands]]

## Open threads as of 2026-08-17

Nothing is mid-deploy. All seven fleets are on `4b84ac1b` (fuel gate) as of
10:05 2026-08-17; 160/160 workers healthy; rescue queue EMPTY.

Carried forward, none of them blocking:

1. **NearestFuel is not wired into haul.** The fuel gate now stops an agent
   safely instead of stranding it, but it does not route it TO fuel — so a
   worker at a dry or unaffordable desk logs "NOT DEPARTING" each pass and
   idles. 161 such refusals appeared in the first minutes after the roll (136
   haul, 15 unlock, 10 mission-learn), which suggests a meaningful number of
   haulers now idle rather than strand. `Collector.NearestFuel` already ranks
   reachable wet desks and is wired into assist only; extending it to haul needs
   `NearestFuel` added to the `OpportunityStore` interface plus the test fakes.
   [[project_haul_departs_without_enough_fuel]]
2. **Battle holotable P1** is the next build step —
   [[project_battle_holotable_visualizer]].
3. **A server bug report is drafted but NOT filed** (float precision in
   get_battle_log); the operator was going to file it.
4. `docs/COLD_START.md` was refreshed for the seven-fleet layout on 08-14 but
   has not been re-checked against the flags added since (`--assets-db-path` on
   every fleet, haul's `--secondment-ledger`).
