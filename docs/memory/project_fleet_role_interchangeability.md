---
name: project_fleet_role_interchangeability
description: "Operator vision (2026-08-01): qualify every general-purpose agent for both freight licence and smuggling reputation so pool membership becomes elastic — surge capacity for arbitrage gold rushes. NOT YET DESIGNED."
metadata: 
  node_type: memory
  type: project
  originSessionId: 2671530b-761a-4dbe-b378-62f725016c20
  modified: 2026-08-01T02:54:47.896Z
---

**STATUS: vision captured, design pass NOT yet run.** Operator asked to brainstorm this fresh; nothing is built. Do not start coding it without that design session.

**The goal, in the operator's framing:** agents become interchangeable so the fleet can respond to opportunity. *"when a sudden massive arbitrage pops up, everyone fitted and available can be shuffled to Haul pool to get in on the gold rush."* Surge capacity is only real if agents are ALREADY qualified when the spike hits — qualification takes days, a spike lasts hours. That is the whole argument for doing it in advance.

**Sequencing the operator specified:**
1. **Now** — the freight crew (21 licensed, already cargo-fitted) run the smuggling reputation missions.
2. **Then** — rotate freight-licensed agents INTO the Haul pool, and bring Haul agents back through freight probation + the smuggling reputation gate.
3. **Steady state** — most agents hold both credentials; pool membership becomes a scheduling decision rather than a fixed identity.

**Scope limit (operator):** *"some agents will stay single purpose like marketbots and assist fleets for the near future."* Those two have hard station pinning and specialist fitting (`refueling_pump`, resident scheduling), so the lattice covers only the freight/mission/haul agents.

**johnny_cab multi-role (operator, same conversation):** move it to a hull with **≥100 cargo in ADDITION to passenger berths**, so it can double/triple up — Passenger+Freight or Passenger+DeliveryMission to the same destination.
- The routing half already exists: `SelectMissionSet` anchors a trip on the best-net candidate and stacks others sharing its destination SYSTEM. Multi-role is that same rule generalised across work TYPES; what is missing is a selector that sees passenger, freight and mission sources at once.
- Economics favour it: a passenger run's fuel and travel time are already sunk, so stacked cargo is near-pure margin.
- 🔴 The hull is a PURCHASE (`buy_ship`/`commission_ship`), not a refit — berths+100 cargo is a specific class. Verify one exists before designing around it.
- 🔴 johnny_cab is the treasury mule (it gifted 100k x14 during the stranding crisis) and the shuttle canary. A second job contends with those errands — decide whether that is acceptable or whether a different agent takes passenger+cargo.

**Constraints I identified that should shape the design:**
- **Rotation INTO freight is expensive.** A Haul agent entering freight starts *probationary*, and that tier is where agents stall: of 4 probationary agents on 2026-07-31, three (explorer-8, explorer-11, fighter-7) had exactly 1 delivery each and were **starved of contracts, not failing them** — their logs are wall-to-wall `no candidate (No freight contracts are posted at this station right now)`. Bulk rotation would park a chunk of the fleet in the least productive tier. Argues for a few at a time, continuously.
- **The smuggling gate is now expensive at scale.** Each agent needs L3, capped at `missionSmugglingXPBudget`=25,000cr; 21 agents is a **525k credit programme** if they all hit the cap. Worth running in waves (~5) so spend is paced — and if the treasury fix lands ([[project_empire_treasury_payout_collapse]]) the same XP gets bought far cheaper.
- **fighter-1 is stuck, not starved** — 38 contracts, 7 delivered, with `breached`/`return_failed`/`returned_infeasible`. Different problem from the starved three; needs its own look.

**The unifying design question to answer in the brainstorm:** *what makes an agent eligible for a role, and who decides when it switches?* Rotation, surge-shuffling and multi-role all hinge on it; designing them separately would produce three incompatible mechanisms. Sub-questions: wave size; who rotates first; whether pool membership becomes automatic or stays operator-triggered (today: overrides sidecar + SIGHUP, [[project_fleet_pool_dynamic_membership]]); how "fitted and available" is evaluated at surge time; whether multi-role selection is one unified selector or a per-role priority.

Related: [[project_overmind_fleet_manager]] · [[project_haul_fleet_capacity_ceiling]] · [[project_smuggling_enablement]]
