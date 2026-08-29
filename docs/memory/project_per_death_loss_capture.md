---
name: project_per_death_loss_capture
description: Follow-up feature — instrument per-death loss capture (lost hull + cargo manifest + cause + insurance payout) so detour/PvP losses are measurable
metadata: 
  node_type: memory
  type: project
  originSessionId: 868ac572-f106-4923-9823-e82622bf66b9
---

**Follow-up task (requested 2026-06-28):** add per-death loss capture to the worker fleet so ship-destruction cost is actually measurable.

**Why:** When asked "how much did the Crix/Bellatrix stronghold detour cost the haulers?" the answer was *unrecoverable* — insurance pays out CREDITS on death (masking the loss in balance history), the death events weren't in the post-relaunch log, and daily fleet-history snapshots are too coarse. The real cost (lost cargo_expander fittings + in-transit cargo + productivity gap in the 50-cargo starter) is never recorded anywhere. Pairs with [[reference_ship_replacement_workflow]] — that feature SPENDS to recover; this one MEASURES what was lost.

**How to apply:** Hook the worker's death handler (the `player_died` / ship-destruction protocol event — see internal/protocol + pkg/game handleResponse). On death, capture and persist one loss record:
- lost hull: class id + insured value + the cargo_expander/utility modules that were fitted (gone, not refunded)
- cargo manifest at destruction: items + quantities + est. value (e.g. the "40 trade_authenticators aboard, routed to sell @crix_stronghold" load that was lost)
- context: system, POI, cause (stronghold / pirate / police / PvP), tick, and the insurance payout received (so net loss = fittings + cargo − payout)
Persist to a new `ship_losses` table (or extend the existing `danger_zones`) in the knowledge DB; surface a cost-of-losses report (cmd/tools) and optionally a column in the overmind status page. Then a question like "what did incident X cost" becomes a query.

**Confirmed data gap (2026-06-28 forensics):** the cargo a destroyed worker was carrying is recoverable from NOWHERE today. `data/agents/<agent>/checkpoint.db` `known_state` has a `cargo_json` column but stores only the LATEST state (single row, overwritten) — empty once recovered. `data/agents/<agent>/action_log.jsonl` logs `trading.exchange_fill` (buy/sell item+qty+price) but the **pkg/worker runtime does NOT write to it** (only play_as does; worker-era agents have zero fills logged). The overmind haul log records stronghold-bound loads by type/qty (e.g. trade_authenticator ~40/run, salvage_metal 75–300, steel_plate 250) but the `haul: opp …` lines are NOT worker-tagged, so cargo can't be pinned to a specific dead ship. Death fingerprint in logs = `reconcile diverged: system changed "<stronghold>"->"<home_base|empty>"; docked false->true` followed by a `Ready! Credits: X | Ship: <name> | Cargo: 0/<cap>` respawn line with credits UNCHANGED (insurance covers hull credit-value; cargo+fittings are the real loss). So capture must happen AT death in pkg/worker, not be reconstructed after.

**Status:** NOT STARTED — captured as a follow-up while building the haul ship-replacement feature ([[reference_ship_replacement_workflow]], plan `docs/superpowers/plans/2026-06-28-haul-ship-replacement.md`). The worker fleet currently has NO rebuy logic and NO loss instrumentation.
