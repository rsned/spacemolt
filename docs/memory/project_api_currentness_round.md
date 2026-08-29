---
name: project_api_currentness_round
description: Status of the API + patch currentness round (v0.322.1) — 3 workstreams
metadata: 
  node_type: memory
  type: project
  originSessionId: ca64e2a6-eb83-4db2-b98e-60306d83990c
---

A round of API/patch currentness work against server spec **v0.322.1**
(`server_docs/openapi.json`), kicked off 2026-05-23. Three workstreams chosen:

1. **19 new actions** — DONE (branch `feat/api-currentness-phase1-new-actions`).
   Plan: `docs/superpowers/plans/2026-05-23-api-currentness-phase1-new-actions.md`.
   Added drones (get_drone, get_drones, load/unload/recall/upload_drone_script),
   factions (faction_accept_invite, faction_withdraw_invite, faction_remove_enemy),
   empire/citizenship (citizenship, get_empire_info, petition), economy
   (get_tax_estimate, view_insurance, scrap_ship), missions/notes/log
   (completed_missions, delete_note, captains_log_delete, agentlogs). Each got a
   serverapi struct, interface method, WS + MCP client methods, and
   actionResponseTypes monitor entry. agentlogs reuses MessageResponse.
   Mock fix needed — see [[feedback_gameclient_interface_mocks]].
2. **Field-drift audit** — DONE (branch `feat/api-currentness-phase2-field-drift`).
   Plan: `docs/superpowers/plans/2026-05-23-api-currentness-phase2-field-drift.md`.
   All 25 candidates were genuine drift. Three kinds: ack-frame (dock/jump/mine
   gained command+pending; self_destruct remapped to PendingActionResponse);
   wrong-nesting FLATTENED (read_note → flat note_id/title/content/created_by/
   created_at/updated_at/value — note `created_by`, NOT author_id/author_name;
   complete_mission → flat credits_earned/items_received/skill_xp_gained/
   chain_next/community_*); plus 19 plain field adds. Note/MissionRewards types
   kept (used elsewhere); both restructured structs had no external consumers.
   Possible REVERSE drift noticed (out of scope): view_storage struct has
   `credits` not in spec; get_base struct has market/poi/resources not in spec.
3. **Monitor-map gaps (55)** — DONE (branch `feat/api-currentness-phase3-monitor-gaps`).
   Plan: `docs/superpowers/plans/2026-05-23-api-currentness-phase3-monitor-gaps.md`.
   Registered all 55: 5 existing structs + 8 reusing MessageResponse (message-only
   or empty result: claim/leave_faction/logout/trade_cancel/trade_decline/
   faction_deposit_items/faction_withdraw_items/session) + 42 new structs (missions/
   log, ship/wreck, commission/forum/agents, faction roles/rooms, faction intel,
   faction orders). `attack` got command/pending ack fields like dock/jump/mine.
   Verified ZERO remaining monitor gaps (every spec path action now mapped).

ROUND COMPLETE — all three workstreams done; client current with spec v0.322.1.
Action names = openapi PATH (not operationId; a few operationIds are camelCase
like agentLogs/createSession while runtime action is agentlogs/session).

Terminator rule: use the `x-is-mutation` flag — mutations use
WithTimeout(SleepTick*3)/updateStateFromResult, queries (and unflagged-but-
mutating actions) use WithAckOnly()/cacheResultAs.
