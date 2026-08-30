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
