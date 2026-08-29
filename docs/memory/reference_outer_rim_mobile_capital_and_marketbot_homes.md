---
name: reference_outer_rim_mobile_capital_and_marketbot_homes
description: Frontier was the Outer Rim capital until the capital became MOBILE (`mobile_capital` station, First Step on 08-29, Altais on 08-14); the Frontier system now has no station, and marketbot_frontier/ramens_rest/the_telescope all live at Unknown Edge Waystation
metadata:
  type: reference
---

**Operator, 2026-08-29: "frontier was the capital of Outer Rim until the
capital began moving."** The Outer Rim capital is now the station
`mobile_capital` ("Mobile Capital"), which changes system: it was in Altais
on 2026-08-14 (assist-frontier "on its way back to mobile_capital"; the dry
fuel-desk incident) and in **First Step** on 2026-08-29. The `frontier`
system still exists (empire outerrim) but holds only `frontier_star` — no
station. Route/home logic keyed to a fixed "Frontier Station" id is stale by
construction; resolve the capital by querying `pois` for `mobile_capital`
each time.

**Why marketbot names and positions disagree (and when that is NOT a bug):**
residents with `station: ""` in `mb-fleet.yaml` never move — `ensureHome`
no-ops with no configured station (`pkg/worker/dispatch.go:528`) — so each
sits at its in-game `home_base`, i.e. wherever the account was created.
- `marketbot_frontier`, `marketbot_ramens_rest`, `marketbot_the_telescope`
  all sit docked at **`unknown_edge_waystation` (Unknown Edge, outerrim)**.
  Frontier has no station any more; The Telescope has NO station at all
  (two nebulae only); Ramen's Rest is in Last Light. Only ramens_rest has a
  real namesake it could be posted to.
- `marketbot_market_prime` sits at `grand_exchange_station` in **Haven**
  (same home_base as marketbot_haven) — never went to Market Prime
  Exchange (system `market_prime`, 1 jump).
- `marketbot_node_beta` sits in Nexus Prime; its namesake
  `node_beta_industrial_station` is in Node Beta.
- 29 of 36 station-less residents match their namesake only because the
  account was created there. `marketbot_001/002` are numbered on purpose
  (player-station residents, Dheneb/Arneb).

**How to apply:** a resident goes where `station:` says, nowhere else. To
post one, set `station: <poi id>` in the yaml and respawn that worker
(remove/readd or fleet restart). Do not "fix" the Outer Rim three onto
Frontier/The Telescope — those stations do not exist.
[[project_player_sightings_timeline]] · [[reference_station_id_aliases]]
