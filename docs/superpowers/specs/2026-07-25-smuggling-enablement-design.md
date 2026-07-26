# Smuggling enablement for the mission-learn fleet

**Date:** 2026-07-25
**Status:** DRAFT — design under review.

**Pilot status (engineer-2, 2026-07-25): PHASE 1 CONFIRMED end-to-end.** Chain leg 1
(L0→1) + leg 2 (L1→2, cross-border to Sol) run clean on a freighter via continuous-travel
evasion (no seizure); at level 2 a smuggling courier accepted successfully (was
`skill_required` at L1). Confirmed thresholds L0→1=60 / L1→2=165 / L2→3=340. Still
_(pilot-pending)_: leg 3 `an_introduction` shape, chain-2/3 mission shapes, and the
accepted-courier completion. Notes: accepted courier instance id shape is
`smuggling_courier_claim_<hash>`; courier `expires_at` varies widely (87 ticks at
treasure_cache vs ~11h for the Sol courier).

## Goal

Let every mission-learn worker become eligible for `category=smuggling` missions and
run them autonomously, so the idle fleet earns from smuggling couriers in addition to
delivery + exploration. Eligibility is gated by a game skill (`smuggling` level 2), so
the work has three parts:

1. **Capture** all mission-board content — including the parameterized (procedural)
   missions currently discarded — so we can see where smuggling chains/couriers spawn.
2. **Enable** `category=smuggling` in the missionrunner selection code.
3. **Bootstrap** each worker's `smuggling` skill to level 2 by auto-running the
   smuggling tutorial chain, which is the only route to the gate.

This is also the **first worked example of the broader "train every agent to work in any
overmind fleet" endgame** (`project_generalist_agent_selector`): a repeatable pattern for
unlocking a gated capability (run the gate chain → cross the skill threshold → the
category/role opens), which later capabilities (mining, combat, etc.) and the
capability-driven ship provisioning (`project_ship_role_naming_scheme`) follow.

## Pilot findings (engineer-2 @ treasure_cache_trading_post, 2026-07-25)

Driven manually via `play_as engineer-2` (pulled from the fleet via the overrides
sidecar + SIGHUP; re-add when the pilot completes).

### The smuggling tutorial chain (the "gate")
All legs are **single `deliver_item` objectives with items provided** (no buying). The
chain is anchored at `treasure_cache_trading_post` (Nebula frontier); giver "Dara Kesh".

| Leg | mission_id | type | difficulty | deliver (provided) | destination | reward | smuggling XP | chain_next |
|-----|-----------|------|-----------|--------------------|-------------|--------|--------------|------------|
| intro | `a_word_in_private` | delivery | 1 | — (dock at Cache) | treasure_cache_trading_post | 500cr, +1 rep | **+50** | — |
| 1 | `no_questions_asked` | smuggling | 2 | 5 starshine | Haven / grand_exchange_station (3 jumps, Nebula-internal, no border) | 800cr | **+100** | `across_the_line` |
| 2 | `across_the_line` | smuggling | 4 | 3 void_dust | **Sol / confederacy_central_command (17 jumps, CROSSES Solarian border)** | 2500cr | **+200** | `an_introduction` |
| 3 | `an_introduction` | _(pilot-pending)_ | ? | ? | ? | ? | ? | unlocks **chain 2** |

### Second chain + pirate-reputation unlock (from operator)
After completing chain 1 (leg 3 `an_introduction`), a **second chain** unlocks once
`smuggling` reaches **level 3**. Chain 2 is ~3 more, longer smuggling requests. The
payoff for completing chain 2 is a large **pirate reputation** boost: from **−30
(attacked by pirates on sight)** to **+10 (can travel through and dock at pirate
strongholds without being attacked)**.

This is a **fleet-wide strategic unlock**, well beyond mission-learn income: pirate-
stronghold access (sable_port / crix_stronghold / kael_arsenal / voss_redoubt) opens
those markets/services to the mb + haul fleets and removes the attack-on-sight hazard in
pirate space. Progression ladder:
- **level 2** → repeatable couriers eligible (Part 3 core goal).
- **level 3** → chain 2 unlocks.
- **complete chain 2** → pirate rep −30 → +10 (stronghold access).

