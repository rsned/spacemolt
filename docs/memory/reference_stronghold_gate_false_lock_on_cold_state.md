---
name: reference_stronghold_gate_false_lock_on_cold_state
description: "The stronghold gate reads live standings and fails CLOSED, so an autopilot issued as the first command after login is refused with 'complete the pirate unlock' even when the agent holds it — seed get_status first"
metadata:
  type: reference
---

**An `autopilot` issued as the very first command after login can be refused
for a pirate unlock the agent already has.**

The movement-layer gate resolves blocked systems through
`strongholdRefsForRoute` (`pkg/worker/stronghold_gate.go:110`) →
`strongholdRefsFor` (`pkg/worker/haul.go:216`) → `smugglingUnlocked`
(`pkg/worker/mission_standing.go:49`), which returns false when
`st.Player.Standings` is nil or empty. On a cold session the player payload
has not arrived yet, so the map is empty, the agent reads as LOCKED, and
autopilot refuses before departing.

**Observed 2026-10-13.** craftsman-1 at `caph_disk`, routing to
`alhena`/`voss_redoubt`:

```
   NOT DEPARTING: route enters pirate stronghold(s): Alhena
❌ Error: route passes through a pirate stronghold: route to alhena enters
   Alhena (complete the pirate unlock to fly it)
```

Its standings at that moment were `pirate_voss 40 (base 10)` and **base 10 on
all nine pirate keys**, against `pirateUnlockBaseline = 10` — fully unlocked.
Re-running the identical autopilot with `get_status` seeded ahead of it in the
same session departed normally.

**Seen again 2026-11-06** on craftsman-1 routing to Barnard 44, after a server restart.

**Fixed 2026-11-06:** `strongholdRefsForRoute` now calls `get_status` once when
standings are empty, and still blocks if they stay unread. A play_as session or
worker started before that commit needs a restart to get it; until then send
`get_status` before `autopilot`.

A refusal naming the unlock is NOT evidence the agent lacks it: check `base N`
on the `pirate_*` keys first. Keep the gate failing closed: guessing unlocked
costs the ship, guessing locked costs one skipped route.

Note the unlock raises the BASELINE to 10 on all nine keys; the current
standing (40 for voss here) is separate and is not what the gate reads.

[[feedback_stronghold_routing_requires_pirate_unlock]]
[[reference_stronghold_guard_is_per_role]]
[[reference_pirates_standing_key_drift]]
