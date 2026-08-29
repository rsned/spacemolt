---
name: project_play_as_smart_battle_handler
description: Deferred feature — make play_as battle handler auto-retreat/warn instead of dying passively
metadata: 
  node_type: memory
  type: project
  originSessionId: ca64e2a6-eb83-4db2-b98e-60306d83990c
---

Deferred to a future session (idea raised 2026-05-25): make the `play_as` battle
handling smarter so the pilot doesn't passively die. Concretely:
- Auto-issue `battle retreat` (or stance change) when own hull drops below a
  threshold, rather than waiting for the user.
- Warn/notify when a `battle_update` shows `auto_pilot` flipped back to `true`
  (the ship re-engaged after a retreat) or when own `your_zone` isn't moving
  outward while attackers close to `engaged`.

**Why:** The client-side combat fixes this session (battle event handling +
`battle retreat` no longer timing out) are done and validated in production, but
retreat is multi-tick: a real battle showed retreat accepted yet the pilot still
died because disengaging took ticks and incoming damage (51 then 90) outpaced
zone movement. That loss is server-side combat mechanics, not a client bug — so
the value-add is proactive client behavior, not a fix.

**How to apply:** Build on the battle push-event handlers added in `pkg/game`
(`battle_started`/`battle_update`/`battle_damage`, commit 37db812) — they expose
hull/shield pct, `your_zone`, `your_stance`, and `auto_pilot`. The `play_as`
battle command lives in `cmd/tools/play_as` and calls `client.Battle(...)`
(`pkg/game/client_commands.go`). This is a feature, not a bug; confirm scope with
the user before building. Related: [[project_llm_rollout]] (agents may want the
same logic).
