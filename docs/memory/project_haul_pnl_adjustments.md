---
name: project_haul_pnl_adjustments
description: Deferred haul-pnl extension — non-trading adjustments for full balance reconciliation
metadata: 
  node_type: memory
  type: project
  originSessionId: 1e4c8a31-2e69-4e33-8b4b-1c2956839940
---

DEFERRED (user said "push (1) for later" on 2026-06-27): extend `cmd/tools/haul-pnl`
to break out the non-trading credit movers so `TRUE_NET + adjustments ≈ actual
balance change`. Sources are already in each agent's `data/agents/<id>/action_log.jsonl`:
- `trading.gift_received` (+, e.g. trader-1 got a 100k infusion)
- `ship.insured` (−, ~4.2k/ship; user insured all haul agents on non-starter ships)
- `ship.bought_listing` / ship buys + commissions (−)
- `other.rent_paid` (−, facility rent)

Current `haul-pnl` TRUE_NET = Σ seller fills − Σ buyer fills − Σ ship.refuel total_cost
(trading flow only; correctly excludes the above). The adjustments view is what makes
the report reconcile to real credit-balance deltas before a baseline reset.

See [[project_current_status]]. Tool shipped 2026-06-27 (commit 0afa41b).
