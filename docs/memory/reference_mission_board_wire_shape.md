---
name: reference_mission_board_wire_shape
description: Mission board/active-mission wire shapes — NO requirements block ever; objectives-based; type vocabularies; procurement jackpots
metadata: 
  node_type: memory
  type: reference
  originSessionId: 051adb3a-a06c-4dac-aed0-f51137d16814
---

**The server NEVER sends `requirements` on board entries or active missions** (openapi `additionalProperties:false` forbids the key). `serverapi.MissionRequirements` / `MissionBoardEntry.Requirements` / `ActiveMission.Requirements` are dead legacy fields — any code filtering on them rejects 100% of real missions (2026-07-16 live-smoke bug). Deliver details live in `objectives[]`.

- **Board objective** (serverapi.MissionObjective): `type`, `item_id`, `quantity`, `system_id`, `target_base_id`. **Active objective** (serverapi.ActiveMissionObjective): `target_base` (NOT target_base_id!), `required`/`current`/`in_cargo`/`completed`.
- **Mission-level `type`** = category: delivery, smuggling, mining, combat, exploration, trading, crafting, equipment. **Objective types**: deliver_item, mine_resource, kill_creature, kill_pirate, dock_at_base, visit_system, buy_item, sell_item, craft_item, sell_wreck, traverse_wormhole.
- **Smuggling missions use deliver_item objectives too, with NO warnings and often provided contraband** — only the mission-level type allowlist excludes them.
- **Smuggling-flavored TITLES on plain deliveries are a red herring** (2026-07-17 live): the game names ordinary `type=delivery` missions "Unmarked Cargo", "Perfectly Legal Goods", "Smuggler's Circuit" etc. — all real deliveries of tradeable items (flex_polymer/power_battery/silver_wiring), NOT smuggling. The `type` field is authoritative; the title/mission_id is not. Don't panic at a scary completion title in the logs — check mission_results.mission_type. Real smuggling = `type=smuggling` + `smuggling_courier_*`/`smuggling_black_*` ids (correctly gated out).
- Multi-leg chains exist (First Links: 2 deliver_item objectives); compounds too (deliver_item+visit_system). v1 mission-runner accepts only single-leg type=delivery.
- `expires_in_ticks: 0` = never expires (most hand-authored); procedural ones are finite (smuggling ~159–1059, procurement ~8611 ≈ 24h).
- **Procurement ("Shipyard Supply") missions are jackpots**: e.g. gold_bar ×8000 → 6.62M cr at grand_exchange. Need multi-trip partial delivery (actives track current/required — server supports partials) → v2 opportunity; v1 cargo gate skips them.
- Trade Runs (type=trading) use `sell_item` objectives with credits reward 0 — profit is the sale itself.
- Dump tool: `go run ./cmd/debug/dump-missions --agent <id>` prints raw get_missions + get_active_missions JSON. Related: [[project_idle_agent_income_paths]]

## ⭐ Distress missions auto-accept, consume slots, and are FREE to abandon

MAYDAY broadcasts create `distress_response` missions on your agent **without
any accept_mission call** — pirate-6 was found holding three it never asked
for, at `Active Missions (4/5)`. They pay piloting XP only, no credits, and
expire in ~3h.

They occupy the **5-slot active-mission limit**, so a full queue of them can
block a worker from accepting the mission it actually wants.

**The fix is free (operator, 2026-08-09): `abandon_mission <id>` on a distress
mission has NO cost and NO penalty, and works from anywhere — no dock, no
travel.** So slot pressure from distress calls is a nuisance, not a trap: any
role can clear them inline. Worth doing in the mission/hunt pass before reading
a board, since the alternative is silently failing to accept.

Read the queue with `get_active_missions`, which also needs no dock (the BOARD
commands `get_missions`/`missions` do).
