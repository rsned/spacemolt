---
name: project_freight_wrong_origin_return
description: "Freight returns are ONLY legal at the package's origin station; wrong_origin used to be retried forever and breached fighter-1. Fixed 3f010dd, rolled out 2026-07-25"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2f3b8937-e63d-42aa-8015-c67d52bc5fd2
  modified: 2026-07-27T03:31:42.056Z
---

**✅ POLICY FLIPPED + PUSHED 2026-07-27 (`68b6d82`, `fix(freight): stop returning packages that are merely late`).** `chainStop` gained `RecoveryDeadlineTick`, and both `chainFeasible*` and `freightWorstReturnableStop` now measure `deliverableUntil()` — recovery deadline when the server supplies one, reward deadline as fallback — so a merely-late contract has positive slack and is never nominated for return. `chainOrder` still sorts on `DeadlineTick` because the speed bonus is real money. **NOT deployed:** the fleet still runs the older `bin/worker`, and contracts already in flight were persisted without `recovery_deadline_tick` so they keep the old conservative behaviour until delivered — wiring `shipping action=active` is the proper fix for that.

**🔴 PREMISE CHANGED 2026-07-26 (gameserver v0.549.0): the fix below is still correct, but the POLICY it serves is now mostly wrong.** Missing a deadline is no longer terminal — contracts stay deliverable **2880 ticks past** the deadline for a **capped 100–2500 cr late fee with no tier demotion**, and the dev team says plainly *"delivering late is now always better than opening the package."* So the thing this whole machinery exists to avoid (default → flat debt → all later accepts silently blocked) is no longer what a blown deadline produces. **A deadline collapse should stop triggering returns at all: deliver late and eat the fee.** The fly-home detour (and the `b48a288` guard that prices it) now spends fuel and delays the rest of the hold to dodge a fee capped at 2500 — a losing trade in nearly every case. Return becomes reserved for genuinely undeliverable cargo. Windows also went to 540 ticks + 180/jump (3–6x), so `chainFeasible` will rarely trip at all. [[reference_v0549_freight_and_percrew_pirates]]

**A shipping package can only be returned at its ORIGIN station.** `ShippingReturn` anywhere else is refused with `wrong_origin: Return the intact package at its origin station.`

**The bug (live 2026-07-25, cost one breach):** `freightChainRun` judged a contract infeasible, called `ShippingReturn` from wherever it stood, got `wrong_origin`, and treated it like any other failed return — record `return_failed`, keep the contract, **park the pass**. The next pass hit the identical state ~10s later and made the identical impossible call. fighter-1 retried **28 times in 4.5 minutes** while the log counted its own deadline down 11 → 0, then breached at 00:55:18Z.

**Fix (`3f010dd`, built `v0.2.5-6-g3f010dd`, rolled out to mission-learn 2026-07-25):**
- `freightIsWrongOrigin` matches the `wrong_origin` **code**, not the prose.
- `freightReturnAtOrigin` flies to `OriginBaseID`, settles the dock, retries — a clean discharge with no failure debt (a defaulted contract's debt silently blocks ALL later accepts, see [[reference_freight_orphan_salvage_unpack]]).
- If the origin is unreachable → new `freightStepUndischargeable` (NOT `Stuck`, which parks). The contract is excluded from victim selection and **flown to its destination**: a late delivery may still settle, parking cannot.
- `freightChainWorstStop` → `freightWorstReturnableStop(stops, tick, skip)`, returning ok=false when every blown stop is skipped. Without the skip set the victim loop re-nominates the same contract forever. Retries are now bounded by construction.

**Open judgment call, NOT built:** flying home costs hops, and on a cap-4+ carrier that detour can blow the *other* held contracts' deadlines → cascading returns. Loop re-prices each iteration so it degrades rather than breaks. Guard it on "remaining chain stays feasible" if this shows up live.

**Only `data/overmind/mission-learn-fleet.yaml` has `enable_freight`** — freight rollouts are a one-fleet job (39 live workers; craftsman-1/engineer-2/explorer-3 held out via the overrides sidecar). [[project_freight_load_confirm_regression]] [[project_freight_probationary_cargo_fence]]
