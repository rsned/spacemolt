---
name: reference_server_api_history
description: "Settled Spacemolt server-API changes already absorbed into the client — craft batch sizing, notification ticks, faction events, request_id rollout"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 7da8be40-9f9f-4a30-8440-d70ba696b4ee
  modified: 2026-07-27T02:32:54.199Z
---

Server-API changes that are fully handled client-side. No trap, no pending work — kept only so a future "was this ever handled?" resolves without re-reading the diff.

**Why:** these sat in MEMORY.md's "Server API Updates" section next to live traps, diluting it. Live traps (wire shapes, decode quirks, migration hazards) stayed in the index; settled absorptions moved here.
**How to apply:** if a server behaviour here looks unhandled in the code, that's a regression, not new work.

- **Craft batch size is skill-based** — `MaxCraftBatchSize(state)` replaced the hardcoded 10. → [[project_craft_batch_skill]]
- **`get_notifications` for tick tracking** — lightweight tick refresh after login and inside the runner loop. → [[project_notifications_tick]]
- **Faction events** — promote + invite are handled; demote is still pending server-side. → [[project_faction_events]]
- **`request_id` rollout** — ALL faction commands carry it. API-currentness phase 1 DONE. → [[project_request_id_rollout]] [[project_api_currentness_round]] [[project_api_struct_drift_audit]]
