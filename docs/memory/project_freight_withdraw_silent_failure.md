---
name: project_freight_withdraw_silent_failure
description: "Freight 'package did not load into cargo after withdraw' (52 events, 18 agents) — the withdraw SUCCEEDS and its request_id-correlated action_result carries quantity/cargo_total/cargo_space/storage_remaining, but the freight path throws that payload away and infers from a 30s cargo poll instead. A zero-movement 'success' is indistinguishable from a slow load, so a healthy contract gets returned."
metadata: 
  node_type: memory
  type: project
  originSessionId: f744e650-ff1a-4add-9401-5a3087024568
  modified: 2026-07-27T23:16:36.863Z
---

**✅ SHIPPED + PUSHED 2026-07-27 (`6eb2c10`), fleet restarted on it.** `freightLoadPackage` now confirms from the withdraw's correlated `action_result` (`freightWithdrawConfirmed`), falls back to the old poll only when no such reply is readable, and on a still-unconfirmed load **parks for `freightLoadMaxParks`=2 passes instead of returning** (`freightLoadUnconfirmed` + `missionRunState.noteLoadPark`). Tests proven red before green. **STILL OPEN: the clobber itself** — see the mechanism below; operator deferred it as a separate change ("then we can figure out the clobber"). Fixing `parseShipData` would close a whole class of stale-cargo bugs across mining/hauling/crafting.

**Traced 2026-07-27.** Symptom: `freight: package package:<id> did not load into cargo after withdraw; returning contract` — 52 occurrences across 18 agents (fighter-4 8, explorer-8 5, engineer-6 5, fighter-5 4, explorer-5 4).

**The wire flow is two-part and request_id-correlated** (operator-captured live trace, craftsman-1, withdraw_items xenon_gas 1):
1. `ok` — `{"command":"withdraw_items","message":"Action 'withdraw_items' pending...","pending":true}`
2. ~3s later, SAME request_id — `action_result` — `{"command":"withdraw_items","result":{"action":"withdraw_items","cargo_space":344,"cargo_total":1,"item_id":"xenon_gas","quantity":1,"storage_remaining":296},"tick":...}`

Note the payload is wrapped in `result` (same shape as [[reference_craft_action_result_wrapping]]), and the client logs `Action result: withdraw_items (unhandled)` — **no state parser** — while still doing `Stored raw JSON for withdraw_items`. So `GetRawJSON("withdraw_items")` already holds the answer.

**CORRECTION — an earlier version of this note said `WithdrawItems` awaits only the pending ack. That was WRONG.** `terminateOnAction` (pkg/game/terminator.go:21) explicitly treats a `pending:true` OK as intermediate and waits for `action_result`/`action_error`/`error`. `WithdrawItems` (client_commands.go:747) uses that default terminator, so it *does* block for real completion and *does* return an error on failure or timeout.

**Actual root cause — the package DID load; the poll went blind.** Operator's decisive argument (2026-07-27): the **returns all succeeded** (fighter-5: 6 returns, 0 breaches, 0 defaults, no debt). `shipping return` requires the package in your active ship — proven by the failure code `package_not_present: That exact sealed package is not in your active ship`. A succeeding return therefore means the package **was aboard**, so the withdraw worked and the confirm poll simply failed to see it.

**🔴 THERE IS NO CLOBBER — that theory was tested and DISPROVED 2026-07-27.** Do not re-derive it. Live `get_status` on craftsman-1 returns the ship object with the **full, current** cargo array (`[{"item_id":"xenon_gas","quantity":1}]`, cargo_used 1, capacity 345), so `parseShipData`'s `c.state.Ship = ship` writes fresh truth, not an empty hold. A sweep of every cached payload in `data/game-api/*/` found **zero** ship-like objects lacking a `cargo` key, and `pkg/game` has only two wholesale cargo writers — `parseShipData` (client.go:3209) and `parseGetCargoData` (client.go:3279) — both carrying full cargo. (`cargo` IS optional in the openapi `Ship` schema, which is what made the theory plausible; the server populates it anyway.)

**The actual gap is an ASYMMETRY, not a clobber.** `parseActionResult` (client.go:3726) has cases for `deposit_items`, `craft`, `create_sell_order`, `create_buy_order` — and **none for `withdraw_items`** (hence the live debug line `Action result: withdraw_items (unhandled)`). So a SUCCESSFUL withdraw never adds the package to `state.Ship.Cargo`. The only thing that can correct local cargo afterwards is an explicit refresh — and `freightPollLoaded` (freight.go:780) discards that refresh's error with `_ = deps.Client.GetCargo(ctx)`. A `GetCargo` that never lands is therefore indistinguishable from "the package never loaded", for the full 30s budget.

**Proposed follow-up (small, symmetric, helps every role — NOT the risky parseShipData surgery originally floated):** add a `withdraw_items` case to `parseActionResult` applying `item_id`/`quantity`/`cargo_total` from the reply, and stop swallowing the `GetCargo` error.

The freight path never inspects the withdraw's own `action_result`, which reports the truth immune to this race (`cargo_total`, `cargo_space`, `quantity`). `freightLoadPackage` (freight.go:793-815) discards it and infers from the poll, then **returns a healthy contract** as `returned_infeasible`. The error path at freight.go:799 has fired **0 times in the entire log** — no withdraw ever actually failed.

**Eliminated, do not re-investigate:**
- Poll refresh works — `parseGetCargoData` (client.go:3255) writes `Ship.Cargo` + `CargoUsed`.
- Item-id format `"package:"+PackageID` is right — 70+ successful loads match on it.
- Not an accept→withdraw timing race — successful and failing cases have identical timing.

**Fix (minimal, no new server call):** after `WithdrawItems` returns, read `deps.Client.GetRawJSON("withdraw_items")`, unwrap `result`, and gate on `quantity`/`cargo_total` instead of polling. play_as already does exactly this pattern — `simpleCommand` (main.go:8734) attaches `game.WithResultSink` to capture the request_id-correlated frame, and `formatItemTransfer` (main.go:5890) reads `dest_total`/`source_remaining` with `cargo_total`/`storage_remaining` as fallbacks. **Read the play_as flow before writing the freight fix.**

Then: on an unconfirmed load, **park (Stuck) and retry next pass** rather than returning — returning is the expensive irreversible move. Must stay bounded; freight.go:1059-1076 deliberately avoids an unbounded accept/fail/return loop.

Worth doing alongside: implement `shipping action=active` + `ShippingActiveResponse` and gate on the authoritative `package_in_your_cargo` / `next_step` (confirmed live 2026-07-27, unimplemented in our client) — same TODO already named in [[reference_shipping_no_active_contracts_listing]].

**Why it matters:** returns earn no tier credit. fighter-5 live profile = 4 successes vs **6 returns**. This is what starves the probationary cohort — NOT debt (pool-wide: zero outstanding debt, one default ever).

Related: [[project_smuggling_enablement]], [[reference_freight_orphan_salvage_unpack]], [[reference_v0549_freight_and_percrew_pirates]].
