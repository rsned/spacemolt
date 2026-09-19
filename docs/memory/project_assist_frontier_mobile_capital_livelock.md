---
name: project-assist-frontier-mobile-capital-livelock
description: assist-frontier sent 1.26M no-op travel commands over 76 days at 11.5/min — pin-arrival string equality fails against the dual-named mobile capital
metadata:
  type: project
---

**assist-frontier has been livelocked since 2026-07-04 18:39.** Discovered
2026-09-18. It issues `travel mobile_capital` every ~6s, the server replies
"You are already at the target system", and it repeats.

```
1,258,126 no-op travels   2026-07-04 -> 2026-09-18   11.5/min sustained
assist-overmind.log = 1.5 GB
```

Other assists show the same shape only in flickers: haven 18,317 (0.2/min),
nexus 343, krynn 130, sol 129. One agent, not a fleet behaviour.

**A RESTART DOES NOT FIX IT** — verified 2026-09-18: killed pid 2217626, the
new worker resumed at 6.5/min within 65 seconds. The livelock is structural,
not accumulated state.

## Root cause

`assistEnsureHome` (pkg/worker/assist.go ~line 428) guards the re-travel with
a single string equality:

```go
if st == nil || st.CurrentPOI != deps.HomeStation {
    deps.navigate(ctx, home, deps.HomeStation)
}
```

`deps.HomeStation` is the `--station` flag = `mobile_capital` (a POI id).
`st.CurrentPOI` never equals that literal, so the guard always fires. The
author explicitly anticipated the thrash — the comment above it warns that
re-travelling to an occupied POI auto-undocks and loops forever, "Critical
for mobile_capital" — but one string compare cannot recognise arrival at a
dual-named station. See [[reference_station_id_aliases]] and
[[reference_pin_arrival_check_four_directions]]: arrival needs FOUR checks.

All five assists are pinned by POI id with a differing base id
(grand_exchange/grand_exchange_station, sol_central/confederacy_central_command,
war_citadel/crimson_war_citadel, the_core/central_nexus,
mobile_capital/frontier_station), so the mismatch is universal. The
discriminator is that mobile_capital MOVES, and the KB's copy is stale — KB
had it in `void_gate` while the agent was live in `first_step`.

## Why it matters beyond one agent

11.5 req/min sustained for 76 days against a **shared per-IP counter**. This
ran through every IP block we investigated, including the 09-09 freeze where
heartbeats and craft-spam were ruled out and we concluded "the fleet is just
bigger than the per-IP budget". See
[[reference_ip_block_20260909_freeze_and_staged_thaw]] and
[[project_execute_loop_has_no_pacing_floor]].

Health checks report it `100.0%` / connected / `yes` throughout — the
livelock-invisible-to-health-checks pattern
([[reference_livelock_invisible_to_health_checks]]) at the largest scale yet
seen.

## Consequence: hauler-0 cannot be rescued

hauler-0 sits at 0/120 fuel at `icecap_drift` in **Frontier, which has NO
dockable base** (zero rows in `bases`), so its 23.2M credits cannot buy fuel.
Only a tanker can reach it, and assist-frontier IS that tanker — stuck at
2/1500 fuel. It was gifted 50,000 cr on 2026-09-18 (credits confirmed after
restart; the pre-restart `credits 0` was the known stale heartbeat, see
[[reference_worker_heartbeat_credits_stale]]) and still cannot refuel,
because the loop never lets its scheduler reach the refuel step.

## Fix options (NONE APPLIED)

1. **Mitigation now:** stop or unpin assist-frontier. It performs no useful
   work, so stopping loses nothing and returns ~11.5 req/min to the budget.
   Re-pinning to a FIXED Outer Rim station (e.g. first_step_memorial_station,
   which has refuel and is where it already sits) sidesteps the mobile
   capital entirely.
2. **Real fix:** make the arrival check accept POI id OR base id OR name, per
   [[reference_pin_arrival_check_four_directions]], and refresh the mobile
   capital's location from live state rather than the stale KB row.
