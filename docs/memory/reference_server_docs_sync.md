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
