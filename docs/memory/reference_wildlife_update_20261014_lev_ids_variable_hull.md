---
name: reference_wildlife_update_20261014_lev_ids_variable_hull
description: "2026-10-14 wildlife update: creature ids are no longer all crt_ (leviathans use lev_) and hull varies per INDIVIDUAL and grows with maturity; the lev_ prefix silently discarded every leviathan scan"
metadata:
  type: reference
---

Two changes landed with the 2026-10-14 wildlife update, both of which broke
assumptions baked into our code.

**1. Creature ids have more than one prefix.** The original fauna are `crt_`
(all 159 rows in `wildlife_kills` historically). The leviathan family uses
**`lev_`** — e.g. `lev_48383fdffaa8222348d32134fdc996b1`, a Young Cloudwhale.
`captureCreatureScan` gated on `strings.HasPrefix(targetID, "crt_")` and
returned before doing anything, so **scanning a leviathan recorded nothing and
reported no error**, leaving explore announcing it as an unscanned creature
class on every later pass. The tell is `wildlife_species.danger = ''` on a
species you know you scanned, because `UnscannedWildlifeSpecies` keys on
`COALESCE(danger_scanned_utc,'') = ''`.

Fixed `e3de3311`: the gate is now whether the id resolves to a creature in the
cached `get_nearby` roster (prefix-agnostic — `speciesForCreature` always did an
exact id match). `creatureIDPrefixes` survives ONLY to decide whether to print
the "run get_nearby first" hint. **Never gate capture on an id prefix again**;
the server adds them without notice.

**2. Hull is per-INDIVIDUAL and grows.** Three Young Cloudwhales at
`yarrowfield_vapor_fields` read 1368 / 1433 / 1408, and the operator confirms
the value climbs until they mature. This is new: the apparent variance in
`wildlife_kills` for older species is all `min_hull = 0`, i.e. capture gaps
(belt_grazer is otherwise fixed at 60).

What this does NOT break: `wildlife_species.max_hull` uses
`MAX(existing, incoming)` so it only ratchets up and converges on the largest
individual ever seen, and **no decision logic reads it** — `hunt.go` uses only
our own ship's hull. It is a field-guide display value.

What it DOES mean:
- `max_hull` now means "largest individual observed so far", not a species
  stat. A juvenile at 1368 is not a 1433-hull target. Do not read it as a
  per-individual expectation.
- `wildlife_species.name` is last-write-wins and now holds a *stage* name
  ("Young Cloudwhale"), so it flip-flops as different individuals are seen. The
  growth-stage vocabulary is UNKNOWN — these were the first ever sighted — so
  do not hardcode stage words. [[reference_creature_scan_bracket_threat_flag]]
- `wildlife_sightings` stores neither hull nor `creature_id`, so per-individual
  numbers were being discarded entirely; only kills kept per-creature hull.

**Temporary collector, `e3de3311`.** `data/wildlife-growth/observations.jsonl`
(gitignored, `--wildlife-growth-dir=` disables) dumps one row per creature per
`get_nearby`, and `wildlife_growth` in play_as renders the per-stage histogram
the eventual writeup wants. It is a FILE not a KB table on purpose: a migration
cannot be edited once applied and the right shape depends on facts only
collection can supply. Buckets key on the creature NAME, which carries the
stage, so a new stage needs no vocabulary and no code change.

**Count individuals, not rows.** The explore loop looks twice per POI on
purpose ([[reference_wildlife_arrival_race]]), so raw observation rows double
every creature — a naive count reports a population twice its real size.

Context: cloudwhale is leviathan-class, 3 individuals, scarce, only at
`yarrowfield_vapor_fields`, never in combat with us, and we have ZERO
leviathan-class kills on record. A scanned mature Rainbow Leviathan is 2200
hull, so 1368 for a juvenile is consistent.
