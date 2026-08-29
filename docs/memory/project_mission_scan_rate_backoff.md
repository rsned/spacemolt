---
name: project_mission_scan_rate_backoff
description: "QUEUED optimization: back off the mission/freight board RE-READ cadence when a worker is dry/parked — boards change slowly, but workers re-poll every ~10s tick, causing high server load + 893MB log spam"
metadata:
  node_type: memory
  type: project
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-23T21:40:51.404Z
---

**QUEUED optimization 2026-07-23 (user: "dial down the mission/shipping scan rate — change rate per hour is not that high").**

**Current behavior:** the mission runner re-reads the board EVERY standing-loop pass (~`game.SleepTick` 10s). Existing throttles cover REPOSITIONING only, not the read: `missionDryPassLimit` (hop after N dry passes), `parkedUntil` (after too many dry hops, park at station and stop hopping — but "passes keep re-reading the local board for free"), and skip-LOG dedup (mission.go:74-76: a dry board's skip lines print once, not every pass). So a parked/dry worker still fires `get_missions` + freight `shipping list` every ~10s. Across the 42-worker mission-learn fleet = ~12.6k mission polls/h + ~14.5k freight polls/h (measured 2026-07-23), against boards that turn over on the order of minutes → most polls wasted. Also the main driver of the 893MB `mission-learn-overmind.log`.

**Fix:** scan-interval BACKOFF when dry/parked — stretch the board re-read cadence graduated: active ~10s → first dry passes ~30-60s → **deep-parked up to ~5 MINUTES** (user, 2026-07-23: "might even start at 5 minutes sleep time"). ~5min = ~30x fewer polls vs the 10s tick. Composes with existing park logic (park = "don't hop"; add "and don't re-poll as often"). Cuts server load + worker CPU + log volume ~proportionally.

**Make it ADAPTIVE:** snap back to fast polling the instant an accept happens (or the board's entry-set changes), stretch out again on a dry streak. So the 5-min floor only applies to a genuinely dead board.

**Guardrail:** a worker could miss a briefly-available mission in a 5-min gap — but the fleet completes ~0 missions/h now, so that risk is negligible vs the load win. Once TRADE/SMUGGLING/probation raise the accept rate, the adaptive snap-back keeps responsiveness where work actually exists. The respawn-responsiveness vs poll-load tradeoff is the whole design question.

Knobs live in `pkg/worker/mission.go` (missionDryPassLimit ~L23, park constant ~L30-33, `missionRunState{dry, hopsDry, parkedUntil}` ~L61-76) + the poll cadence in the standing/schedule loop (`pkg/worker/schedule.go` StartLoop ticker / `game.SleepTick`). Relates to [[project_mission_category_coverage]] [[project_mission_learning_pool]] [[reference_sleep_constants_actual]].
