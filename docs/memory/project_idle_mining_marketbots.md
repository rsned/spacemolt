---
name: project_idle_mining_marketbots
description: Feature — resident marketbots run a short idle-mining loop between hourly data scans for easy XP + ore stockpile; canary measured viable
metadata: 
  node_type: memory
  type: project
  originSessionId: 868ac572-f106-4923-9823-e82622bf66b9
---

**Feature (requested 2026-06-28):** have the ~34 resident marketbots run an idle resource-mining loop in the gaps between their scheduled data updates (kb_update/update_market) to earn easy mining XP + stockpile sellable ore. Loop: `{ undock; travel <belt>; loop -f 25 mine; travel <station>; dock; deposit; refuel }`.

**CANARY MEASURED 2026-06-29 (play_as `marketbot_algol`, a SPARE not in the running fleet, at Deep Range — safe; flying a STARTER Cobble + one `mining_laser_i`):**
- **Loop time ≈ 10 min 32 s** (undock 01:49:42 → refuel 02:00:14) — comfortably under the 15-min target. ~63 ticks total; 25 are mining (~4 min), the rest is undock + two travel legs + dock/deposit/refuel → **travel legs are the dominant overhead**, not the mining.
- **Fill: 70/75 cargo (93%)** even on a tier-1 laser — `-f 25` nearly fills a 75-cargo Cobble, so 25 cycles is well-calibrated (no need to raise it for a starter hold). Haul (exact) = 58 units / 70 cargo across 4 ores: carbon_ore ×24, tungsten_ore ×12, vanadium_ore ×16, **platinum_ore ×6** (platinum = high-value mineral → run is genuinely worth the ~10.5 min even on a tier-1 laser).
- **Fuel: 14 units / 28 cr per run** — negligible.
- **XP per loop (measured): +2 mining, +2 deep_core_mining, +1 piloting** — a steady trickle (~5 XP/run across 3 skills), not a fast grind. Feeds deep_core_mining too, plus a piloting drip from travel. So the ORE STOCKPILE (58 units incl. platinum → credits when sold) is the bigger payoff; XP is a real-but-gradual bonus. Confirms the "easy credits + XP" goal, with credits the heavier lever (⇒ the sell step matters).

**Build implications (fold into the mb batch with [[project_passenger_demand_intel]] + faction-auto-join):**
1. **Gate**: only start a run when the next scheduled scan is > ~15 min away (run ≈10.5 min + buffer); MUST return + dock before update_market (which requires being docked at the station).
2. **Precondition — mining laser**: the 34 residents are scout-fitted and (per KB module data) have NO mining laser; algol happened to have one. Fleet-wide idle mining needs a `mining_laser_i` fitted on each (costs a utility slot) at deploy.
3. **In-system belt** required (321 systems have asteroid POIs → most residents qualify; skip those without one).
4. **Credits**: the loop `deposit`s to storage (XP + stockpile only). For actual CREDITS add a `sell` step / periodic sell of the ore stockpile.

**Status:** canary DONE & POSITIVE 2026-06-29; feature NOT BUILT. Batches into the one mb redeploy with faction-auto-join + passenger-survey (mb restart held until then, per user 2026-06-29). [[project_treasury_and_shuttle]] for fleet context; [[reference_overmind_launch_commands]] for the relaunch.
