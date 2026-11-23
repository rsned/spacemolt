---
name: reload-replaces-whole-magazine
description: "reload consumes one whole ammo item and replaces the magazine; rounds still loaded are lost, so never top up a nearly full weapon"
metadata:
  node_type: memory
  type: reference
  originSessionId: 734f5d40-f51d-46b3-9086-72af3ca8b239
  modified: 2026-11-23T03:56:47.545Z
---

A `reload` spends one whole ammo item from cargo and replaces everything in the magazine. Topping up 648/650 costs a full box and discards the 648 (operator-confirmed 2026-11-22; seen live when pirate-5 burned both spare boxes this way).

**How to apply:** only reload a weapon that is empty or low (the hunt worker uses below half, `huntWorthReloading`). "No spare ammo aboard" does not mean dry: check loaded rounds before sending a ship to rearm. Also `combat_state.max_weapon_reach` is the longest FITTED weapon even if it is dry, so range must be judged on weapons that can fire.

Related: [[index_combat_wildlife]]
