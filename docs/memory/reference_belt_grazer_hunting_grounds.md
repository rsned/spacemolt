---
name: reference_belt_grazer_hunting_grounds
description: "Where Belt-Grazers actually spawn near First Step, and the manual play_as recipe for killing them (the `hunt` command lives only on feat/wildlife-hunt)"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-10T03:47:02.223Z
---

**Species matters and most belts do not have the one you need.**
`first_hunt_belt_grazers` counts **Belt-Grazers only** — not Slag-Tortoises,
Patina-Grazers, Glitterback Crabs or Aeonbears, which share the same belts.
Surveyed live 2026-08-09 from `first_step`:

| belt | jumps | Belt-Grazers |
|---|---:|---|
| `colony_debris_field` (first_step) | 0 | 0 — slag-tortoises + a Crusher-Mantis |
| `markeb_belt` | 1 | 0 — one Geiger-Hound |
| `void_gate_mineral_fields` | 1 | 0 (no creatures at all) |
| `starfall_belt` | 2 | 0 (no creatures at all) |
| `beid_belt` | 2 | 1 |
| **`kochab_belt`** | **2** | **5, observed respawning mid-session** |
| **`ironpeak_belt`** | **2** | **5** |

`kochab_belt` and `ironpeak_belt` are the grounds. Khambalia (where pirate-6
hunted) is **10 jumps** out and unnecessary. `void_gate_outpost` is the nearest
station for both (1 jump from kochab, 2 from ironpeak) — that is where to dock
and `complete_mission`.

**⭐ `hunt` does not exist in `main`'s play_as** — it is on `feat/wildlife-hunt`
only. Build from the existing worktree
(`.claude/worktrees/feat+wildlife-hunt`) rather than switching main's tree,
which carries the dirty `data/*.json` churn: `go build -o bin/play_as_hunt
./cmd/tools/play_as`.

**The manual loop**, per kill (Prospector + `pulse_laser_i`, damage 10, reach 3):
`get_nearby` for `crt_…` ids → `hunt <crt_id>` → `battle advance` until
`zone_distance` ≤ `combat_state.max_weapon_reach`. Opens at distance 6, so ~4
advances to close and ~7 more to kill a 60-hull grazer — about **19 ticks end to
end**. Cost per kill: 120 damage dealt, 40 taken, **hull never left 100%** on any
of four agents (shields absorbed it all).

**⭐ Two traps that make a run look like it failed when it did not:**
1. **Combat resolves server-side after the client disconnects.** A session that
   ends mid-fight still gets the kill. The mission counter read at the end of a
   session is a FLOOR, not a verdict — re-read it in a fresh session before
   concluding anything.
2. **`hunt` on a new target while the previous battle is still resolving can
   silently abandon that fight.** pirate-9 lost a kill this way (target left
   alive at 23/60); pirate-7's identical batched script got all three because
   its fights happened to finish first. Non-deterministic. **One target per
   pass**, or budget 20+ advances so the fight certainly ends.

Related: [[reference_v0536_wildlife_combat]] · [[project_pirate_bands]] ·
[[reference_battle_replay_viewer]] (every fight gets a `battle_id`).
Payout on completion is the treasury lottery, not a hunting problem —
[[project_empire_treasury_payout_collapse]].
