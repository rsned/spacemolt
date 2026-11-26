# Explorer weekly sweep — design (small change)

## Intent

Keep the charted galaxy current: POI renames, hidden POIs and wormholes we
missed, and which wildlife lives at which POI (for the hunt fleet and anyone
needing wildlife goods). Goal (operator, 2026-11-26): **eventually cover every
system**; one ship reaching everywhere roughly once a week, then starting
again, is fine.

## Today

The `explorer` role (`pkg/worker/explore.go`) already does the per-system work:
`survey_system` (hidden POIs, wildlife census), a tour of every POI (capture,
renames, two wildlife reads per POI), dock/capture/refuel at stations, NEW
wormholes logged and never entered. Its target picker (`NextExploreTarget`)
takes the nearest frontier / unvisited / stale system, with stale hard-coded
to 1 day (`DefaultExploreStaleTicks` = 8640 ticks).

## Approaches considered

1. **Configurable age limit, 7 days for the sweeper** — CHOSEN (operator,
   2026-11-26). Nearby systems stay fresh for a week, so nearest-stale keeps
   pushing the ship outward; a lap that runs past a week just continues.
2. Oldest system first, distance as tiebreak — guarantees the stalest corner
   next, but cross-galaxy hops every time.
3. Fixed precomputed lap — most code; the route goes stale as the map changes.

## Design

1. **`explore_stale_days` fleet-yaml field** (worker config, default 1 = today's
   behaviour). The `explorer` dispatch passes it as `ExploreDeps.StaleTicks`
   (days × 8640).
Staleness stays the KB's `LastUpdatedTick`. Passing traffic refreshes busy
hubs without a tour, but ~70% of the galaxy is not visited on a given day, so
there is plenty to choose from (operator, 2026-11-26). A per-explorer tour
record was considered and dropped.
No stronghold filter in the picker (operator, 2026-11-26): every flying pilot
is being pirate-unlocked, and the Autopilot stronghold gate still refuses the
route for any agent that is not.

## Rollout

explorer-7 (Survey Vessel, 1,020 fuel, idle at Nexus Prime, no pathfinder —
not needed for lane travel) joins a fleet as `role: explorer`,
`explore_stale_days: 7`. Check its survey-scanner fit first; without one there
is no survey (no hidden POIs, no census).

## Non-goals

Entering wormholes; frontier/off-lane travel (the `frontier_explore` fleet
covers that); hunting.

