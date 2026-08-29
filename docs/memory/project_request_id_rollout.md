---
name: project_request_id_rollout
description: Long-term goal — convert all client commands from fire-and-forget send to Submit (request_id correlation)
metadata: 
  node_type: memory
  type: project
  originSessionId: 08a5ff18-24e1-4fed-b7eb-e7a5bf60f099
---

User wants **every** client command to eventually use `c.Submit` (request_id correlation + response tracking) instead of fire-and-forget `c.send`, so replies get matched to requests reliably.

**All faction commands in `pkg/game/client_commands.go` are converted as of 2026-05-22.** None use `c.send` anymore.

**CRITICAL — use the OpenAPI `x-is-mutation` flag to pick the terminator, NOT semantic guessing.** Many state-changing faction ops (faction_edit, faction_*_role, faction_write/delete_room, faction_decline_invite) are `x-is-mutation=false` — the server returns `type=ok` immediately, no pending→action_result. Using the default terminator on them would wait for an action_result that never comes → 30s timeout. (I made exactly this mistake on FactionDeclineInvite first, then fixed it.)

Conversion pattern keyed off `x-is-mutation` (check via `server_docs/openapi.*.json` operationId):
- **x-is-mutation=false** (returns type=ok immediately — includes all the "query" reads AND the faction edit/role/room writes): `c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))` then `h.Result(ctx)`. Models: `FactionInfo`, `ViewOrders`.
- **x-is-mutation=true** (tick-gated: pending ack → action_result): `c.Submit(ctx, msg, WithTimeout(SleepTick*3))` then `h.Result(ctx)`. Models: `FactionInvite`, `FactionKick`, `craft`.

Note: request_id correlation is about *waiting for / matching* the reply — it does NOT change `storeRawJSON` keying (still content/action-based), so play_as raw-JSON lookup still needs a storeKey per command.

Next batches: non-faction commands still on `c.send` elsewhere in client_commands.go and other files (e.g. RawCommand, and assorted social/forum/mail/etc.). Same x-is-mutation rule applies.
