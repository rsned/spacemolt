---
name: reference_v0549_freight_and_percrew_pirates
description: "gameserver v0.549.0 patch notes: late delivery replaces default (2880-tick grace, capped fee, no demotion), shipping action=active listing, package_id addressing, and pirate standing going PER-CREW — plus what each one invalidates in our code"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-27T00:56:04.411Z
---

**Operator-supplied patch notes, 2026-07-26 (gameserver v0.549.0).** Client `BuiltForAPIVersion` was still `v0.547.1` when this landed (`pkg/version/checker.go:48`) — needs the docs sync + symlink flip, which IS the API-bump record. [[reference_server_docs_sync]]

## Freight — the policy premise INVERTED

- **Missing a deadline is no longer terminal.** Contracts stay deliverable **2880 ticks past** the deadline: forfeit the reward, pay a **100–2500 cr late fee** (always < half of defaulting), **no tier demotion**. Dev team states outright: *"Delivering late is now always better than opening the package."*
- **Delivery windows are 3–6x longer:** **540 ticks + 180 per jump** (standard run), with room to reroute. Speed bonus still pays for arriving early.
- `return` on an overdue package pays that same late fee; returning **before** the deadline is still free.
- New **`shipment_overdue`** notification when a run passes its deadline, carrying the remaining grace.
- **`shipping action=active`** lists every contract you are party to — destination, reward, ticks left.
- **`shipping get`/`track`/`deliver`/`return` now accept `package_id` as well as `shipment_id`**, so you can act on a sealed box straight from the hold without knowing its contract ID. `inspect` on that package now names the contract too.

**What this invalidates in OUR code (audited 2026-07-26):** the entire doomed-contract return machinery is built on a premise that no longer holds — that blowing a deadline means default, and default means a flat debt that silently blocks every later accept. Affected: `chainFeasible`-driven victim selection, `freightWorstReturnableStop`, `freightOriginNav` + the `b48a288` detour guard, and the `3f010dd` `wrong_origin`→fly-home path (`pkg/worker/freight.go`, `freight_chain.go`). **Correct new policy: a deadline collapse should NOT trigger a return at all — deliver late and eat the capped fee.** A fly-home detour now costs fuel plus delay to the rest of the hold to avoid a fee capped at 2500; that trade is almost always losing. Return should be reserved for genuinely undeliverable cargo. Also `freightTicksPerHop = 19.0` × `freightDeadlineSlack = 1.5` (≈28.5 ticks/hop) is now far inside the server's 540+180/jump allowance, so deadlines will rarely bind. [[project_freight_wrong_origin_return]]

## Pirates — standing is PER-CREW now

- **Docking is gated on standing with the crew that runs the station** — *"being barred from one stronghold says nothing about the next."*
- Pirates report **`faction` and `faction_name`**; the pirate tables in `get_nearby` and `get_state` gained a **`crew`** column. Dev warning is for **column-position parsers**.
- **Completing the pirate contact chain still earns an introduction to the whole network at once** — the `an_introduction` blanket unlock survives.
- **Supplying a stronghold / running its cargo / moving a crew's contraband now earns goodwill with THAT crew**, not with pirates in general. **Missions state which crew their pirate standing pays.**

**Our exposure:** the column-position warning is a **no-op for us** — we decode JSON by key, never by position. But `serverapi.NearbyPirate` (types.go:1031) and `PirateWarning`/`PirateCombat`/`PirateDestroyed`/`PirateSpawn` (events.go:241+) have **no `faction`/`faction_name`/`crew` fields**, so that data is silently dropped (same class as [[project_kind_discriminator_drift]]). And **player standings are still not modelled at all** in pkg/game — confirmed again 2026-07-26 — so the per-crew gate is greenfield rather than a break. [[project_smuggling_enablement]]
