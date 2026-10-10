---
name: reference_buying_a_hull_by_hand
description: "Hand-flying an agent to buy a listed ship: buy_listed_ship AUTO-SWITCHES and stores the old hull, but autopilot lands you in SPACE and POI names with apostrophes do not resolve — travel by POI id, then dock"
metadata:
  type: reference
---

Verified 2026-10-10 across 8 purchases (see
[[project_haul_fleet_rehull_2026_10]]).

## The sequence that works

```
travel <station_poi_id>     # POI ID, not the display name
dock
buy_listed_ship <listing_id>
```

- **`buy_listed_ship` auto-switches the active ship** and stores the old one
  at that station: `"Your old ship is stored at this station."` No separate
  `switch_ship` — so the nexus trap in
  [[reference_assist_tanker_migration]] does NOT apply to a purchase. The new
  hull arrives with a full tank.
- `buy_ship <class>` is the shipyard path; `buy_listed_ship <listing_id>` is
  the marketplace/Station-Manager path. Station Manager listings are the cheap
  ones.
- Sales tax is ~1% on top of the listed price.

## Two traps that cost a second pass each

1. **`autopilot <system> <poi>` arrives in the system but NOT docked** —
   salvager-8 ended at `colony_debris_field` and the buy failed `not_docked`.
   Autopilot's poi argument does not reliably berth you. Always follow with an
   explicit `travel <poi_id>` + `dock`. This is the movement-layer face of
   [[reference_sell_leg_dock_gap]] / [[reference_pin_arrival_check_four_directions]].
2. **POI DISPLAY NAMES do not resolve for travel/autopilot — only POI ids
   do.** Not just an apostrophe problem: both `travel "Ramen's Rest"` and
   `travel "Unknown Edge Waystation"` returned `Unknown destination`, while
   `ramens_rest` and `unknown_edge_waystation` worked. Get ids from
   `ship_listings.station_id`, which carries the POI id beside `listing_id`.
   Passing a display name as autopilot's optional poi arg fails the same way
   AFTER the 16-jump system leg has already been flown — so it costs a whole
   second trip, not just a retry.

## Prices drift; listings persist

Across ~40 minutes the same `listing_id`s re-priced by ~2% (congregation
5,657 → 5,544 → 5,554 at Starfall). Re-read `ship_listings` right before
buying, but the listing ids themselves stayed valid.

## Agents move while you plan

A nearest-listing assignment computed from a status snapshot is stale by the
time the agent drains: trader-4 was 1 jump from a listing at plan time and 11
at execution; salvager-6 went from 13 to 24. **Assign from live position at
dispatch**, and expect ~60s per jump at speed 1 (`jumpTicks = max(1, 7-speed)`).

## Hand control requires removal from the fleet

`POST /api/overmind/fleets/{fleet}/agents/{id}/remove`, wait for
`membership: "<id>" removed from fleet` (drain is up to
`DefaultRemoveDrainTimeout` = 4m), confirm the worker PID is gone, THEN open
the play_as session — otherwise two sessions contend for one account. Readd
after. Anchor any log grep to today's date: this log holds months of
identically-worded lines.
