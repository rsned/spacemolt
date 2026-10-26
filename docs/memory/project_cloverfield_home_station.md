---
name: project_cloverfield_home_station
description: "CRFT's own player station Conjunction Junction in Cloverfield (founded 2026-10-25) — record set, recorder flags, budget rules, and what the docs got wrong"
metadata:
  node_type: memory
  type: project
  originSessionId: 90ad50cf-9c6a-4f6f-a1d4-f3dc05c8ebcb
  modified: 2026-10-25T22:26:02.158Z
---

**Conjunction Junction** — CRFT player station in `cloverfield` (lawless crossroads
Haven↔Outer Rim). Founded 2026-10-25 13:49 PDT, tick 2232761, by craftsman-1 (the
user's MANUAL account, never in a fleet) with `build_base --name "..."`, fee 5,000,000.
base_id `c976fa7df53c87f9616165d7d411b41a`, poi_id `862e07fa76571d0a76cab3cbaf4046e7`
(station got its OWN new POI; founder auto-docked).

**Why:** a home of our own first (stop paying rent across NPC stations), growth from
crossroads traffic second, defenses required. Also a charting expedition: findings
feed a KB station guide (~/spacemolt/kb) and later a SCADA/HMI station console.

**How to apply:**
- Spec `docs/superpowers/specs/2026-10-25-cloverfield-home-station-design.md`; plan
  `docs/superpowers/plans/2026-10-25-cloverfield-home-station.md` (Tasks 1-4 code done;
  5-9 are by-hand ops with the user).
- Record set `data/stations/cloverfield/` — run play_as with
  `--record-dir data/stations/cloverfield`; `station_snapshot`; `ledger <cat> <item|credits> <qty>`.
  `findings.md` is the guide's source — tag every fact measured/doc-quoted + date.
- Budget (user): CRFT treasury floor 1M; consumable purchases ≤1M total; EVERY facility
  build planned + user-approved first (credits + materials); the_banker tops up.
- User's top fear: facilities going offline from missing maintenance inputs — stock
  each facility's inputs BEFORE building it.
- Fresh station: 500k FACTION storage included (player-reported, LT1428), but 0 power /
  0 life support and NO personal storage (`view_storage` → no_storage). Reported best
  build order: locker bay → faction workshop → atmospheric recycler → then fuel production.
- Don't `set_home_base` until checked against fleet routing.
- KB guide (2026-10-25): `~/spacemolt/kb` has `cmd/generate-guides` + `pkg/stationplan` +
  `guides/player-stations.md` → `kb/guides/` (catalog-driven build steps, sticky upkeep sidebar),
  merged to KB LOCAL main 74cdd577, NOT pushed. KB local main has DIVERGED from origin (24 local
  vs 10 remote, incl. origin 167f8fd4 "Did You Know in the top nav" which touches kbnav.Items) —
  user chose to leave it; resolve before pushing. Guides is linked from kb/index.html only; adding
  it to kbnav.Items needs a full ~14k-page regen (TestGeneratedPagesMatchNav).
- Station went CRITICAL after the Locker Bay finished with no flex_polymer stocked; fix =
  faction_deposit_items flex_polymer (CRFT has ~1,940 at GE, craftsman-1 1,164 personal).
