# Contraband Pipeline — Rollout Runbook (plan 2 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put all 31 miningbots, two haulers and the Voss crafters to work on the moonshine line using the verbs plan 1 shipped, with every later line's inputs stockpiling and every bot routed through the pirate unlock.

**Architecture:** No new code. Everything is fleet yaml, per-agent schedule entries, the secondment ledger and operator play_as sessions. The unlock waves fall out of the existing reconciler's `--max-in-flight` cap; the haul links are `deliver` / `buy_directed` schedule entries on two residents; the sell side is `shopkeep` on pinned marketbots. Each task ends with a verification command whose output is the acceptance test.

**Tech Stack:** data/overmind/*.yaml, data/agents/<id>/schedule.json, bin/overmind, bin/fleet-secondment, cmd/tools/banker, go run ./cmd/tools/play_as, sqlite3 on data/assets.db.

**Spec:** `docs/superpowers/specs/2026-10-24-contraband-production-pipeline-design.md` (D1, D2, D3, D4, D5, D6, D8, D9). Plan 1 (shipped, merged d761593d): `docs/superpowers/plans/2026-10-24-contraband-pipeline-worker-fleet.md`. Verb reference: memory `reference_pipeline_worker_verbs_and_flags`.

## Global Constraints

- Every running overmind launched before 2026-10-24 13:44 is the pre-merge binary and silently drops `pipeline`, `shopkeep`, `faction_pull` on reload. An overmind must be SIGTERMed and relaunched with its exact `ps` arguments before any of those fields is used on its roster. Craft and crft-mining are already current.
- One harvesting module per miningbot (operator rule): gas harvester, ice harvester, or mining laser. Group = fit. Cobble CPU 12: ice = drop `mining_laser_iii`, fit `ice_harvester_ii`; gas = drop `mining_laser_iii` AND `pulse_laser_i`, fit `gas_harvester_iii`; titanium = stock fit. Ice fields refuse the laser outright.
- Never route a locked bot (pirate baseline < 10) into Lawless space. `mine_qty_at` refuses it; the yaml must not rely on that refusal as a scheduler (a refused pass repeats every idle tick).
- A bot's `pipeline:` recipient must be an agent whose storage at the drop station we control (the hauler), never a crafter at a station the bot cannot dock at.
- An item may be in `shopkeep` OR in `consign`/`consign_offer` on one worker, never both (the worker refuses to start).
- Credits floor 10,000 per bot before launch (D6); gifts come from `the_banker` only.
- Never `git add -A` (dirty data/*.json). Commit yaml and docs explicitly. Never SIGSTOP an overmind for more than 60 s (SilenceTimeout 90 s rolling-restarts its fleet).
- Fleet launches use the formula in the overmind table below; every launch includes `--assets-db-path data/assets.db`.

## Review Focus

1. A bot whose wave has not started must sit docked and idle, not loop on a Lawless refusal: Task 4 verifies each non-ice bot is in NO fleet until the reconciler seconds it.
2. The reconciler must never move a crft-mining bot into unlock while 10 are already away: Task 5 verifies `fleet-secondment --status` shows at most 10 `seconded` crft-mining entries.
3. A graduate must come home with its pipeline pointing at its final (Lawless) site and its fit unchanged: Task 5's return check reads the relaunched worker's `--pipeline` and the KB standings.
4. The two haulers must never be both shopkept and consigned for moonshine, and craftsman-10's shopkeep must move to marketbot_alhena before craftsman-10 gets a consign rule: Task 6 orders the edits so the worker start refusal cannot fire.
5. Restarting an overmind relogs its whole fleet through the connection gate: Task 1 does one fleet at a time and checks for `gate block` lines before the next.

---

### Task 1: Operator prerequisites and the overmind restarts

**Files:** none (operations); record each restart in memory's status board.

**Interfaces:**
- Produces: all 10 overminds on the 2026-10-24 `bin/overmind`; craftsman-8 at faction role Officer.

- [ ] **Step 1: Promote craftsman-8** (operator, from the craftsman-boss play_as session): `faction_promote 609bf71b99c9d628b1157bfdbcbb5fba officer`. Verify: `sqlite3 data/assets.db "select faction_rank from agent_profile where player_id='609bf71b99c9d628b1157bfdbcbb5fba'"` → `officer` after the next hourly capture_profile, or `faction_info` in the boss session.
- [ ] **Step 2: Confirm the binaries are current:** `ls -la --time-style=+%m-%d_%H:%M bin/overmind bin/worker` → both 10-24 13:44 or later; `git log -1 --format=%h` ≥ d761593d.
- [ ] **Step 3: Restart the eight stale overminds ONE AT A TIME**, in this order (smallest first): banker, shuttle, assist, hunt, mining, mission-learn, unlock, haul. For each: `ARGS=$(ps -o args= -p <pid> | sed 's#^\./bin/overmind ##')`; `kill -TERM <pid>`; wait until `ps -eo args | grep -c -- '--fleet-name <name>'` (or the fleet's worker count) is 0; `setsid nohup ./bin/overmind $ARGS >> data/overmind/<name>-overmind.log 2>&1 < /dev/null &`. The exact arguments as captured 2026-10-24 (do not add flags):

| fleet | args |
|---|---|
| banker | `--socket data/overmind/banker.sock --fleet data/overmind/banker-fleet.yaml --worker-bin bin/worker --status-file data/overmind/banker-status.json --history-file data/overmind/banker-history.jsonl --assets-db-path data/assets.db` |
| shuttle | same shape with `shuttle` |
| assist | same shape with `assist` |
| hunt | same shape with `hunt` |
| mining | `… --assets-db-path data/assets.db --handoff-queue data/overmind/handoff-queue.json --fleet-name mining` |
| mission-learn | `… --assets-db-path data/assets.db --market-db-path data/market.db` |
| unlock | same shape as banker with `unlock` |
| haul | `… --assets-db-path data/assets.db --secondment-ledger data/overmind/secondments.json` |
| mb (64 workers, do LAST and only when Task 6 needs it) | `--socket data/overmind/mb.sock --fleet data/overmind/mb-fleet.yaml --worker-bin bin/worker --status-file data/overmind/mb-status.json --history-file data/overmind/mb-history.jsonl --market-db-path data/market.db` (verify with `ps` before killing) |

- [ ] **Step 4: Verify after each restart** before starting the next: the fleet's worker count is back to its pre-restart number (`ps -eo args | grep -E '(^|/)bin/worker ' | grep -c -- '--fleet-name <name>'`, or for fleets without `--fleet-name`, the status table in the log), and `tail -n 2000 data/overmind/<name>-overmind.log | grep -c 'gate block'` is 0 since the restart. Expected: ≤ 2 min per fleet; haul ≈ 16 workers, mission-learn 23.
- [ ] **Step 5: Record** in memory (`project_status_board.md`): "all overminds on post-merge binary as of <time>"; `make memory-sync`; commit docs/memory.

---

### Task 2: Fund the 31 miningbots

**Files:** scratch script from `cmd/tools/banker --emit-script`.

- [ ] **Step 1:** `go run ./cmd/tools/banker --from the_banker --floor 10000 --emit-script /tmp/claude-1000/.../grants-all.txt` then `grep '^send_gift "miningbot_' /tmp/.../grants-all.txt > /tmp/.../grants-bots.txt`. Expected: 30 lines (miningbot_0000 already holds ~11k and is skipped), ~9,000 each.
- [ ] **Step 2:** Take the banker worker off its roster for the session (the worker reconnects over play_as in 2 s otherwise): add `the_banker` to `data/overmind/banker-overrides.json` `removed`, `kill -HUP <banker overmind pid>`, wait until no `bin/worker --agent the_banker` process remains.
- [ ] **Step 3:** Split grants-bots.txt into files of 10 lines with `sleep 2s` after each line; for each file: `printf 'run <file>\nexit\n' | go run ./cmd/tools/play_as the_banker`. Expected per file: 10 `"credits_sent"` lines.
- [ ] **Step 4:** Restore: remove `the_banker` from the overrides `removed`, `kill -HUP` the banker overmind, confirm `Ready! Credits:` ≈ 9,660,739 − 270,000.
- [ ] **Step 5 (verify):** `for i in $(seq -w 0 30); do printf 'status\nexit\n' | go run ./cmd/tools/play_as miningbot_00$i 2>&1 | grep -oE 'Ready! Credits: [0-9.]+'; done` → every line ≥ 10,000. (31 logins at 1/s through the gate — fine.)

---

### Task 3: The fitting pass (one module per bot)

**Files:**
- Create: `scripts/fit-miningbots.sh` (bash; loops bot ids and pipes a per-group command file into `go run ./cmd/tools/play_as <bot>`).
- Create: `data/scripts/fit_ice.txt`, `data/scripts/fit_gas.txt` (play_as command lists; plain text, not .smolt).

**Interfaces:**
- Produces: the group table below is the single source of truth for every later task.

| group | bots | module fitted | removed to storage |
|---|---|---|---|
| ice | miningbot_0000 (done), 0001–0003 | `ice_harvester_ii` | `mining_laser_iii` |
| chlorine (gas) | 0004–0011 | `gas_harvester_iii` | `mining_laser_iii`, `pulse_laser_i` |
| titanium | 0012–0019 | stock (`mining_laser_iii`) | — |
| hot cells (gas) | 0020–0025 | `gas_harvester_iii` | `mining_laser_iii`, `pulse_laser_i` |
| spare | 0026–0030 | stock, unfitted | — |

- [ ] **Step 1: Write the command files.** `fit_ice.txt`: `withdraw ice_harvester_ii 1` / `sleep 6s` / `uninstall mining_laser_iii` / `sleep 8s` / `install ice_harvester_ii` / `sleep 8s` / `deposit mining_laser_iii 1` / `sleep 5s` / `ship` / `exit`. `fit_gas.txt`: the same with `gas_harvester_iii`, plus `uninstall pulse_laser_i` / `sleep 8s` before the install and `deposit pulse_laser_i 1` / `sleep 5s` after. All bots are docked at `mobile_capital` (Distant Light) with the harvesters in that station's storage; `install`/`uninstall` need the ship docked.
- [ ] **Step 2: Write `scripts/fit-miningbots.sh`:** usage `fit-miningbots.sh <ice|gas> <bot-id>...`; for each bot: `printf` the file through `go run ./cmd/tools/play_as <bot> 2>&1 | grep -E 'Installed|Uninstalled|Transferred|Error|cpu'`, then `rm -f data/agents/<bot>/play_as.lease`; exit non-zero if any `Error` line appears for a bot and print which.
- [ ] **Step 3: Dry-run on one bot per group** (`miningbot_0001` ice, `miningbot_0004` gas): expected output contains `Installed Ice Harvester II` / `Installed Gas Harvester III` and no `cpu_exceeded`. The gas bot's `ship` must list `cpu_used` ≤ 12.
- [ ] **Step 4: Run the rest:** `scripts/fit-miningbots.sh ice miningbot_0002 miningbot_0003` and `scripts/fit-miningbots.sh gas miningbot_0005 … miningbot_0011 miningbot_0020 … miningbot_0025`. ~40 s per bot.
- [ ] **Step 5 (verify):** `for b in <all fitted>; do printf 'ship\nexit\n' | go run ./cmd/tools/play_as $b 2>&1 | grep -oE '"type_id": "(ice_harvester_ii|gas_harvester_iii|mining_laser_iii)"' | sort -u | tr '\n' ' '; echo " $b"; done` → ice bots show only ice_harvester_ii, gas bots only gas_harvester_iii, titanium/spares only mining_laser_iii.
- [ ] **Step 6: Commit** `scripts/fit-miningbots.sh data/scripts/fit_ice.txt data/scripts/fit_gas.txt` (`git add -f` for data/scripts, which is gitignored).

---

### Task 4: Rosters — crft-mining, unlock, haul, craft

**Files:**
- Modify: `data/overmind/crft-mining-fleet.yaml` (replace the one-bot smoke roster)
- Create: `data/overmind/crft-mining-overrides.json`
- Modify: `data/overmind/unlock-fleet.yaml`, `data/overmind/unlock-overrides.json`
- Modify: `data/overmind/haul-fleet.yaml`, `data/overmind/haul-overrides.json` (only if trader-9/10 appear there)
- Create: `data/agents/trader-9/schedule.json` entries, `data/agents/trader-10/schedule.json` entries (append to the existing arrays; keep the `{id, frequency, command, created_at}` shape)

**Interfaces:**
- Consumes: Task 3's group table.
- Produces: the pipeline strings per bot, used by Task 5's checks.

- [ ] **Step 1: crft-mining-fleet.yaml — all 31 bots + 2 haulers.** `pipeline` values:
  - ice (4): interim secure sites round-robin until unlocked, then Glenhaven/Enif:
    `water_ice,frontier,icecap_drift,frontier_station,trader-9` (0000, 0001), `water_ice,altais,altais_outer_ice,frontier_station,trader-9` (0002), `water_ice,deep_range,deep_range_cryobelt,deep_range_outpost,trader-9` (0003). (icecap_drift richness 75, Altais 50, Deep Range 43; all High/Max security, 1–2 jumps from Distant Light.) Final values after unlock: `water_ice,glenhaven,glenhaven_ice_belt,treasure_cache_trading_post,trader-9` (two bots) and `water_ice,enif,enif_ice_belt,treasure_cache_trading_post,trader-9` (two bots).
  - chlorine (8): `chlorine_gas,glenhaven,glenhaven_gas_cloud,treasure_cache_trading_post,trader-9` — Lawless; these bots are in crft-mining's `removed` until they graduate (Step 3), so the line is inert until then.
  - titanium (8): `titanium_ore,ashmark,ashmark_iron_ti_reef,treasure_cache_trading_post,trader-9` (richness 40; Treasure Cache is the nearest non-stronghold station the builder confirmed reachable — verify the hop count with the BFS used in recon before launch; fall back to `factory_belt_manufacturing_hub`). Lawless; inert until graduation.
  - hot cells (6): `plasma_gas,timberline,timberline_gas_pocket,alpha_centauri_colonial_station,trader-9` — Frontier security, 10,000 pool; SECURE, so these bots can start immediately and skip the unlock queue's first waves.
  - spares (5): no `pipeline`; `role: pipeline_miner, station: frontier_station` and listed in `removed` so they stay parked.
  - haulers: `- { agent_id: trader-9, role: resident, station: treasure_cache_trading_post }` and `- { agent_id: trader-10, role: resident, station: voss_redoubt }`.
  Every bot line: `role: pipeline_miner, station: frontier_station`.
- [ ] **Step 2: crft-mining-overrides.json:** `{"removed": [<chlorine 8>, <titanium 8>, <spares 5>], "updated_at": "<now>", "by": "rollout: Lawless groups wait for unlock; spares parked"}`.
- [ ] **Step 3: unlock-fleet.yaml:** append one line per bot (all 31): `- { agent_id: miningbot_00NN, role: unlock, station: treasure_cache_trading_post, mission_categories: [smuggling, delivery, exploration] }`; add all 31 to `unlock-overrides.json` `removed` (the reconciler deletes an id from `removed` when it seconds it). Both yamls now list every bot (removed-sets rule).
- [ ] **Step 4: haul yaml:** delete the `trader-9` and `trader-10` lines from `haul-fleet.yaml`; if either id is in `haul-overrides.json` `removed` or in `secondments.json` with phase ≠ home, stop and resolve by hand (ledger first).
- [ ] **Step 5: hauler schedules** (append to each agent's schedule.json; `frequency` values are `ten_minutely` / `hourly` / `daily`):
  - trader-9: `ten_minutely` → `deliver water_ice 1900 treasure_cache_trading_post voss_redoubt craftsman-10`; `ten_minutely` → `deliver pirate_moonshine 1900 voss_redoubt treasure_cache_trading_post marketbot_treasure_cache`. (Both are no-ops with a log line when the source storage holds nothing. `deliver` withdraws from trader-9's OWN storage at FROM: the bots gift into it at Treasure Cache; craftsman-10 consigns moonshine into it at Voss — Task 6.)
  - trader-10: `daily` → `buy_directed pharmaceutical_culture 850 sirius_observatory_station 10 self` then `daily` → `deliver pharmaceutical_culture 850 sirius_observatory_station voss_redoubt craftsman-10`. (`buy_directed` hands off AT the purchase station, so the carry is a separate `deliver`; junk_convoy hold 850, speed 2, smuggling 30 — the highest in the fleet.)
- [ ] **Step 6 (verify):** `go run ./cmd/tools/validate-roles data/overmind/roles.yaml` (if it exists) and `python3 -c "import yaml;yaml.safe_load(open('data/overmind/crft-mining-fleet.yaml'));yaml.safe_load(open('data/overmind/unlock-fleet.yaml'));print('ok')"`; `grep -c miningbot_ data/overmind/crft-mining-fleet.yaml` → 31; `grep -c miningbot_ data/overmind/unlock-fleet.yaml` → 31; `jq '.removed|length' data/overmind/crft-mining-overrides.json` → 21; `jq '[.removed[]|select(startswith("miningbot_"))]|length' data/overmind/unlock-overrides.json` → 31.
- [ ] **Step 7: Commit** the yamls, overrides and the two schedule.json files explicitly.

---

### Task 5: Launch and the unlock rotation

**Files:** none new; `data/overmind/secondments.json` gains 27 entries.

**Interfaces:**
- Consumes: Task 4 rosters.

- [ ] **Step 1: Restart the crft-mining overmind** on the full roster: capture its args (`ps`), `kill -TERM`, relaunch with the same args plus `--secondment-ledger data/overmind/secondments.json`. Expected within 3 min: 4 ice + 6 hot-cell bots + trader-9 + trader-10 = 12 workers `Ready!`; the 21 removed bots absent.
- [ ] **Step 2: Nominate the 16 Lawless-bound bots** (chlorine 8 + titanium 8): `bin/fleet-secondment --nominate <16 ids, comma-separated> --home crft-mining --home-overrides data/overmind/crft-mining-overrides.json --home-socket data/overmind/crft-mining.sock --away unlock --away-overrides data/overmind/unlock-overrides.json --away-socket data/overmind/unlock.sock --ledger data/overmind/secondments.json --reason "pirate unlock for pipeline mining"`. Then the ice bots (4) and the hot-cell bots (6) in a second `--nominate` so they queue behind; spares are not nominated.
- [ ] **Step 3: Start the crft-mining secondment daemon** (the existing one sweeps `--home haul` only): `setsid nohup bin/fleet-secondment --watch 5m --home crft-mining --home-overrides data/overmind/crft-mining-overrides.json --home-socket data/overmind/crft-mining.sock --away unlock --away-overrides data/overmind/unlock-overrides.json --away-socket data/overmind/unlock.sock --ledger data/overmind/secondments.json --assets-db-path data/assets.db --max-in-flight 10 >> data/overmind/crft-secondment.log 2>&1 < /dev/null &`.
- [ ] **Step 4 (verify, first sweep):** `bin/fleet-secondment --status | grep miningbot` → exactly 10 `seconded`, the rest `nominated`; `ps -eo args | grep -E '(^|/)bin/worker ' | grep -c 'agent miningbot_'` → 10 (unlock) + 10 (crft-mining active, i.e. 4 ice + 6 hot cells minus any seconded) consistent with the ledger; no miningbot appears in both fleets' worker lists.
- [ ] **Step 5 (verify, command budget):** after the first wave logs in: `grep -c 'gate block' data/overmind/unlock-overmind.log` unchanged over 30 min and no `rate_limited` lines; if either rises, lower `--max-in-flight` to 5 and restart the daemon.
- [ ] **Step 6 (graduation check, ~1 week):** when `--status` shows an entry `home`, confirm `ps` shows that bot under `--fleet-name crft-mining` with its `--pipeline` value, and `sqlite3 data/assets.db "select min(baseline) from agent_standings s join agents a on a.player_id=s.player_id where a.agent_id='<bot>' and s.faction like 'pirate_%'"` → 10. For ice bots, change their `pipeline` to the Glenhaven/Enif final value when they graduate (yaml edit + `kill -HUP` the crft-mining overmind, which is now on the current binary and relaunches the worker with the new flag).

---

### Task 6: Production and sell nodes

**Files:**
- Modify: `data/overmind/craft-fleet.yaml` (craftsman-10, craftsman-8 lines), `data/overmind/mb-fleet.yaml` (marketbot_alhena, marketbot_treasure_cache lines)
- Modify: `data/agents/craftsman-10/schedule.json`, `data/agents/craftsman-8/schedule.json`

**Interfaces:**
- Consumes: Task 1 (mb overmind restarted; craftsman-8 Officer).

- [ ] **Step 1: Shopkeepers.** mb-fleet.yaml: `marketbot_alhena` line gains `shopkeep: ["pirate_moonshine:3000:1500"]`; `marketbot_treasure_cache` line gains `shopkeep: ["pirate_moonshine:3000:1500"]` (operator's 3,000 opening price; floor 1,500). Restart the mb overmind (Task 1 table, 64 workers — do it once, when both lines are in).
- [ ] **Step 2: Crafters — edit order matters.** craftsman-10: FIRST remove `shopkeep: [...]` (its open ask at 3,000 keeps running on the exchange; leave it), THEN add `consign: ["pirate_moonshine:marketbot_alhena:50:200", "pirate_moonshine:trader-9:100"]` beside the existing `faction_pull: [pirate_moonshine]`. craftsman-8 (once Officer): `faction_pull: [pirate_moonshine], consign: ["pirate_moonshine:marketbot_alhena:50:200", "pirate_moonshine:trader-9:100"]`. `kill -HUP` the craft overmind after each line.
- [ ] **Step 3: Craft schedule.** Append to craftsman-10's and craftsman-8's schedule.json: `ten_minutely` → `craft distill_moonshine 60` (60 output = 10 runs; the server draws inputs from CRFT faction storage and delivers output there; faction_pull brings it to personal storage next pass; the still is 2.7 ticks/run so a 10-run job finishes inside the 10-minute window).
- [ ] **Step 4 (verify):** within 30 min of Step 3: craft-overmind.log shows for craftsman-10 `faction_pull: moved N pirate_moonshine`, then `consign: gifted … to marketbot_alhena`; mb-overmind.log shows `shopkeep: posted … pirate_moonshine @3000` for marketbot_alhena; no worker line containing `refus` or `fatal`.
- [ ] **Step 5: Commit** the yaml and schedule files.

---

### Task 7: Day-one measurement and the stop conditions

**Files:** `data/agents/<id>/schedule.json` for craftsman-8, craftsman-10, marketbot_alhena, marketbot_treasure_cache, trader-9, trader-10 (hourly `capture_storage` entries).

- [ ] **Step 1:** Append `hourly` → `capture_storage` to the six agents above (the pipeline_miner role already has it hourly).
- [ ] **Step 2 (verify, after 24 h):** units/day of pirate_moonshine produced = Σ (`faction_pull: moved`) over 24 h in craft-overmind.log; units/day sold = fills from `view_orders` deltas in the shopkeep log lines (`shopkeep: posted` after a disappearance means a fill) plus `market_orders.my_quantity` drops at voss_redoubt / treasure_cache for those two agents. Record both numbers in memory's status board; they size everything after this.
- [ ] **Step 3: Stop conditions** (operator rules, written into the status board): pause the still schedule when alhena's and treasure_cache's open asks have sat unsold for 24 h at the floor; pause a mining group when its hauler's storage at Treasure Cache exceeds 5,000 of that input; pull a bot off a site when three consecutive passes log `mine_qty: … stopped yielding`.

---

## Self-review notes

- Spec coverage: D1 allocation (Task 3 table, Task 4 pipelines), D2 unlock waves and Lawless rule (Task 5 via `--max-in-flight 10`; non-ice groups first because only ice and plasma gas have secure interim sites near the bots — a plan ruling, flagged in the hand-over), D3 fleet/role/harvesters (Tasks 3–5), D4 haulers as residents with schedule links (Task 4 step 5; `buy_directed` cannot carry, so the culture run is buy-to-self plus `deliver`), D5 production node (Task 6), D6 money (Task 2), D8 rollout order (Tasks 1→7), D9 shopkeepers (Task 6). D7 dashboard → plan 3.
- Names consistent with plan 1: `pipeline`, `shopkeep`, `faction_pull`, `consign`, role `pipeline_miner`, verbs `deliver`, `buy_directed`, `craft`.
- Open items left to the executor: the exact Ashmark→Treasure Cache hop count (BFS before launch); whether `validate-roles` exists; split sizes for the banker sessions if the gate is busy.
