---
name: reference_server_docs_sync
description: How server_docs stays in sync with the live server and why actionspace drifts
metadata: 
  node_type: memory
  type: reference
  originSessionId: 032ededf-f27b-43c1-9a5e-b39ff784606f
---

`server_docs/openapi.json`, `api.md`, and `skill.md` are **symlinks** to dated snapshots (e.g. `openapi.20260519.json`). They are regenerated ad-hoc but regularly by `cmd/data/update-server-docs/` to keep in sync with the live server; a docs bump just repoints the three symlinks to a newer date.

`pkg/actionspace` hardcodes the action catalog (`actions.go`, `annotations.go`). `TestLoadFromOpenAPIContainsAllHardcoded` asserts every hardcoded action exists in whatever spec `openapi.json` currently points to. So when a fresh `update-server-docs` run pulls a server API change (renamed/removed command), this test fails — that's the guardrail flagging the hardcoded catalog needs updating, NOT a bug in the test.

Example (2026-05-19 snapshot): server replaced unilateral `faction_set_ally` with the mutual-consent workflow `faction_propose_ally` + `faction_accept_ally` (+ `faction_remove_ally`); `faction_set_enemy` stayed unilateral. Fixed in actionspace, committed together with the symlink bump so spec and catalog agree.

Still-stale client-side references to `faction_set_ally` (deferred, not yet migrated to propose/accept/remove): `pkg/game/client_commands.go`, `pkg/game/mcp_game_client_commands.go`, `pkg/calllog/mutations.go`, `cmd/tools/play_as/main.go`.


**2026-08-29 refresh (`openapi.20260829.json`, server v0.571.0):**
`repair_module` is GONE from the spec (module wear removed) — dropped from
the client surface in `repair_module` removal commit (actionspace, calllog,
api monitor, handleResponse, RepairModuleResponse, play_as). The
hardcoded→OpenAPI test is green again. The REVERSE direction has **31
OpenAPI commands absent from the actionspace catalogue** (`AllActions`):
build_base build_outpost buy_ship_license cancel_ship_buy_order citizenship
deploy_drone dismantle_outpost faction_accept_invite faction_prepay_tax
faction_remove_enemy faction_scan_poi faction_withdraw_invite
forum_create_thread forum_delete_reply forum_delete_thread forum_reply
forum_upvote hunt load_drone load_passenger pay_bounty place_ship_buy_order
prepay_tax recall_drone recycle refit_ship scrap_ship sell_ship_to_order
unload_drone unload_passenger upload_drone_script. Several ARE implemented
in pkg/game (hunt, drones, passengers, faction_accept_invite) but not
catalogued for the LLM agent; `pay_bounty` and the outpost/citizenship
commands are not implemented at all. No test guards this direction. Also
absorbed the same day: `player_kill` push and scan `description` (lore).
`BuiltForAPIVersion` still reads v0.547.1 on purpose — bumping it would
claim v0.548–v0.571 are absorbed.

**2026-08-30 refresh (`openapi.20260830.json`, server v0.572.1):** six new
paths (`claim_prize`, `faction_personnel`, `recruit_personnel`,
`service_prize`, `transfer_personnel`, `treat_personnel`), 21 new schemas,
~40 existing schemas gained fields. Nothing removed, so the
hardcoded→OpenAPI test stayed green; the six commands join the
uncatalogued-in-actionspace list until Layer B lands. See
[[reference_v0572_boarding_personnel]].

**Formatter type auditor (2026-08-30):** `scripts/audit_playas_field_types.py`
diffs every play_as struct field's Go type against the spec kinds for that
property name. Run it after each `update-server-docs` refresh — the failure
class it hunts is a type flip that makes a styled formatter's Unmarshal fail
and silently fall back to raw JSON (wrecks modules `[]string`, v0.572;
`coverage_pct` string-vs-number). Known false positives are play_as-authored
plan structs (sellable/unload) and `get_location`'s string connections.


**2026-08-30 (late): refreshed again at v0.573.1** (server restarted through
v0.572.1-v0.573.1 the same day). Capacitor fields REMOVED from the Ship schema
(v0.572.4) — the day-old TestShipMatchesOpenAPISpec caught it, first real catch.
Absorbed: capacitor fields deleted; GetBaseResponse.Repairs
(StationRepair{Response,Entry,Material}, v0.573.1 repair ledger with material
bills) and GetShipResponse.DroneBay (DroneBayView/DroneBaySlot). openapi.v2.json
refresh got HTTP 429 — retry pending.
