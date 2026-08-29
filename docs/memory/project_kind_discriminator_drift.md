---
name: project_kind_discriminator_drift
description: "DONE 2026-07-20 (`2b45a9d`, pushed): v0.531.4 `kind` discriminator drift + shipping client_api_monitor spam cleanup. 17 structs got Kind; shipping reads registered; list = facility∪shipping union."
metadata: 
  node_type: memory
  type: project
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
  modified: 2026-07-20T19:34:55.997Z
---

**FIXED 2026-07-20, commit `2b45a9d` (pushed).** 17 serverapi structs got `Kind json:"kind,omitempty"` (RecycleResponse = alias of CraftJobQueued so it rode along; bulk order shapes have no client method → no struct, YAGNI; transit variants stay folded into the single GetSystem/GetPOI structs). Monitor: shipping reads `get`/`profile`/`track` registered (mutations are action_result frames the monitor never field-checks — only reads needed); new `actionExtraResponseTypes` unions `"list"` = FacilityListResponse ∪ ShippingListResponse because the payload carries no originating command. Tests proven discriminating (removing MineResponse.Kind fails naming "mine"). Mapping was derived from `openapi.20260719.json` via $ref walk — the spec schema NAMES mostly don't match our struct names; map through ACTIONS via actionResponseTypes, not names.

Original context below, kept for the drift-audit trail:

Surfaced 2026-07-19 while smoke-testing shipping (user saw many `[SERVER API CHANGE]` warnings about a new `kind` field).

**What it is:** the v0.531.4 server added a top-level **`kind` discriminator** enum to ~39 response/event schemas (openapi.20260719.json). Our `pkg/game/serverapi` structs (baseline ~v0.495) have no `Kind` field on any of them except shipping's `ShipmentActor`, so `pkg/game/client_api_monitor.go` (which diffs each response's top-level keys against the struct's json tags, keyed on `action`) emits `[SERVER API CHANGE] New fields in "<action>" response not in <Struct>: [kind]` for every one.

**Scope (39 shapes with a `kind` property):** order ops (`CreateBuyOrderResponse`/`CancelOrderResponse`/`ModifyOrderResponse` → `kind:"single"`; bulk variants → `"bulk"`), craft (`CraftJobResponse`"job"/`CraftQueueResponse`"queue"/`CraftQuoteResponse`"quote"/`PackageJobResponse`"package"/`BulkCraftResponse`"bulk_craft"/`JobCancelResponse`"cancel"/`BulkJobCancelResponse`"bulk_cancel"), passengers (`UnloadPassengerResponse`"single"/`UnloadAllPassengersResponse`"all"/`TransferPassengersResponse`"transfer"), facility-type (`detail`/`discovery`/`list`), combat (`AttackNPCResponse`"npc"/`AttackPlayerResponse`"player"/`ActiveBattleParticipantInfo`/`ParticipantSnapshot`), mining (`MiningYieldPayload`/`Notification_mining_yield` → "yield"), `MineFilteredResponse`"filtered", `LoungeCheckInResponse`, `InspectResponse` (enum package|item|module|ship_class|system|poi|base), `ShipmentActor` (player|faction|station — already has Kind), and the **variant-discriminator pairs** `GetSystemResponse`("normal") vs `GetSystemTransitResponse`("transit"), `GetPOIResponse`("normal") vs `GetPOITransitResponse`("transit").

**NOT a behavioral bug.** Verified: our client has a single `GetSystemResponse`/`GetPOIResponse` and reads in-transit from the top-level `in_transit` bool (`client.go:3345`), so the transit variant is already handled generically; `kind` is redundant. Every other case is a pure discriminator the client can ignore. Extra unknown fields don't break decode — only the log spam.

**Why it matters now:** the same monitor also false-alarms on every shipping response (bare-action keying is unaware of the `shipping_<action>` GetRawJSON namespace; `action:"list"` collides with facility). Once Sub-project B runs freight in the fleet, this spams worker logs. See [[project_api_struct_drift_audit]] and the shipping-carrier spec's "Follow-up" section.

**Fix (its own task, user must greenlight):** add `Kind string \`json:"kind,omitempty"\`` to the ~39 affected serverapi structs (or a shared embedded discriminator), and either register the shipping actions in `client_api_monitor.go` or teach it the `shipping_` namespace. Part of the broader v0.495→v0.531 API sync; BuiltForAPIVersion was bumped to v0.531.4 for shipping only, so this `kind` drift is currently unaudited.
