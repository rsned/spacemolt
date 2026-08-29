---
name: reference_pin_arrival_check_four_directions
description: docked_at_base is EMPTY while docked far more often than it looks — a pin-arrival check must resolve base↔POI in both directions and never early-return on the empty value
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T05:47:42.544Z
---

A pinned worker decides "am I there?" by comparing its pin against live state. Two
independent traps make the naive check wrong, and both were paid for on 2026-08-12.

**Trap 1 — two id spaces.** A pin may be written as a BASE id or a POI id, and
thirteen stations differ between the two. Three have no textual relationship at
all (POI `the_core` → base `central_nexus`), so no string rule can bridge them.

**Trap 2 — `docked_at_base` is routinely EMPTY while docked.** Six marketbots sat
docked at their strongholds logging `returning to pinned station X (at "")` on a
loop. Any check that early-returns on the empty value skips everything after it.
See also [[reference_docked_at_base_gotcha]].

Combined, they sent a worker travelling to the station it was standing on, once
per dry pass, a jump of fuel each time — 164 loops took alhena's 130-unit tank to
ZERO, in a stronghold whose fuel desk is empty.

**The check that actually works** (`atPinnedStation`, pkg/worker/mission.go) runs
all four, none skipped for an empty field:

1. `docked_at_base == pin`                     (base ↔ base)
2. `current_poi == pin`                        (POI ↔ POI — needs NO KB row)
3. `GetBaseByPOI(pin).ID == docked_at_base`    (POI pin → base)
4. `GetBase(pin).POIID == current_poi`         (BASE pin → POI)

(2) is the only one that works at a station the `bases` table has never seen —
four of the nine pirate strongholds have never been scanned. (4) is what rescued
the live case, where `current_poi` was the only populated field.

**Diagnostic:** grep a fleet log for `returning to pinned station`. A worker that
is docked and still logs it is in this loop. The `(at "...")` suffix prints
`docked_at_base` — an empty one there is the tell.

Related: [[reference_station_id_aliases]] ·
[[reference_pinned_mission_workers_never_refuel]] (the other half of the strand)
