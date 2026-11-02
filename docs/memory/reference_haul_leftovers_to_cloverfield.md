---
name: reference_haul_leftovers_to_cloverfield
description: "Since 2026-11-01 haulers never list leftover/failed-arbitrage cargo; they carry it and gift it to miningbot_0000 at Conjunction Junction (within 3 jumps, or when leftovers > 50% of hold); faction_deposit_all sweeps it into CRFT faction storage"
metadata:
  node_type: memory
  type: reference
  originSessionId: 90ad50cf-9c6a-4f6f-a1d4-f3dc05c8ebcb
  modified: 2026-11-02T01:31:46.611Z
---

**Behaviour (pkg/worker/haul.go, operator decision 2026-11-01):**
- A failed arbitrage leg (demand gone at the destination) completes the claim and KEEPS the goods (`haulKeepForDump`). Consigned routes (gifted stock) still list at the planned price.
- Idle leftovers are sold only into a buyer at the current station; otherwise carried. No more 10%-under-best-ask sell orders.
- `haulDumpLeftovers`: within `haulDumpRadius` = 3 jumps of Cloverfield, or leftovers > `haulDumpHoldShare` = 50% of the hold, the hauler autopilots to Conjunction Junction and `send_gift`s every non-reserved item (fuel_cell stays) to `haulDumpRecipient` = miningbot_0000, source cargo.
- miningbot_0000 runs `faction_deposit_all` (half-hourly): every personal-storage item into faction storage except `energy_crystal` (reserved for the drone project, [[project_drone_project_crystal_reserve]]).

**Why:** the operator: "all those odds and ends will eventually come up for use in some crafting request". Haulers had been leaving cost-price asks that sold a unit at a time for weeks (salvager-7's 1,059 coolant_fluid at Grand Exchange, 214 fills).

**Pre-existing asks** posted before the change were left to sell out. The line dashboard defers the haul fleet (`--defer-fleets mb,haul`) and credits asks by poster, so these never appear as lines ([[reference_line_dashboard]]).
