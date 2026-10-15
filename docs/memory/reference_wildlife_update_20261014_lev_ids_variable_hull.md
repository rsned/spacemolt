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

## 10-14 later: the stage vocabulary is REAL, shipped `1caf9bd4`

Three `lev_` species are now sighted and every one varies per individual; all
**35** `crt_` species observed are fixed, so "variable hull" is exactly
"leviathan-class", not "new species".

| species | stage seen | n | hull range |
|---|---|---|---|
| cloudwhale | Young | 6 | 1287-1767 |
| hoarfrost_leviathan | Juvenile | 3 | 669-769 |
| molt_leviathan | Juvenile | 6 | 549-691 |
| rainbow_leviathan | (none — mature) | 0 sighted | 2200 |

Operator-supplied vocabulary: **hatchling, young, calf, juvenile**, in
`pkg/knowledge.ProtectedStageWords`. A stage can LEAD or TRAIL the name, and
matching is whole-word and case-insensitive — a substring match would spare
adults ("Youngling") and mangle names.

- `CanonicalSpeciesName` strips it, and `UpsertWildlifeSpecies` now stores the
  canonical name. This is what fixes the name flap above: the table is one row
  per species, last-write-wins. **Pre-existing rows still hold the qualified
  name** until the next sighting upserts over them.
- `IsProtectedJuvenile` is the same vocabulary as a don't-hunt predicate. It is
  NOT wired into hunt yet — offered, not approved.
- Lifecycle, operator: cloudwhales age and DIE, producing a whalefall and a
  temporary scavenger surge; they do not grow forever. Lobsters grow until the
  food runs out. So a per-stage histogram is the right shape and a single
  growth curve is not.

## 10-15: wildlife FULLY REGENERATES; first leviathan kill

craftsman-1 attacked `lev_29d070abd3f2d9be47df6acc5004e2a8` (Juvenile Molt
Leviathan, 569 hull, oxbow_belt_b), lost the ship, came back and killed it.
First leviathan-class kill on record — 161 kills, exactly 1 leviathan.

The growth collector accidentally captured the whole arc, because it logs
`hull`/`max_hull`/`in_combat` per sighting:

```
08:34:53  569/569          tick 2143625
08:40:25  364/569  COMBAT  tick 2143658   <- the fight that killed us
09:21:59  569/569          tick 2143890   <- healed to FULL
09:23:59  dead
```

**205 HP over 232 ticks ≈ 0.88 HP/tick (~53 HP/min).** Corroborated fleet-wide:
of ~1,500 observations only **3** are below full hull and all 3 are
`in_combat=True`. We have NEVER observed a damaged creature out of combat.

- **Attrition is not a strategy.** You cannot soften a leviathan over several
  passes; every engagement starts from full. Any hunt logic must assume the
  target is at max_hull regardless of history.
- Damage is a GRIND not a burst: molt_leviathan hit 37-41 per hit (tight ±5%),
  but 21 times for 830 total — 3.2x the previous single-battle worst. Survival
  depends on breaking contact, and `loop 20 battle retreat` did NOT break it.
  Predict survivability as hull / 40 = ticks.
- A won battle can leave ZERO `wildlife_attacks` rows: the rematch killed it in
  two `battle advance` with no return fire. That is correct, not a miss — a
  0-hit volley still stores a row (cf. quantum_moth 2 shots/0 hits).
- `wildlife_attacks` was a biased sample until now: 161 kills and zero losses
  meant every row came from a fight we survived.
- Combat totals bug is the MAJORITY case, not occasional: **120 of 161**
  `wildlife_kills` rows have duration_ticks, damage_dealt AND damage_taken all
  zero. The 41 populated rows are the ones to diff to find what writes them.
  [[reference_capture_loss_taxonomy]]