### Third chain — Crimson wormhole shortcuts (from operator)
After chain 2, a **third chain** unlocks that grants **2 wormhole shortcuts into Crimson
Pact space, originating from one of the pirate strongholds**. This is the only practical
way into Crimson territory — normal routes go through the Rampart checkpoint, which
smuggling back-doors can't bypass ("nobody gets past the Rampart," per leg 2's dialog).
Reaching the stronghold requires the chain-2 pirate-rep unlock, so the chains are
strictly ordered.

Strategic value: direct wormhole access to Crimson markets (e.g. `crimson_war_citadel`
at Krynn — a 26-jump haul via the Rampart today) for the haul + mb fleets. Wormhole map
knowledge is likely universal-once-discovered, but the stronghold departure point needs
the pirate-rep unlock.

### Full smuggling progression (operator-provided)
| Stage | requires | unlocks |
|-------|----------|---------|
| Chain 1 (3 legs) | — | smuggling XP → **level 2** = couriers eligible |
| Couriers | level 2 | repeatable smuggling income |
| Chain 2 (~3 longer runs) | level 3 (+ chain 1 done) | **pirate rep −30 → +10** = stronghold access |
| Chain 3 | chain 2 done (stronghold access) | **2 wormhole shortcuts into Crimson space** |

Chain 1 + couriers = **Phase 1** (fleet-wide, the immediate deliverable). Chains 2 & 3
= **Phase 2** — high value, but scoped after Phase 1 proves out.

**Phase 2 beneficiaries (why the pirate-rep / wormhole unlocks matter, per-agent):**
- **Stronghold-resident marketbots** — chain 2 (+10 rep) is a hard prerequisite to dock
  and scan at their assigned stronghold (see cohort below). *Required.*
- **Shuttle fleet** — chain 2 (+10 rep) lets shuttle agents haul **passengers to pirate
  strongholds** without being attacked on sight, opening stronghold passenger routes /
  demand. (Shuttle today = johnny_cab canary.)
- **Mission runners themselves** — the board often carries **ordinary `delivery`
  missions whose destination is a pirate stronghold**, uncompletable today (can't dock at
  −30). Chain 2 (+10 rep) makes those acceptable, widening the mission-learn pool with
  *non-smuggling* work. This is the direct answer to the original "take on more missions
  overall" goal.
- **Haul + mb fleets** — chain 3 (Crimson wormholes) opens Crimson markets
  (e.g. crimson_war_citadel/Krynn, 26 jumps via the Rampart today).

Because unlocks are per-agent (no shared accounts), each agent in these roles that wants
the benefit runs the chains on its own account.

**Phase 2 cohort — stronghold-resident marketbots (the concrete driver).** A set of
star-named `marketbot_*` accounts exist specifically to be **resident market-scanners at
pirate strongholds** (`marketbot_alhena`, `marketbot_algol`, `marketbot_bellatrix`, and
similar; currently created but in no fleet YAML). Each has a chicken-and-egg onboarding:
at pirate rep −30 it is attacked on sight and cannot dock at its target stronghold, so it
**must clear chain 2 (→ +10 rep) first**, from accessible space, before it can relocate
and begin resident scanning. So for these agents the smuggling bootstrap (chains 1 → L2 →
grind to L3 → chain 2) is a **required onboarding prerequisite**, not optional — the
lifecycle is: bootstrap in safe space → verify pirate rep +10 → travel to assigned
stronghold → join a stronghold-resident marketbot fleet. (Stronghold assignment per
marketbot: TBD — 4 known strongholds sable_port / crix_stronghold / kael_arsenal /
voss_redoubt; see `reference_pirate_base_registry`.) Chain 3 (Crimson wormholes) is
independent of stronghold residency and applies to whichever agents want Crimson routing.

**Account model:** every agent is its own game account (no shared accounts). So the
pirate-rep and Crimson-wormhole unlocks are **per-agent** — they benefit only the agent
that ran the chains. There is no single-agent shortcut to unlock stronghold/Crimson
access for the fleet; each agent that needs it must complete chains 2 & 3 itself. Phase 2
is therefore an **opt-in per-agent progression**, rolled out to the subset of agents that
would actually route through pirate strongholds or into Crimson markets (candidates: the
haul/mb agents that trade Crimson goods), rather than a single dedicated runner.

