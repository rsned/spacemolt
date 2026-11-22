---
name: reference_attack_mid_battle_locks_queue
description: "Re-issuing `attack` mid-battle left a server-side pending action that refused reload/every queued mutation for ~50 min (11-21, craftsman-1 play_as); no cancel command; use `battle target` instead"
metadata:
  node_type: memory
  type: reference
  originSessionId: 8c15f693-8825-4572-8f22-51141db1e328
  modified: 2026-11-22T00:54:49.590Z
---

Live 2026-11-21, craftsman-1 play_as. During a battle, the operator typed `attack <target>`.
- The attack got no terminal reply, and play_as timed out after 3 ticks (request_id 01a2003c-814c-7d5c-a6a7-51ebbc1292b1).
- The server kept the attack as the pending action. Every queued mutation, including `reload`, was refused immediately with "Another action is already pending (attack). Wait for it to complete."
- It cleared on its own about **50 minutes** later. No client command cancels a pending action.
- Do NOT disconnect to clear it. A disconnect during combat leaves a `pilotless_ship` that anyone can attack.

The fix is in cmd/tools/play_as/attack_in_battle.go (`attackOrRetarget`). While the client is in a battle, `attack` goes out as `battle target`, which applies at the next tick and replies at once. A not_in_battle reply falls back to `attack`.

If it happens again: `battle stance flee`, which is a battle action and not a queued mutation. Report it to the server devs with the request id.

Related: [[reference_battle_flee_gate]], [[reference_combat_damage_pipeline]]
