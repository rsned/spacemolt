---
name: v2 API migration blockers
description: Known v2 API gaps filed with the server team that block migrating client usage from v1 to v2
type: project
originSessionId: 8274e202-ffe0-430a-a56a-0e8bcfc03ea5
---
The client currently consumes the v1 OpenAPI spec (`server_docs/openapi.json`).
A v2 spec exists (`server_docs/openapi.v2.json`) but has gaps that block migration.

**Why:** Bugs were filed with the server developers as of 2026-05-04. Migration
should not begin until those are resolved (or we have confirmation the
behaviour is intentional and we plan around it).

**How to apply:** Before scheduling/starting v2 migration work, re-run the
v1↔v2 diff and confirm these are addressed. Don't migrate piecemeal — the
storage consolidation in particular is fine, but the missing endpoints below
break workflows.

## Bugs filed (2026-05-04)

1. **Duplicate request-body shapes in v2.** Of 201 v2 endpoints, only 18 distinct
   request-body schemas exist. The generator emits one merged "union of all
   params" schema per plugin instead of per command (e.g.
   `spacemolt_faction_admin/promote` and `.../post_mission` advertise identical
   `{id, text}` bodies with descriptions that cross-reference unrelated verbs).

2. **v1 commands with no v2 counterpart** (after accounting for the storage
   consolidation):
   - `agentlogs`
   - `catalog` — v1 dispatcher; only `ships` survived as `spacemolt_catalog/ships`.
     Other catalog actions (modules, items, etc.) are unreachable in v2.
   - `claim_insurance` — v2 has `insure`/`policies`/`quote` but no claim flow
   - `faction_remove_ally`, `faction_remove_enemy` — may be folded into
     `set_ally`/`set_enemy` with a clear mode; needs confirmation
   - `get_guide`

## Confirmed-fine v1 → v2 changes (no action needed)

- **Storage consolidation:** v1's `view_storage`, `deposit_items`,
  `withdraw_items`, `view_faction_storage`, `faction_{deposit,withdraw}_{items,credits}`,
  and `send_gift` (9 commands) all collapse into v2's
  `spacemolt_storage/{view,deposit,withdraw}` selected via `source`/`target`
  parameters. Adds `target=faction:TAG` (cross-faction donate) and ship-instance
  `item_id` (carrier loading) as new modes.
- **Faction admin namespace:** v1's `faction_<verb>` admin commands map cleanly
  to v2's `spacemolt_faction_admin/<verb>`.
- **Infra renames:** v1 `help` → per-plugin `GET /api/v2/<plugin>/help`;
  v1 `session` → `POST /api/v2/session`.

## New in v2 (not in v1)

- 5 state-query endpoints under `/api/v2/spacemolt/`: `get_location`,
  `get_player`, `get_queue`, `get_ships`, `get_state`
- `spacemolt_auth/login_token` (token-based login alongside existing `login`)