### The eligibility gate
Attempting to accept a repeatable courier at smuggling level 1 returns:
```json
{ "code": "skill_required", "message": "Courier runs require smuggling level 2." }
```
So **repeatable `category=smuggling` courier runs require smuggling level 2.** The
`code: "skill_required"` is programmatically detectable — the bootstrap trigger.

### XP model + the level-2 math
Skill XP is per-level; `next_level_xp` is the threshold _within the current level_ to
advance. Observed thresholds (confirmed on engineer-2): **L0→1 = 60, L1→2 = 165, L2→3 =
340**. So 225 total XP from a cold start reaches level 2; reaching level 3 (chain-2 gate)
needs another 340. engineer-2 after legs 1+2: **level 2, 115/340** — i.e. +225 more XP
(≈2–4 courier runs) unlocks chain 2.

Chain grants: intro +50, leg 1 +100, leg 2 +200 = **+350**, which clears level 2 with
margin. **Legs 1 + 2 alone (+300) reach level 2** from a cold start; the intro and leg 3
are extra. Crucially, the *easy* smuggling XP (intro +50, leg 1 +100 = 150) is **not
enough** on its own, and every courier is itself level-2-locked — so **the only path to
level 2 runs through the cross-border leg 2 (`across_the_line`).**

### Repeatable couriers (the income, once eligible)
All `type: smuggling`, single `deliver_item`, **items provided**, **no `warnings` field**
in the observed board. From treasure_cache they are long hauls:

| item | dest | jumps | reward | XP | difficulty | expiry (ticks) |
|------|------|-------|--------|----|-----------|----------------|
| pirate_moonshine / nerve_burn | Frontier Station (altais) | 14 | 300cr | +50 | 3 | 87 |
| nerve_burn | Confederacy Central Command (Sol) | 17 | 1000cr | +125 | 6 | — |
| red_mist | Central Nexus (nexus_prime) | 24 | 1400cr | +175 | 6 | 1287 |

These specific ones are too far / too short-expiry for the current selection gates
(`DefaultMissionMaxJumps=5`, `missionMinExpiryTicks=180`). Couriers must be discovered
**near** each worker — which is what the capture table is for.

### Customs mechanics (from leg 2 dialog + prior notes)
Customs ships watch empire borders and scan inbound traffic; contraband found = seized +
fined. Back doors exist (dialog names **Epsilon Eridani** as a soft crossing into Sol).
Our standing note: **no scan unless a ship stops ~10 ticks at a border** — continuous
travel ≈ zero confiscation risk. The missionrunner autopilots continuously, so evasion
is largely automatic **provided the park/reposition backoff never idles in a border
system**. _(Pilot-pending: confirm engineer-2's slow Bulk Terms makes Sol clean.)_

## Part 1 — Capture parameterized missions

**What exists (matches the chosen model exactly):**
- `mission_templates` (PK `id`) — dedup catalog with `first/last_seen_tick`,
  `rewards_skill_xp`, `provided_items`, `chain_next`, `requirements`,
  `required_modules`, `difficulty`, per-field diff tracking.
- `mission_objectives` — child rows per mission.
- `mission_template_locations` — per-`(mission_id, base_id)` sighting log.
- `worker.KBUpdateMissions` captures a board; play_as `update_missions` / `update_all`
  and the fleet `kb_update` dispatch call it.

**The gap (confirmed):** `KBUpdateMissions` (capture.go:790) skips entries with empty
`template_id`. Parameterized missions — couriers (`smuggling_courier_<origin>_<dest>_<item>~<hash>`)
and trade-runs (`trade_...`) — have no `template_id`, so they land in the
"N procedural skipped" bucket and are lost. These are exactly the repeatable smuggling
income we need to route the fleet to.

**Change:**
1. When `entry.TemplateID == ""`, derive a **synthetic template id** = `mission_id`
   truncated at the first `~` (e.g.
   `smuggling_courier_treasure_cache_trading_post_frontier_station_pirate_moonshine`).
   This dedups repeat sightings of the same route while keeping economics.
2. Add a `procedural INTEGER DEFAULT 0` column to `mission_templates` (via the standard
   `ensure*Cols` pattern + regenerate `initial_schema.sql`; do NOT alter the `ships`
   table — N/A here but note the ships-table migration trap for the pattern). Set it to
   1 for synthetic-id rows so consumers can distinguish hand-authored from route-generated.
3. Store the pre-`~` prefix's constituent fields as-is (objectives already carry item/
   qty/dest); the synthetic id + objectives are enough to reconstruct the route.

