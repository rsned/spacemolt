---
name: reference_creature_scan_bracket_threat_flag
description: "A creature scan's \"[grazer — harmless prey]\" bracket is a two-valued THREAT flag, not the role taxonomy — writing it to wildlife_species.role filed scavengers as grazers and made predators uppercase, so WHERE role='predator' matched nothing"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-17T22:57:48.145Z
---

`scan` on a `crt_` target returns no species/role/danger fields. It packs
them into `username` as **`Name [threat — trait, trait]`** and lists what the
scanner tier unlocked in `revealed_info`. The separator is an **EM DASH**, and
four of the twelve species have a hyphen in the display name, so splitting on
`-` truncates them.

## ⭐🔴 The bracket token is NOT the role
It takes exactly **two** values and is a prey/predator threat flag:

- `[grazer — harmless prey]`
- `[PREDATOR — hunts ships]`

The server files **scavengers under "grazer"**: Carrion-Moth and Ash-Scarab are
`role=scavenger` in the `survey_system` census but scan as `[grazer — ...]`.

`CaptureWildlifeScan` wrote that token into `wildlife_species.role`. Two live
corruptions resulted, both found 2026-08-17:
- `carrion_moth` stored as **grazer** — a scavenger refiled as prey.
- `rainbow_leviathan` and `crusher_mantis` stored as **`PREDATOR`** (uppercase),
  so `WHERE role = 'predator'` matched **neither of the only two species that
  can kill you**. Silent filter failure in exactly the query a hunter wants.

**FIXED `35ae5f4b`**: the field is now `CreatureScan.ThreatClass`, the scan no
longer writes `role` at all, and the upsert's CASE-on-non-empty leaves the
census taxonomy standing. Live rows repaired in place. Regression test verified
red by reinstating the assignment.

## Hulls only a scan can give
The census reports no hull, so `max_hull` stays 0 until someone scans. `max_hull`
merges with SQL `MAX()`, so an observed hull is safe as a **lower bound** — a
damaged creature just fails to raise it.

| species | max_hull | threat |
|---|---|---|
| **rainbow_leviathan** | **2200** | hunts ships |
| **crusher_mantis** | **320** | hunts ships |
| cauldronback | 120 | harmless |
| coronid | 100 | harmless |
| slag_tortoise | 90 | harmless |
| inkwyrm | 65 | harmless |
| belt_grazer / patina_grazer / soot_grazer / glitterback_crab | 60 | harmless |
| ash_scarab | 45 | *never scanned* |
| carrion_moth | 40 | harmless |

**A Rainbow Leviathan is 18× the toughest grazer.** That is what killed
salvager-7's shard 20 seconds after arrival at `the_gold_crest`
[[project_action_log_capture]] · [[reference_belt_grazer_hunting_grounds]].

`ash_scarab` is the one species still unscanned — its scan returned no traits,
and `CaptureWildlifeScan` deliberately declines to stamp `danger_scanned_utc` in
that case so it stays on the work list.
