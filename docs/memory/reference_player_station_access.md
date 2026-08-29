---
name: reference_player_station_access
description: "Player stations can refuse an outsider's dock; access is learned, not readable, and a bases row with public_access=1 is the only ahead-of-time signal"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T00:21:12.855Z
---

Player-built stations may refuse a dock (`Error: Access denied`), and nothing in
the wire protocol says which will beforehand. Discovered 2026-08-11 when
johnny_cab booked a 13,125cr passenger for **Fortress Blackthorn**, flew there,
was refused, and circled holding an undeliverable fare until recovered by hand.

**Identifying a player station:** its id is 32-char lowercase hex; NPC stations
carry readable slugs (`treasure_cache_trading_post`). Verified against the whole
KB — 21 hex-id POIs, all `type=station`, zero false positives either way.
Stations are dual-named, so both the base id and the poi id are hex and differ
([[reference_station_id_aliases]]).

**⭐⭐ `GET https://game.spacemolt.com/api/stations` — 75 stations, live and
authoritative** (operator-supplied 2026-08-12). Fields: `id` (base id), `name`,
`type` (station|outpost), `system_id`, `services[]`, `condition`,
`facility_count`, `wrecked`, `faction_id/name` (absent ⇒ NPC). 33 player-faction,
42 NPC — including all nine pirate strongholds (41-70 facilities each, full
service lists).

**IT DOES NOT REPORT ACCESS.** `services` is what a station OFFERS, not who it
ADMITS; access stays owner-gated and unreadable (operator-confirmed). It agreed
with all 12 of the survey's flown verdicts, but that is correlation on a thin
sample, NOT the mechanism — do not promote a station to `open` from it. Seeding
59 stations open on that inference was a real regression, caught and reverted:
guessing closed costs one declined fare, guessing open strands a passenger.

What it DOES settle, safely:
- **presence** — a KB station POI absent from the endpoint is DISMANTLED. Found
  exactly the two ghosts the survey flew ten jumps to discover.
- **`type: outpost` ⇒ members-only faction object**, never a fare destination.
  All 14 publish empty services; the two flown (garnet_post, Ironbelt Range)
  both refused. Structural and permanent — unlike a closed station, it will not
  open later.
- **negatives** — an outpost, a wreck, or a station publishing no services
  cannot serve an outsider whatever its access policy. Only 2 of 61 stations
  publish nothing: Fortress Blackthorn (facility_count 0) and Ashborne Reach
  (condition critical). Both could recover.
- **fuel** — `refuel` in services, 8/8 against the survey including the merely
  depleted desk. A CAPABILITY, contingent on being able to dock.

Seeder: `scratchpad/seed_access.py` (not yet productionised — the worker should
refresh from this endpoint rather than a one-off script).

**⭐ THERE IS NO READABLE ACCESS SIGNAL — the bases-row theory is DEAD (2026-08-12).**
It once looked like one: only 3 of 21 player stations have a `bases` row, and
those three were exactly the three then proven open (Hex Star, The Obsidian
Well, The Veil Anchor — each `public_access=1`, `pirate_rep_required=0`), while
Blackthorn had none. The survey refuted it in one pass: **Ironlight Crossroads,
The Second Ledger, Starlight Cartographer and Private Rain all ADMITTED us and
none of them has a bases row.** A bases row still corroborates open, but its
ABSENCE means nothing. Flying there is the only test.

**Most player stations are OPEN.** Of the six surveyed stations that still
exist, **four admitted and two refused** — the opposite of the assumption behind
"unverified reads as closed". That rule is still right (a stranded passenger
costs far more than a declined fare) but it is expensive by default, which is
what makes the survey worth the fuel.

**How the fleet handles it** (`pkg/worker/station_access.go`, `e16c70fc`):
unverified reads as CLOSED, per operator ruling. Asymmetric on purpose —
guessing wrong costs one declined fare, guessing wrong the other way strands a
passenger nobody can drop. The map at `data/overmind/station-access.json` is
shared fleet-wide, seeded from `assets.ProvenDockedBases()`, and learns only
from conclusive outcomes: a clean dock proves open, `access denied` proves
denied, any other error teaches nothing.

The map also learns **fuel** (`f0ced219`): `fuel` / `no_station_fuel` keys,
taught by `RecordRefuel` from a refuel attempt. `no_fuel_source` (station has no
desk) is matched NARROWLY — never against `no_fuel_cells` / "no fuel cells in
cargo", which is the SHIP being dry and says nothing about the station.
engineer-5's 2026-07-29 strand was the ship-dry one. **Hex Star DOES sell
fuel** (johnny_cab refuelled there 2026-08-12); the dry-desk cases are seven
other player stations.

**⭐ A station can be GONE, and the POI catalogue will not tell you** (`5f5b1bb2`).
`pois` rows refresh only when an agent visits that system, so a dismantled
station in a rarely-flown system reads as current data forever. The 2026-08-12
survey flew 7 jumps to **Veilwatch Shoal** (oakridge) and 3 more to **ENDL
Kitalpha Cache** for `{"code":"invalid_poi","message":"Unknown destination"}` —
and those were the two OLDEST rows in the table (ticks 1,429,376 / 1,468,302 vs
a 1,599,600 clock). **Staleness is not evidence a station is gone; it is
evidence nobody has looked** — which is exactly why the survey went there. Only
flying settles it. `RecordTransit` now banks the answer in a `gone` set (kept
separate from `denied`: refused-us vs not-there-to-refuse-anyone), `Deliverable`
consults it because `open` never expires, and the probe skips known ghosts.

**⭐ THREE fuel states, not two.** A third appeared 2026-08-12 at Ironlight
Crossroads: `station_fuel_empty` — "This station's fuel reserves are depleted.
Buy fuel cells from the market and use them directly." That is a desk that
EXISTS but is currently dry, distinct from `no_fuel_source` (no desk at all) and
`no_fuel_cells` (the SHIP is dry). It is deliberately recorded as **nothing**:
marking `no_station_fuel` would be permanently wrong for a station whose
reserves refill, and marking `fuel` would be dangerously optimistic for a router
planning a long leg on it.

**Gotchas:**
- The skip line logs once per station per PASS (~3.5s), so a fare parked at a
  closed station writes ~1000 lines/hour. Minor against a 259MB log; not fixed.
- **A running worker owns this file.** It loads the map at startup and rewrites
  it on save, so an out-of-band edit gets clobbered — mine was, 6 minutes after
  I made it. Stop the owning fleet before editing by hand.
- Survey tool: `cmd/tools/station-probe` (`f3b54493`, `de40a1b9`) — TSP tour,
  per-leg fuel gate incl. in-system approach cost, pre-departure refuel, leaves
  the ship adrift for a GSA tow rather than reserving return fuel.

Related: [[project_treasury_and_shuttle]] · [[project_passenger_feature]]