**Cadence:** confirm the fleet's `kb_update` runs often enough to sweep boards. Optional
enhancement: capture on every missionrunner board read (throttled to once/dock or on
board-change) so capture rides the existing paid coverage. _(Decision: start with the
existing kb_update cadence; add per-dock capture only if coverage is thin.)_

## Part 2 — Enable `category=smuggling` in the missionrunner

Edit points (mapped):
1. `pkg/worker/mission_select.go:108` — add `missionTypeSmuggling = "smuggling"`.
2. `deliverShape` (mission_select.go:118) currently requires `type==delivery`. Generalize
   to accept `delivery` **or** `smuggling` for the single-`deliver_item` shape. Keep the
   `required_modules` gate (a mission needing `smuggler_hold` is skipped unless the ship
   has it).
3. `buildMissionCandidate` warnings gate (mission_select.go:141): for `smuggling`
   missions, contraband/insurance warnings are expected — **do not auto-reject on
   warnings for the smuggling category** (or filter only genuinely blocking warning
   types). Observed couriers carried no warnings, but chain/other couriers may.
4. Accept switch (`pkg/worker/mission.go:629`, the `default: continue`) — add a
   `smuggling` case wired to the (generalized) delivery candidate builder, gated by
   `missionCategoryEnabled(deps, "smuggling")`. Mirror in the resume path (~mission.go:1141).
5. Economics: provided items → `net = reward − fuel`. Consider a smuggling-specific
   `maxJumps` (couriers pay more and are longer) and relaxing `missionMinExpiryTicks`
   evaluation for provided-item runs. _(Keep v1 conservative; revisit after capture shows
   the real near-worker courier distribution.)_
6. Customs evasion: ensure the reposition/park backoff never idles ≥10 ticks in a border
   system while carrying contraband. Prefer continuous travel; optionally prefer routes
   through known back-door crossings (KB-derived).
7. Fleet YAML: add `smuggling` to `mission_categories` for mission-learn workers (only
   after they are level-2 eligible — see Part 3).
8. **Stronghold-destination dockability gate.** `buildMissionCandidate` checks
   reachability by *jumps* only — it does not check whether the worker can *dock* at the
   destination. A delivery (smuggling or ordinary) to a pirate stronghold is uncompletable
   at pirate rep < the stronghold's threshold (attacked on sight, can't dock). Add a gate:
   reject a candidate whose destination base is a pirate stronghold unless the worker's
   pirate standing clears it. Once an agent has chain-2 rep (+10), the same missions pass —
   this is what turns pirate-rep into "more acceptable missions." (Stronghold set from
   `reference_pirate_base_registry`; rep read from player standings.)
9. Tests: update `mission_select_test.go:70-98` (currently assert smuggling rejected) and
   add smuggling-accepted cases; add a `skill_required` handling test; add a
   stronghold-destination dockability-gate test.

## Part 3 — Smuggling level-2 bootstrap

Parallel to the freight probation bootstrap; gated behind a `--smuggling-bootstrap` flag
(default OFF initially, canary rollout).

**Trigger:** worker whose `smuggling` skill < 2 (read from state/`get_skills`), or which
catches `code: "skill_required"` on a courier accept.

**Action:**
1. Use the KB (`mission_templates` + `mission_template_locations`) to find the nearest
   **chain-entry station** — a station offering the smuggling intro/leg-1 (type
   `smuggling` mission with a `chain_next` and smuggling XP reward). For Nebula this is
   `treasure_cache_trading_post`; other empires have their own — capture reveals them.
2. Route there, run the chain in `chain_next` order: `accept_mission` → deliver provided
   items → `complete_mission` → follow `chain_next`, until `smuggling >= 2`.
3. Cross-border leg (`across_the_line` → Sol): rely on continuous-travel evasion; optionally
   route via a known back door (Epsilon Eridani for Sol). Provided items → a seizure only
   fails the mission (no credit loss); retry.
4. On reaching level 2, the worker's normal `category=smuggling` selection (Part 2) takes
   over. Record completion so the bootstrap doesn't re-run.

