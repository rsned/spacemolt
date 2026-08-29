---
name: project_api_struct_drift_audit
description: DONE (2026-07-08) — API-drift audit across server v0.398→v0.473; command coverage verified+guarded, 2 real breaks fixed; response-struct verification partial (sample-bounded). Findings doc in docs/superpowers/specs.
metadata: 
  node_type: memory
  type: project
  originSessionId: 9d76ccbd-ca44-4de8-904c-982c4f6dc972
---

**DONE 2026-07-08 (branch `feat/api-drift-audit`).** Audit that backs the `BuiltForAPIVersion` v0.473.0 claim (bumped in `af8e099` without verification). Ran subagent-driven via superpowers plan. Full write-up: **`docs/superpowers/specs/2026-07-08-api-drift-audit-findings.md`** (design + plan siblings same date).

**What shipped (commits on feat/api-drift-audit, NOT yet merged/pushed):**
- `1fd0898` — Layer 1: registered 5 handled-but-unmapped commands in `actionResponseTypes` (get_achievements + 4 passenger cmds, new minimal `UnloadPassengerResponse`); added **`pkg/game.TestServerCommandsCoveredByClient`** guardrail (fails when a get_commands.json command is neither typed nor in a justified `ignoredCommands` list) — closes the reverse-direction gap the old `TestLoadFromOpenAPIContainsAllHardcoded` missed.
- `fe2dcdb` — **Break #1 fixed:** `unload_passenger` field is `fare_collected`, not `fare_paid`; client used wrong key in `responses.go`, `client.go` detection (never matched!), and play_as formatter (printed "0 cr"). +test fixtures.
- `c376ba7` — **Break #2 fixed:** `view_orders` has no `faction_orders` array at v0.473; faction orders come back in `orders` when request passes `scope:"faction"` (default is `personal`). Faction collector was silently writing ZERO faction-order rows to KB. Fixed `pkg/faction/collector.go`+`parse_market.go`.
- Task-5 commit — findings doc + honest caveat on `BuiltForAPIVersion` doc comment in `pkg/version/checker.go`.

**Verdict (honest):** command coverage VERIFIED + drift-guarded; payload shapes verified (no breaks); response structs PARTIALLY verified — client-read fields verified for every struct with a live sample in `data/game-api/latest/`, but structs without a sample stay UNVERIFIED (openapi is an incomplete superset, its deltas are NOT reliable drift signal). get_status/get_base phantom fields = dead DTO fields (never unmarshaled), left in place. view_storage.credits = ambiguous, left. ~67 openapi-only deltas + 29 UNVERIFIED structs documented, not touched.

**Follow-up (optional):** capture populated live samples for high-traffic unverified structs (travel/jump/combat/facility) to verify against ground truth vs the openapi superset. See [[project_v2_api_migration]] (v2_* + get_location/get_state/storage are ignore-listed pending that).

Related: [[project_api_currentness_round]] (absorbed), [[reference_server_docs_sync]], [[feedback_gameclient_interface_mocks]].
