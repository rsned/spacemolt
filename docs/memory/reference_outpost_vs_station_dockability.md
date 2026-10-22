---
name: reference_outpost_vs_station_dockability
description: type=outpost is faction-members-only and UNDOCKABLE for outsiders; 19 systems hold ONLY outposts and are effectively station-less. Migration 65 + stations-refresh now store type/wrecked; the routing GATE is still unbuilt.
metadata:
  node_type: memory
  type: reference
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-10-22T08:07:00.063Z
---

`/api/stations` returns **`type` (`station` | `outpost`)** and **`wrecked`**.
Live counts 2026-10-22: **87 bases — 21 outposts, 66 stations, 2 wrecked.**

## The rule

- **`outpost` = faction members only, explicitly.** All 21 offer **zero
  services**. An outsider's `dock` returns **`Access denied`**, and every
  station-requiring action (`send_gift`, market, refuel) returns **`not_docked`**
  even while the client's own status line shows `⚓Docked`.
- **`station` can be public.** 62 of 66 carry services.
- **`wrecked` is NOT undockable.** It means services are down. Mera Sanctum is
  wrecked with 0 services yet hosts **89 facilities** (incl. one of only two
  public Backwater Stills) and bids 5,000 × 2,868 for pirate_moonshine. It gates
  *service expectations*, never docking or facility use.

Zero services is **not** a proxy for outpost: 25 bases have none — 21 outposts,
2 wrecked, plus Ashborne Reach and Fortress Blackthorn. Only `type` is reliable.

## Two traps

1. **Pick-the-wrong-one.** `lhs_1140` holds BOTH `Ironlight Crossroads`
   (station, poi `0321b3e4…`, 10 services) and `Ironlight Argon Shoal`
   (outpost, poi `029e1bb6…`, 0). Only one system in the galaxy is mixed — and
   it is exactly where we got stuck.
2. **Outpost-only systems.** **19 systems** have no dockable station at all
   (barnards_star, sargas, aspidiske, betelgeuse, fomalhaut, libertas,
   saltwind, sandrift, …). They are **station-less for us** — no fuel, market,
   repair, or alternative — a dead end of the same shape as a stronghold.
   This is the one that will bite **haul**, which roams widely.

## What it cost (2026-10-21)

johnny_cab — a roaming taxi pinned `station: ""`, so `ensure_home` is a no-op —
drifted onto Argon Shoal, cached `⚓Docked`, and failed **every** banker
`send_gift` with `not_docked` for hours while the status file read healthy and
docked. Diagnosed only after `get_poi` on the outpost **ignored the argument**
and returned the POI the ship was standing on (see get_poi retirement).
Recovery = `travel 0321b3e4406021575337b7be26e53dd7` (one hop), then dock.

Do NOT "fix" this by pinning johnny_cab to a home: a taxi must roam. The fix is
the gate.

## SHIPPED `6f5218b1` (2026-10-22)

Migration **65** adds `bases.type` + `bases.wrecked`; `stations-refresh` now
writes both on INSERT **and** UPDATE (it had always *parsed* `Type` and thrown
it away). Applied live: 11 bases inserted, 76 updated.

## STILL OPEN — the gate itself

1. Seed [[reference_station_access_learned_map]]-style `StationAccess` from
   `type='outpost'` so no agent ever attempts the dock. `StationAccess.Seed()`
   already exists. Its header claims *"the server exposes no access flag we can
   read ahead of time"* — **that is now false** and should be corrected.
2. Within a system, never choose an outpost when a station exists.
3. Treat outpost-only systems as station-less for routing.
4. **Put the gate in the MOVEMENT layer, not per role.** Today only
   `shuttle.go` and `station_probe.go` consult `deps.Access`; **haul never
   does** — the same shape as the stronghold guard that got re-written five
   times and went missing from half the fleet.

**Implementation note: key off `bases.poi_id`, NOT a `pois` join.** `pois` is
missing rows for ~10 outposts, so the join reports 9 outpost-only systems when
the truth is 19.

Related: [[reference_station_id_aliases]] — market.db calls it `mera_sanctum`,
the KB calls it `mera_sanctum_station`. Same station, two names.
