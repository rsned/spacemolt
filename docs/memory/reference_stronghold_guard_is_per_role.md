---
name: reference_stronghold_guard_is_per_role
description: Stronghold avoidance is re-implemented in 5 roles and ABSENT from 5 others including assist and autopilot — the check lives above the movement layer, so every new role starts unprotected
metadata:
  type: reference
---

**Why we keep writing stronghold-avoidance code** (operator, 2026-08-29: "several
times over the past couple weeks"): the guard lives in the CALLERS, not in the
movement layer, so each role must remember it and re-implement it.

**Five roles implement it independently** — each builds its own
`map[string]bool` and hand-threads it down its call chain:

| role | mentions | standing-aware |
|---|---|---|
| `haul.go` (`buildStrongholdRefs`, `filterStrongholdRoutes`, `dropStrongholdOpps`) | 60 | **YES** |
| `mission.go` + `mission_explore.go` (`missionRouteClear`, `missionStrongholdHop`) | 42 | no |
| `shuttle.go` (`strongholdByID` threaded through ranking) | 25 | no |
| `mine_qty.go` (`strongholdBlocked`, `mineStrongholdRefs`) | 17 | error text only |
| `play_as/arbitrage_cmd.go` (`strongholdKeys`) | 13 | no |

The comments admit the copying: mission.go:1353 "modeled on haul.go's
filterStrongholdRoutes"; mission.go:575 "Mirrors Haul's protections."

**🔴 Five roles have NO guard at all — zero mentions each:**
`assist.go`, `autopilot.go`, `hunt.go`, `explore.go`, `freight.go`.

- **`assist.go` having none is how assist-sol lost a 1,500-fuel Capacity Tanker
  to combat at `algol` (is_stronghold=1, police_level 0) on 2026-08-15** —
  see [[reference_assist_fleet_is_dry]].
- **`autopilot.go` having none is the real diagnosis.** Autopilot is the shared
  movement layer every role flies through. Because the check sits ABOVE it,
  every new role starts unprotected by default.

**Only haul.go gets the RULE right.** The correct rule is per-agent, not
blanket: a pirate-LOCKED agent (baseline <= 0) must not route to or through
`is_stronghold`; an UNLOCKED agent may. haul.go:762 "an agent holding the pirate
unlock may work stronghold routes". The four blanket bans forced a hand-carved
exception at `mission_select.go:407` ("Used only to permit a stronghold
DESTINATION"), because the pirate-unlock giver LIVES at a stronghold.

**Loss evidence:** 8 of 24 destroyed ships died at strongholds (algol, zaniah),
all plausibly pirate-locked at the time. We store `systems.is_stronghold` for
all 505 systems and `agent_standings.baseline` for all 161 agents — the check is
a join we never make.

**⚠️ `agent_standings` is a SNAPSHOT with no history** — it is upserted per
(player_id, faction), so you CANNOT ask "was this agent locked on 08-15". Four
stronghold victims read baseline 10 today only because they graduated later.
Do not correlate standings against past events without checking campaign dates.

**FIX (not built):** one gate in the movement layer taking the agent's pirate
baseline plus the route, replacing five copies and covering the five roles with
none. haul.go already holds the correct logic — lift it out, do not reinvent.
