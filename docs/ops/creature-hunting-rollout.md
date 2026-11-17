# Creature hunting rollout (operator checklist)

The code is merged-ready; nothing live has been changed. The operator decides
and does the following.

## Decide

1. Per hunter: `station` (its base) and `gift_to` (the collector's agent id) in
   `data/overmind/hunt-fleet.yaml`. `station` is where the hunter banks spare
   parts; `gift_to` is optional (absent = bank only, no gifting).
2. The collector agent. It must be DEDICATED: `collect_gifts` gifts any
   creature-drop cargo aboard, so do not use a hauler that carries other work.
3. The collector's schedule entry in `data/overmind/roles.yaml`, hourly:
   `collect_gifts <station ids...>`. Prefer POI ids over base ids for the
   station args (station ids are dual-named; do not mix them).

## Do

1. Gift unlock for each hunter that lacks it (`play_as`, via `go run
   ./cmd/tools/play_as`): accept the `market_participation_buying` mission,
   buy 10 of the cheapest item, `complete_mission <mission_id>`.
2. Rebuild `bin/worker`, then roll the hunt fleet and the collector's fleet:
   SIGTERM each worker, let the overmind relaunch it. Verify each worker's start
   time is later than the binary mtime.
3. Commit the config as `hunt: gauntlet hunters on creature work`
   (never `git add -A` with dirty `data/*.json`).

## Watch

- `First Hunt` mission acceptance.
- A Station Need delivery.
- `scan` captures.
- No `SHIP DESTROYED` for any hunter.
- The collector's `collect_gifts` log lines delivering parts to CJ.
