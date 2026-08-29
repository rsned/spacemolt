---
name: project_price_command_depthwalk
description: "play_as `price` command uses single-best-ask pricing; revisit depth-walk later"
metadata: 
  node_type: memory
  type: project
  originSessionId: 747fa2be-cbd4-4964-bcfc-3124b2094d4c
---

The play_as `price` command (suggested sell price for `create_sell_order` = component/ore market cost + 20% margin) prices each component as **single best ask × qty**, NOT a full order-book depth-walk.

**Why:** Simpler for MVP, and the 20% margin absorbs thin-book slop. The kb build-cost matrix ([[reference_...]] build-cost-matrix design, kb repo `cmd/generate-build-costs`) *does* depth-walk (consume cheapest sell tiers until qty met) for accuracy.

**How to apply:** If users report the `price` suggestion is too low on thin/illiquid items (component ask qty ≪ required qty), upgrade the Nearby/Market-wide pricing in `pkg/pricing` to a depth-walk over `market_orders` sell tiers (see kb `matrix.go` depth-walk primitive as reference). Until then, single-best-ask stands.

Related: two cost bases (Nearby = local + ≤2 hops; Market-wide avg) × two decompositions (recipe inputs; BoM ore). Spec: `docs/superpowers/specs/2026-07-06-price-command-design.md`.
