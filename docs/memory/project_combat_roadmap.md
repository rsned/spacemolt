---
name: project_combat_roadmap
description: "Combat is entered via wildlife/First Hunt, not PvP sparring — and the sparring testbed is already BUILT but dormant and missing from the shipped index"
metadata: 
  node_type: memory
  type: project
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-08T18:03:02.022Z
---

**Decision 2026-08-08: wildlife is the starting point for combat, not sparring.**
The operator's call — wildlife was a game addition made AFTER the sparring testbed
was designed, and it is the better on-ramp.

Why wildlife wins as the entry point:
- **Server-taught curriculum.** First Hunt grades the difficulty for you
  (belt-grazers → slag-tortoises → sift-rays → pilot-whale), then Pirate Bounty
  unlocks at weapons level 1. Two of your own agents fighting gives no reference
  for what "good" looks like. [[reference_v0536_wildlife_combat]]
- **No pairing.** No rendezvous, no coordination, no risk of the fleet destroying
  its own hulls.
- **Existing machinery.** It is a new mission category in the mission-learn pool,
  not a new fleet.

**⭐ THE SPARRING TESTBED IS ALREADY BUILT — and was missing from
[[reference_shipped_history]], which is why it went unfound for months.**
`cmd/tools/spar/main.go` + `pkg/spar/` (1,196 lines incl. tests), shipped
2026-05-25 (`ab74272`, `796b23b`, `30a7d91`), still compiles and its tests pass as
of 2026-08-08. Four bot policies: aggressor, skirmisher, retreater, dummy. Modes
`botvbot` and `partner` (a human takes one slot via play_as). Run it:

    go run ./cmd/tools/spar --arena ross_128 --policy fighter-1=aggressor,fighter-2=dummy

Flags: `--arena` (REQUIRED, no auto-discovery), `--policy`, `--aggressor`,
`--rendezvous` (default asteroid_belt), `--max-ticks` (60), `--no-equip`,
`--weapon`/`--shield` (auto-fits cheap modules). Spec:
`docs/superpowers/specs/2026-05-25-sparring-testbed-design.md`, plan
`docs/superpowers/plans/2026-05-25-sparring-testbed.md`.

**How to apply when combat work resumes:**
- The spar tool **predates v0.536 wildlife, `get_battle_log`, and the entire tackle
  layer** — it neither exploits nor accounts for warp scramblers, webs, or EM
  disruption. Verify against the live server before trusting it.
- It **logs in as the agents itself**, so it cannot become a fleet role as-is: a
  worker already owns that connection. `pkg/spar` policy/match logic is reusable;
  `cmd/tools/spar` orchestration is not. Dynamic fleet membership is already built
  ([[project_fleet_pool_dynamic_membership]]), so enter/leave is NOT the hard part.
- **`get_battle_log` did not exist when this was written** and is the higher-resolution
  signal: per-tick hit/crit rolls, resist %, damage breakdown, for ANY battle
  including other players'. Wire it in before extending the testbed.
- **Whether PvP sparring grants weapons XP was never established** — the spec never
  mentions XP. Do not assume it.

Related: [[project_pirate_bands]] (the eventual consumer) ·
[[project_play_as_smart_battle_handler]] · [[project_battle_visualization]]