**Phase 2 (chain 2 → pirate rep, chain 3 → Crimson wormholes):** optionally continue past
level 2 — grind `smuggling` to level 3 (via chain-1 leftovers + couriers), run chain 2
(~3 longer requests) for the pirate-rep reward (−30 → +10), then chain 3 for the Crimson
wormhole shortcuts. Every agent is its own account, so these unlocks are **per-agent** and
benefit only the agent that ran them — Phase 2 is opt-in per agent, targeted at those that
would route through pirate strongholds or Crimson markets. _(Scope after Phase 1 proves
out; needs the leg-3 + chain-2/3 mission shapes from a pilot.)_

**Ship selection.** Freighters are **not ideal for smuggling** — slow and scan-prone, so
more customs risk on the cross-border legs. The catalog exposes a concrete selector: the
`ships.inherent_capabilities` JSON (serverapi `InherentCapabilities`) lists a
`scan_resistance` capability with a numeric value on certain hulls. **Prefer hulls with a
`scan_resistance` capability for chain/courier runners**, higher value = safer crossing.

Scan-resistant hulls in the current catalog (value): `solipsism` 50 (+cloak 60),
`probable_cause` 40 (+fuel_efficiency 25), `dust_devil` 30, `interstice` 25 (+cloak),
`terms_and_conditions` 25, `absence` 20 (+cloak 30), `dirk` 20, `parallax` 20 (+cloak),
`reticence` 20, `cloister` 15, `arbitrage` 15, `attenuation` 15, `qualia` 15,
`residuum` 15. Role fits:
- **Couriers / cross-border:** `probable_cause` (scan 40 + fuel efficiency) or `solipsism`
  (stealth king) for the long, border-crossing runs.
- **Shuttle-to-stronghold:** `reticence` / `cloister` carry **both** `scan_resistance`
  **and** passenger berths — the natural passenger-to-stronghold hulls.

The bootstrap / role assignment can query the ships catalog for `scan_resistance` to pick
or commission an appropriate hull. (engineer-2's "Bulk Terms" freighter has none — the
deliberate worst-case pilot; a clean crossing on it means any hull is fine.)

**Ties into the ship-role-naming scheme** (`project_ship_role_naming_scheme`, a separate
future feature): that scheme names owned hulls by role (`miner`, `freighter`, `explorer`,
`mission_runner`, `smuggler`, …) so the overmind can swap an idle agent onto a
role-appropriate ship. Smuggling adds a **`smuggler`** role → a scan-resistant hull, and
reuses the shuttle/passenger hulls (`reticence`/`cloister`) for stronghold passenger runs.
The mechanism to *switch* an idle mission-runner onto its scan-resistant smuggler hull for
the chain/courier work (then back) belongs to that scheme; buying/fitting stays out of
scope there (assume the named hull is already owned). This spec depends on it only for the
"put the right hull under the smuggling agent" step — a soft dependency, since the pilot
proves even a freighter can (worst-case) complete the chain.

**Open questions for the bootstrap:**
- Which agents should pursue Phase 2? Per-agent unlock (no shared accounts), so apply to
  the subset that actually benefits from stronghold/Crimson access, not all 42.
- Level 2→3 XP threshold and chain-2/3 mission shapes _(pilot-pending)_.
- Are chain-entry stations present in every empire region, or must non-Nebula workers
  travel cross-region to Nebula? (capture answers this)
- Leg 3 `an_introduction` — is it required for anything beyond level 2 (e.g. a further
  unlock)? _(pilot-pending)_
- Concurrency/staggering: 42 workers routing to a few chain-entry stations — pace like
  the login stagger.

## Rollout
1. Part 1 (capture) first — merge, let the fleet build the smuggling-mission map for a day.
2. Part 2 (selection) behind the category flag; unit-tested.
3. Part 3 (bootstrap) on a single canary (engineer-2), verify level-2 attainment +
   first autonomous courier, then wave out (mirror the freight canary → wave-1 rollout).

## Out of scope
- No new customs/border simulation subsystem (server-side only; we react to events).
- No smuggler_hold auto-fitting in v1 (skip module-gated missions).
- No ship acquisition/fitting for smuggler hulls in v1 — the pilot proves even a freighter
  can (worst-case) run the chain. Putting the right scan-resistant hull under each agent is
  deferred to the ship-role-naming scheme (near-term: switch owned hulls) and its
  longer-term **fleet-provisioning** horizon (buy/build/fit a role hull per role per agent).
  See `project_ship_role_naming_scheme`.
