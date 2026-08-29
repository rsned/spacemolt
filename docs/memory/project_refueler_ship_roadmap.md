---
name: project_refueler_ship_roadmap
description: Refueler (assist-fleet) hardware upgrade path — expanded_fuel_tank module then Tanker-class hulls; larger tanks = bigger rescue transfers
metadata: 
  node_type: memory
  type: project
  originSessionId: fe15d9e5-8526-406c-8288-52fa83e238fd
---

**UPDATE 2026-08-14: the Tanker milestone is DONE for 4 of 5 assist agents** (haven Reserve 4,000; sol/nexus/frontier Capacity 1,500; krynn still Siphon 140) — full record in [[reference_assist_tanker_migration]]. Tankers have the pump BUILT IN, so the module note below is obsolete for them. Remaining roadmap = tier-4 hulls (10,000-12,000 fuel, Piloting 30) and a hull for krynn.

Roadmap for growing assist-fleet refueler fuel capacity so they can spare more per rescue (they transfer from their own tank via `refuel target=<player>`, needs a Refueling Pump module).

- **Early upgrade (near-term):** `expanded_fuel_tank` is a **utility_slot** module that adds **+100 fuel tank capacity**. Should be a standard early ship-upgrade path option for refuelers — fit these before chasing new hulls.
- **Long-term hulls:** **Tanker**-class ships (there are **Tier 2** and **Tier 4** variants) hold **1500+ up to 10,000 fuel**. Long-term roadmap target for dedicated refuelers.

**Why:** rescue transfer is capped by the rescuer's spare fuel (see [[project_current_status]] rescue-fuel sizing work: `transfer = MIN(strandee remaining capacity, rescuerFuel - homeReserve)`). Bigger rescuer tanks directly raise how much a single rescue delivers and how many strandees one refueler can service before re-tanking. Big-tank haulers (420+ fuel) currently re-strand because small assist ships can only spare ~70/rescue.

**How to apply:** when revisiting refueler fitting/procurement after the dynamic rescue-fuel-sizing change lands, add expanded_fuel_tank to the assist-ship fit template first; scope Tanker-class acquisition as a later milestone. Relates to [[project_treasury_and_shuttle]] (assist/rescue standing behavior).
