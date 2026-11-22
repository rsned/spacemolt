---
name: reference_hand_crafting_is_free
description: "Hand crafting (station workshop venue) charges NO labor fee; only facility crafting is billed — don't gate hand-craft tasks on a bot's credits"
metadata:
  node_type: memory
  type: reference
  originSessionId: 8c15f693-8825-4572-8f22-51141db1e328
  modified: 2026-11-19T17:57:39.111Z
---

Crafting by hand is free: no labor fee, no credits needed (operator, 2026-11-19).
Older notes (e.g. [[project_drone_marketbots]] 09-19, "insufficient_credits ...
crafting charges labor") blamed labor fees for failed crafts on low-credit bots;
do not repeat that for hand crafts.

**How to apply:** when adding `craft <recipe> N` tasks to a drone marketbot
(hand_craftable recipes like basic_iron_smelting / basic_copper_processing run in
the "Station Workshop" venue), don't check or top up its credits first. A player
station still needs a faction_workshop before it allows any crafting
([[project_station_two_beid]]).
