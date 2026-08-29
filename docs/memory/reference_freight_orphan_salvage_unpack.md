---
name: reference_freight_orphan_salvage_unpack
description: "Playbook to salvage orphaned/defaulted freight packages: a DEFAULTED freight contract's sealed package becomes keepable+unpackable — recover its contents via craft recipe_id=unpack_package, then pay_debt to clear the flat default debt that blocks all new freight acceptance."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-24T09:51:47.745Z
---

**When a freight contract DEFAULTS** (deadline passes, or a mid-chain restart orphans it so it never delivers), the server applies a flat 500cr debt per package AND leaves the sealed package in the carrier's cargo as **keepable + unpackable**. Recover the contents instead of losing them:

**Salvage playbook (proven live 2026-07-24, engineer-3 at Dheneb):**
1. Stop the worker so play_as can log in without a double-connection: `POST http://localhost:8091/api/overmind/fleets/<label>/agents/<id>/remove` (mission-learn label = `mission`). Wait for the worker to go quiet in the overmind log.
2. `go run ./cmd/tools/play_as <id>` then `get_cargo` — read the `package:<hash>` ids and contents (the package name spells out contents + consignor/consignee).
3. Unpack via the **craft** command's package recipe. play_as: `craft --file=<blob.json>` where the blob is a JSON ARRAY of job objects (loadCraftJobs→CraftBulk):
   `[{"recipe_id":"unpack_package","package_id":"<hash>","source":"cargo","target":"storage"}]`
   (bare hash, NOT `package:` prefixed, in package_id). Bulk reports per-package success/failure. **Unpack ACCEPTED ⇒ the contract had defaulted** (an active/in_transit package's unpack is rejected). Every empire station has a T1 Package Logistics Bay (fast, returns the cargo_container); if it routes to "Station Workshop" instead it's slower, CONSUMES the container, and only advances while DOCKED at that base. Contents land in local storage over a few ticks — verify with `craft queue` (empty) + `view_storage`.
4. **Clear the debt** (it sets `debt_blocks_acceptance:true` and blocks ALL new freight accepts — the silent `freight: skipping, N credits of global freight debt remain unpaid` reposition-forever trap): `shipping --action=pay_debt` with NO amount pays the full balance (`ShippingPayDebt` amount≤0 = pay all). Confirm `outstanding_debt:0` / `debts:[]` via `shipping --action=profile`.
5. Re-add to service: `POST .../agents/<id>/readd`.

**Why it matters:** a defaulted package's contents (e.g. 100x Titanium Alloy) can be worth MORE than the 500cr debt, so salvage+pay is net-positive AND unblocks a carrier that was 1 delivery from advancing. The load-confirm + [[project_freight_load_confirm_and_resume]] fixes prevent the orphan in the first place, but this playbook recovers ones that already happened (and any pre-deploy). Related: [[reference_shipping_no_active_contracts_listing]] (own contracts never list, so orphans are invisible to reconcile without the persisted held file), [[project_freight_probation_bootstrap]] (probationary tier is what these carriers are climbing out of), [[reference_craft_action_result_wrapping]] (craft replies are action_result-wrapped).
