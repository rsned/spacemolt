---
name: project_haul_idle_trap_gate
description: "Haul fleet self-traps: 21 workers converge on one region, the 5-jump opportunity filter finds nothing, and nothing ever repositions them. Operator's fix = idle-trap gate that sends a worker home or beyond the jump threshold after N barren cycles"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-27T10:09:45.220Z
---

**Observed live 2026-07-27T03:35Z — the whole haul fleet idling while fat opportunities sat unclaimed.** Not a scanner outage (that is the usual suspect, [[project_scanner_outage_expiry_fix]]): the scanner was healthy, inserting 15–18 per scan, and the pool held **15 available, unexpired, unclaimed** opportunities worth up to **102,840** gross.

**The trap** (operator: *"they got 'trapped' by all ending up in the same area at the same time so not enough opportunities within a couple jumps"* / *"no one will travel far to pick up an opportunity"*): the fleet converged on one region while the profitable pool sat in another, and the worker's hard **5-jump** opportunity filter means it never bridges the gap. The log line is `haul: no opportunities within 5 jumps; idling`.

Distances measured at the time (BFS over `connections`, 21 workers) — only **4 of 21** were inside the gate:

| where | workers | jumps → treasure_cache |
|---|---|---|
| Treasure Cache | 2 | 0 |
| Factory Belt | 1 | 1 |
| Cargo Lanes | 1 | 4 |
| The Experiment | 7 | **21** |
| Node Beta | 6 | **24** |
| Procyon / Node Alpha / Synchrony / The Anvil | 4 | **18–26** |

The available pool was also concentrated: **10 of 15 sourced from `treasure_cache_trading_post`** (9 of the top 10 were `circuit_board` out of that one station), the rest from nova_terra_central / starfall_salvage_station / void_gate_outpost. So one region held nearly all the value.

**This is a ONE-WAY trap.** Once a worker drifts out of a profitable region it can never earn its way back, because every candidate that would pay for the trip is outside the radius that lets it see candidates. It idles indefinitely — no error, no alert, just `idling`.

## ⭐ PARTIALLY SHIPPED 2026-07-27: `52b2cfa` — constraints 4+5 only

`HaulAnyDistanceNet = 100_000` in `pkg/worker/haul.go`. `RankHaulOpportunities` no longer
drops a too-far opportunity outright: the `maxJumps` filter **releases for any single
opportunity whose net-of-fuel clears 100k** — the fuel_cell / power_cell /
trade_authenticator tier the operator named. This is constraint 4 (release past a hard
threshold) keyed on constraint 5 (value, not idleness). Approved thresholds: 100k bar, and
**no anti-herd throttle** — claiming already de-conflicts, and the 9 released runs had 9
different sell destinations, so buy-side depth at the single source station is the real
ceiling, which is [[reference_haul_fleet_capacity_ceiling]] territory anyway.

Design details worth not re-deriving:
- The release is judged on **net, not gross**, and net now shares one definition with
  ranking (`netOfFuel`, extracted out of `effNet`; `effNet` = `netOfFuel` × stability boost).
- **The stability streak is deliberately excluded from the release.** A 6-cycle route gets
  +50% for ranking; letting that unlock a cross-galaxy trip would mean a streak, not a
  payday, moved the ship.
- `HaulMaxHaulJumps` (40) still applies to a released opportunity — the release widens the
  approach leg only.
- The distance test runs **after** the haul-leg BFS so the whole trip can be priced, with a
  gross-based pre-filter in front (fuel only subtracts, so gross bounds net from above);
  that keeps the common far-and-small case from paying for a BFS.
- `TestRankDistanceCapDropsFarOpps` had asserted a 999999-gross opp is dropped at 4 jumps —
  now exactly the case that should release. Its gross moved below the bar.

**Still NOT built (constraints 1-3):** per-worker consecutive-barren-cycle counting,
commitment/dwell hysteresis, and the deliberate "reposition to home_base or to the best
opportunity anywhere" move. The shipped release only helps when a fat-tier opportunity
EXISTS; a fleet idling in a genuinely thin pool is still stuck, and nothing yet stops a
worker re-deciding every cycle. Constraint 3 remains the one that will bite.

The 9 voidborn-trapped workers (Node Beta ×8 + Node Alpha ×1) were restarted onto the
`52b2cfa` binary at ~10:10Z; none held a claim, all were docked and idle, so nothing was
orphaned.

