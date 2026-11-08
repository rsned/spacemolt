---
name: reference_commission_supply_materials
description: "Ship commissions: anyone can commission any class at a capable yard; SUPPLY-MATERIALS mode is the better route, because a cash-price commission makes the yard source materials from the market and stalls forever if they aren't listed"
metadata:
  type: reference
---

Operator, 2026-11-08 (discussing Axiomata, the favoured T4 combat hull):

- **Anyone can commission** a class at a shipyard of the needed tier. The quote/commission is
  gated on the requesting agent's own Piloting for the hull tier (T4 = Piloting 30; the_banker
  at 0 was refused `commission_quote axiomata`).
- **Supply the materials** (provide-materials mode). With the cash price the shipyard has to buy
  every material itself; anything not on the market means the build **never completes**.
- So before commissioning, check the class's `ship_build_materials` (spacemolt-knowledge.db) and
  have the stock on hand. Axiomata needs Solarian-only parts (solarian_composite 35,
  solarian_legacy_alloy 10, solarian_logic_matrix 6) plus capital_ship_frame 13, sensor_array 140,
  shield_emitter 140, targeting_computer 10, navigation_core 22 and more.

Distinct from building at our own station, which needs a license (T4 = 20M) and a shipyard of the
right tier; see [[reference_congregation_is_outerrim_exclusive]].
