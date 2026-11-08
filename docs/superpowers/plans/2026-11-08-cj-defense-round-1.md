# CJ Defense Round 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring Conjunction Junction (CJ) to 2 shields, 2 armor and 6 guns, with power supply 52, using game commands, not code.

**Architecture:** This is an operations plan. Every game action runs through `play_as` on a CRFT agent, with that agent's supervised worker paused for the duration (SIGSTOP/SIGCONT, under 60 s) or removed from its fleet (overrides + SIGHUP) for long trips. Each facility build is a separate task that STOPS for the operator's approval.

**Tech Stack:** `play_as` (build fresh: `go build -o $S/play_as ./cmd/tools/play_as`, `$S` = session scratchpad), overmind overrides sidecars, `data/assets.db` captures.

**Spec:** `docs/superpowers/specs/2026-11-08-cj-defense-round-1-design.md` (5c0996fb). Background: `docs/superpowers/specs/2026-11-07-cj-station-defense-design.md`.

## Global Constraints

- **Every `facility faction_build` needs the operator's explicit approval in chat, one build at a time.** Never batch, never pre-authorise.
- CRFT treasury floor **1,000,000** credits; consumable purchases ≤ **1M** total.
- Stock a facility's upkeep inputs at CJ **before** building it.
- Never log into **craftsman-1** (the operator's account). Builds run as **miningbot_0000** (CRFT Officer, docked at CJ).
- Energy crystals are reserved.
- CJ: base `c976fa7df53c87f9616165d7d411b41a`, poi `862e07fa76571d0a76cab3cbaf4046e7`, system `cloverfield`.
- Every play_as session on miningbot_0000 passes `--record-dir data/stations/cloverfield`.
- End every play_as script with `quit`; delete `data/agents/<agent>/play_as.lease` afterwards.
- `send_gift` takes the game **username** (from `data/agents/<id>/credentials.json`), and the sender must be **docked**.
- Commit only named files; never `git add -A` (data/*.json churn).

## Review Focus

1. **A play_as session on a running worker gets displaced (4001)** before the server applies a pending action. Each build/buy task re-reads the result (`facility list` / `get_cargo` / `view_faction_storage`) rather than trusting the "pending" line.
2. **Crafted cells land in the wrong storage.** A job sourced from personal storage delivers to personal storage; the server covers a job from ONE source in full. Task 2 verifies `power_cell` in *faction* storage before Task 4, and uses `faction_deposit_all` if they landed personal.
3. **Live power draw is higher than the 11-07 estimate.** Task 7 reads `get_base` power supply/draw before the shields; if the shields would push draw past supply, stop and ask.
4. **Guns build but stay degraded (no ammo).** Task 5 checks the new batteries show `100% ok` and that `weapon_dps` in `get_base` rises from 30.
5. **Shield upkeep runs on a 100-tick cycle.** Task 8 measures power_cell burn over 48 h; above ~100/day, stop and ask (the spec's tripwire).

---

### Task 1: Treasury top-up (2M from the_banker)

**Interfaces:**
- Produces: CRFT treasury ≥ 7,400,000 (5,486,978 at 11-08 21:34Z + 2,000,000, less any spend since).

- [ ] **Step 1:** As `the_banker` (docked; its single-member banker fleet, so pause the worker with SIGSTOP or let the lease park it), run `send_gift miningbot_0000 credits 2000000`, then `get_status`. Expected: `credits_sent` 2,000,000 and the_banker's wallet down by that much.
- [ ] **Step 2:** As `miningbot_0000` at CJ (worker SIGSTOPped, under 60 s): `faction_deposit_credits 2000000`, then `faction_info` (or `view_faction_storage`). Expected: treasury up by 2,000,000.
- [ ] **Step 3:** Log the treasury value in `data/stations/cloverfield/ledger.jsonl` (play_as `ledger credits credits 2000000`).

### Task 2: Make ~1,100 power cells at CJ

**Interfaces:**
- Consumes: CJ faction storage argon_gas (142,500), copper_ore (47,066), circuit_board (14,439).
- Produces: CJ faction storage `power_cell` ≥ 1,050.

CJ already has an Argon Cell Lab (`synthesize_argon_power_cell`: 3 purified_argon + 2 copper_wiring + 2 circuit_board → 2 power_cell, ~585/hr) and a Neon Cell Lab, plus the copper_wire_mill (`process_copper_wiring`: 4 copper_ore → 2). Purifying is a hand recipe (`purify_argon`: 5 argon_gas → 3 purified_argon). All crafting runs as miningbot_0000 at CJ with `prefer_own`/faction sourcing, worker paused, under 60 s per session (queue the job, then let it run).

- [ ] **Step 1:** `craft purify_argon 1700 prefer_own` (units of output; ≈ 2,834 argon_gas). Verify `purified_argon` ≥ 1,650 in faction storage when the job finishes (`craft-queue` / `view_faction_storage`).
- [ ] **Step 2:** `craft process_copper_wiring 1100 prefer_own` (one-off, on top of the hourly 50 in schedule id 50). Verify `copper_wiring` ≥ 1,100.
- [ ] **Step 3:** `craft synthesize_argon_power_cell 1100 prefer_own`. If the server refuses the full job, the craft verb halves it down to 10 units; queue the rest in further runs. Verify `power_cell` ≥ 1,050 in faction storage.
- [ ] **Step 4:** Record the labor charged per job (treasury before/after) with `ledger`.
- [ ] **Fallback (only if the labs fail or stall):** buy at Node Alpha Processing Station (asks from 290) with trucker-00, gifting to miningbot_0000 at CJ; ask the operator first, since it spends credits.

### Task 3: Build the second `stellar_collector_grid` — **STOP for approval**

**Interfaces:**
- Consumes: steel_plate 2,000, circuit_board 1,200 from CJ faction storage.
- Produces: power supply 40.

- [ ] **Step 1:** Ask the operator: "Build stellar_collector_grid #2 (900k + 2,000 steel + 1,200 circuit_board)?" Wait for yes.
- [ ] **Step 2:** As miningbot_0000 (paused worker): `get_base` (record power/life support), `facility faction_build stellar_collector_grid`, `wait 15s`, `facility list`, `get_base`. Expected: two Stellar Collector Grid rows; power supply 40 once built.

### Task 4: Build `bunker_fed_reactor` — **STOP for approval**

**Interfaces:**
- Consumes: steel_plate 2,000, power_cell 400 (from Task 2), station fuel reserve ≥ 2,000.
- Produces: power supply 52.

- [ ] **Step 1:** As miningbot_0000: `get_base`; confirm station fuel reserve ≥ 2,000 and faction `power_cell` ≥ 400. If either is short, stop and report.
- [ ] **Step 2:** Ask the operator for approval (500k + 2,000 steel + 400 power_cell; burns 55 fuel/cycle). Wait for yes.
- [ ] **Step 3:** `facility faction_build bunker_fed_reactor`, `wait 15s`, `facility list`, `get_base`. Expected: reactor listed; supply 52 when built.

### Task 5: Build 5 × `point_defense_battery` — **STOP for approval (each)**

**Interfaces:**
- Consumes per gun: steel_plate 400, circuit_board 60, targeting_computer 4 (CJ holds 38 targeting computers; 20 needed).
- Produces: 6 guns; `weapon_dps` > 30.

- [ ] **Step 1:** Confirm `coolant_fluid` ≥ 200 and `ferrous_slug_case` ≥ 1,000 in CJ faction storage.
- [ ] **Step 2:** For each of the 5 guns: ask the operator, wait for yes, then `facility faction_build point_defense_battery` and `facility list`. (The operator may approve all five in one message; that counts as five approvals only if they say so.)
- [ ] **Step 3:** After the last one, `get_base`. Expected: six Point Defense Battery rows `100% ok`; `weapon_dps` well above 30 (record the value). If any show degraded/no ammo, stop and report.

### Task 6: Build the second `reinforced_plating` — **STOP for approval**

- [ ] **Step 1:** Confirm `armor_plate` ≥ 300 (240 build + upkeep). Ask the operator (700k + 2,000 steel + 240 armor_plate). Wait for yes.
- [ ] **Step 2:** `facility faction_build reinforced_plating`, `facility list`, `get_base`. Expected: two Reinforced Plating rows.

### Task 7: Build 2 × `shield_projector` — **STOP for approval (each)**

**Interfaces:**
- Consumes per shield: steel_plate 1,200, superconductor 200, power_cell 300; draw 10 power; upkeep 4 power_cell + 2 coolant per cycle.

- [ ] **Step 1:** As miningbot_0000: `get_base`. Confirm power supply − current draw ≥ 20, faction `power_cell` ≥ 650 (600 build + ≥ 1 day upkeep), `superconductor` ≥ 400, `coolant_fluid` ≥ 200. If not, stop and report the numbers.
- [ ] **Step 2:** Ask for approval of shield #1; build; `facility list`; `get_base`. Repeat for shield #2.
- [ ] **Step 3:** Expected: two Shield Projector rows `100% ok`; `max_shield` above 12,000 (record it). Record `power_cell` stock and the tick.

### Task 8: Verify, watch upkeep 48 h, record

- [ ] **Step 1:** As miningbot_0000: `facility list` and `get_base`. Done when the list shows 2 shield_projector, 2 reinforced_plating, 6 point_defense_battery, 2 stellar_collector_grid, 1 bunker_fed_reactor, all maintenance OK, and power supply ≥ draw.
- [ ] **Step 2:** Check CJ's row on `https://game.spacemolt.com/api/stations` for the new `weapon_dps`, shield and hull.
- [ ] **Step 3:** After 24 h and 48 h, read `power_cell`, `coolant_fluid`, `armor_plate`, and the station fuel reserve from `data/assets.db`. Compute burn/day. If power_cell burn > ~100/day, stop and ask the operator (spec tripwire).
- [ ] **Step 4:** Append findings (dates, tick, before/after `weapon_dps`/shield/hull, measured burn rates, defense cycle length) to `data/stations/cloverfield/findings.md`, and update memory `project_cloverfield_home_station.md`. Commit `data/stations/cloverfield/findings.md` and this plan's ticked checkboxes by name.
- [ ] **Step 5:** Note the follow-ups for round 2: top up the 30% repair reserve, battery storage, upgrade 2 point defense batteries to `station_defense_turret`.
