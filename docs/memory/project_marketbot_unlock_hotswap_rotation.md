---
name: project_marketbot_unlock_hotswap_rotation
description: marketbot_ramens_rest is the hot-swap stand-in that gets the pirate unlock first, then holds each resident's station while that resident is seconded to the unlock fleet — runbook-first (docs/runbooks/marketbot-unlock-rotation.md), automate after two clean rotations
metadata:
  type: project
---

**Operator plan, 2026-08-29:** "make marketbot_ramens_rest the hot-swap bot to
go through pirate unlock and then be the stand-in for every other marketbot
one at a time so they can also." 44 marketbots are still pirate-locked (the 9
stronghold bots hold it). Decision: **runbook first, automate later.**

**Runbook:** `docs/runbooks/marketbot-unlock-rotation.md` (committed
`fbce80b4`). Per rotation: set the stand-in's `station:` to X's station in
`mb-fleet.yaml` + `SIGHUP` mb (membership UPDATE → only that worker respawns,
1 login) → dashboard-remove X from mb → `SIGHUP` unlock → on graduation,
remove X from unlock, readd to mb. Two logins per rotation.

**Why it is shaped this way** (all verified 08-29):
- `bin/fleet-secondment` is fully flag-parameterised (`--home`,
  `--home-overrides`, `--home-socket`, `--ledger`) — a second mb↔unlock
  instance needs NO code; but only the haul role self-nominates
  (`pkg/worker/secondment_nominate.go`), so mb rotations need a `nominate`
  CLI (~30 lines) before automation.
- The stand-in's station cannot be changed live: `assign` only delivers a
  task script, and the reconciler never rewrites yaml. yaml edit + SIGHUP is
  an update because `diffSpecs` uses `reflect.DeepEqual`.
- Rotators live in BOTH yamls; removed-sets are the only switch
  ([[reference_secondment_overrides_are_removed_sets]]).
- Stand-in hull: cobble, 120 fuel, 14 jumps to the giver (~28 fuel) — pinned
  to `treasure_cache_trading_post` like the haul rotators.

**Purpose of the stand-in's own unlock** (operator, 2026-09-09): ramens_rest
needs the pirate unlock for its own sake too — travel through dangerous space,
smuggling missions for extra income — not merely as a prerequisite for standing
in. The cycle is: every NON-STRONGHOLD marketbot rotates through the unlock one
at a time, ramens_rest holds that bot's home station meanwhile, then moves on to
the next station.

**State 2026-09-09: step 0 is COMPLETE, and rotation #1 has never started.**

- ramens_rest has **graduated**: baseline 10 on all nine `pirate_*` factions.
- It is nevertheless **still in the unlock fleet**, idling at `gsc_0039_belt`
  with nothing left to win — the exact waste this campaign already documented
  (finished agents pinned in the pool). It holds ~598k credits.
- Marketbot unlock progress: **10 of 64 unlocked** (the 9 stronghold bots +
  ramens_rest); **54 still locked**. Only 1 marketbot is in the unlock fleet
  (ramens_rest itself); 63 are in mb.
- `mb-overrides.json` removes ONLY ramens_rest, so no resident is away.

**Next action is rotation #1**: pick a locked non-stronghold resident X, set
ramens_rest's `station:` (currently `""`, mb-fleet.yaml line ~117) to X's
station, SIGHUP mb, then move X into unlock. Nearest posts make the cheapest
first rotation — `marketbot_008` (The Zero Point Nexus, kornephoros, 5 jumps)
or `marketbot_009` (Ghost Runner Terminal, thuban, 6 jumps).

**History:** step 0 (stand-in's own unlock) started 08-29 after the mb restart
that activated the sensor net; graduation check = `agent_standings.baseline
>= 10` on all nine `pirate_*` factions. The four station pins (frontier →
mobile_capital, market_prime, node_beta, the_telescope → unknown_edge_waystation)
went live in the same restart.
[[project_pirate_reputation_unlock_campaign]] · [[project_player_sightings_timeline]]
· [[reference_outer_rim_mobile_capital_and_marketbot_homes]]