## Operator's specified fix (2026-07-27), constraints 1-3 NOT yet built

> *"need some type of idle-trap-gate where after N cycles with no opportunity they return to `<home_base>` or travel somewhere beyond the default jump threshold"*

### Operator's design constraints (2026-07-27) — the tuning intent, read before implementing

1. **Moves must be SERIOUS, not incremental.** *"it needs to be a serious station movement, 2-3 jumps won't cut it when all the action is on the other side of the galaxy."* The observed gap was 18–26 jumps, so nudging the radius from 5 to 8 accomplishes nothing. Either commit to crossing the map or don't move.
2. **The objective is avoiding TOTAL starvation, not maximising capture.** *"some missed opportunities are okay, we will never get them all, but missing all of them is not okay."* So the gate should be tuned to guarantee a floor of activity, and it is fine if it leaves value on the table.
3. **Do not overcorrect into permanent transit.** *"we also dont want swing the pendulum too far the other way, always moving across the galaxy and never catching any opportunities because we are always traveling."* A worker in flight earns nothing, so relocation needs hysteresis: a high trigger threshold, commitment once moving (no re-deciding every cycle), and a minimum dwell/earn period at the destination before it may relocate again.
4. **Past a hard threshold, release everyone.** *"at a certain threshold it's worth the risk to send everyone free to get something."* So there are (at least) two tiers: a per-worker gate for ordinary barrenness, and a fleet-wide escape hatch when the whole pool is unreachable — at which point the distance constraint comes off entirely and the risk is accepted.
5. **That threshold is keyed on VALUE, and the bar is high.** *"like the fuel_cell, power_cell, trade_authenticators with ranges in the 100's of thousands net or even millions."* So the release trigger is not merely "we are idle" — it is "there exists an opportunity whose NET is in the 100k–millions band," and those are worth any distance. Note the named items are exactly the known fat tier: `trade_authenticator` is where the v0.547.1 economy patch moved the money ([[project_haul_revenue_halved_v0547]]), and `power_cell` run-size is bounded by arbitrage buy-order DEPTH ([[reference_haul_fleet_capacity_ceiling]]). Judge on **net**, not gross — today's best was 102,840 *gross* on `circuit_board`, i.e. right at the low edge of the band, and every row in the pool reported `fuel_cost: 0.0`, which is suspicious and must be verified before net can be trusted as a trigger input.

This reads as an escalation ladder: normal local scan (5 jumps) → after N barren cycles that worker may relocate long-range and commits to it → if the FLEET is starving, drop the distance limit for everyone. Constraint 3 is the one that will bite: without dwell/commit logic, tier-2 and tier-3 will oscillate.

Design notes:
- Count **consecutive barren cycles** per worker; past N, stop re-running the same doomed local scan and instead reposition.
- Two destinations to choose between: the worker's **`home_base`**, or **the best opportunity anywhere regardless of the jump threshold** — i.e. deliberately break the 5-jump rule once the local pool is proven dry. The second is strictly better when the pool is as lopsided as above (a 21-jump trip to a 102k gross run is obviously worth it), so price the trip rather than capping distance.
- **Anti-herd:** the trap was CAUSED by convergence, so a naive "everyone repositions to the best opportunity" rule recreates it one jump later. Stagger or partition destinations — this is the same saturation problem as [[reference_haul_fleet_capacity_ceiling]] (21 haulers already saturate the fat tier; leftovers earn 220 cr/jump vs 3,057).
- Related lever, complementary not alternative: the scanner runs `--min-profit 10000`, so nearer/smaller runs are never even recorded. Lowering it surfaces local work for a trapped cluster. [[project_haul_revenue_halved_v0547]] flags `MinProfit: 1000` as the allocation lever.
- The 5-jump constant is the same `maxJumps=5` that made smuggling chain 1 unreachable until `f522cc2` dropped the flat cap for missions and priced distance instead — **that commit is the working precedent for how to fix this**: price distance, don't cap it.

**Fleet restarted onto the current binary at 2026-07-27T03:41Z** (drain via SIGUSR1 logged NOTHING on the `f522cc2` overmind build — no drain lines at all — so it was a plain TERM; safe because every worker was idle with nothing in flight). Restarting does NOT fix the trap: the workers come back up in the same systems and idle again. Repositioning or the gate above is required.
