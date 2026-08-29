---
name: project_pirate_reputation_unlock_campaign
description: "Fleet-wide campaign to raise every agent's pirate baseline from -30 to 10; the unlock role, why only 4 of 33 are pinned, and the two blockers"
metadata: 
  node_type: memory
  type: project
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T01:05:40.741Z
---

**Goal (operator, 2026-08-11): every agent unlocks pirate reputation.** At baseline
-30 — the only hostile default in the game — an agent cannot dock at a stronghold and
is **attacked on sight**. Completing smuggling chain 2 mission 1 (`an_introduction`)
raises the BASELINE to 10 permanently, which grants docking AND stops the attacks. So
this is a safety property, not a trade perk. **7 of 122 agents hold it; 115 do not.**

Chain: `a_word_in_private` (dock_at_base, +50 XP → L1) → `no_questions_asked` →
`across_the_line` → couriers to smuggling **L3** → `an_introduction`. Runs unattended
in ~3h. The bar is low — engineer-2 unlocked at smuggling L4.

**SHIPPED `c31371c0`:** `unlock` role in `data/overmind/roles.yaml` (missionrunner
behaviour + full capture schedule) and `data/overmind/unlock-fleet.yaml`, 33 workers,
which **replaces `idle-fleet.yaml`** (`git mv`). Guarded by
`pkg/worker/unlock_fleet_test.go`.

## ⭐✅ 2026-08-21: johnny_cab GRADUATED and went home to the shuttle fleet

The loan is repaid. johnny_cab holds **baseline 10 on all nine pirate factions**
(crix/dross/kael/korr/mera/nyx/sable/thane/voss), smuggling **15**, zero bounty — so
the nine stronghold destinations the shuttle role used to refuse are payable fares now,
which was the entire point of lending him out on 2026-08-12. Campaign counter is **26**
of 161 agents.

**He had been dead weight for 47 hours before anyone looked.** From 2026-08-19 22:01 he
sat in a ~10s livelock — **14,244 iterations** — on a finished exploration mission
(`cd407fa0…` "HEXC recon: Hex Star", 0 legs remaining) whose return-dock could never
succeed: `Finding route to dheneb → already at target → dock → Error: Already docked →
held for next pass`. Same family as [[reference_sell_leg_dock_gap]]: an already-there
early return meeting a leg that only completes on a fresh dock. **A completed mission
with 0 legs left is never retired if its return-dock reports "Already docked" — the
generic symptom to grep for is `held for next pass` repeating at tick cadence.** He was
also parked in Dheneb rather than at his pin, and `unlock-overrides.json` is stamped
`by: "restore johnny_cab after hand-driven commands"` (2026-08-19T03:40Z), so a
hand-driven session likely left the mission half-finished.

Nothing alerted: fleet-watch catches log SILENCE and unmatched disconnects, and this
worker was *loud* and *healthy* the whole time. **A livelock is invisible to every
health check we have.**

Fuel worry was unfounded — the `shuttle` role refuels at the origin before departing
(`Refueled at station: 120/120` then `Fuel: 2 per jump, ~32 total, 120 available` on a
16-jump run), unlike the pinned mission roles in
[[reference_pinned_mission_workers_never_refuel]].

Move procedure and the two monitors that need a restart: [[reference_overmind_launch_commands]]

## ⭐ 2026-08-12: DEPLOYED, wave 1 BANKED, all 9 marketbots now pinned

The overmind IS running `--fleet unlock-fleet.yaml` (the "NOT YET DEPLOYED"
section below is spent). Progress counter **7 → 11**, and wave 1 delivered
exactly what it was for: **marketbot_alhena and marketbot_sheratan both hold
the unlock** (smuggling 8, pirate baseline 10), alongside miner-1/miner-2.

**The strand fear is a HULL property, not a jump count** — this is what
unblocked the other seven (`d56bbef9`). engineer-5 died on 135 fuel needed
against a 128 tank. Jump fuel is `ceil(scale^1.5 × speed)`, so a scale-1
starter burns **1-2 per jump**; all seven marketbots fly scale-1 hulls with
95-130 tanks. Worst case gsc_0008 (Maul, 2/jump, 27 jumps) needs 54 and
arrives with 46 spare. **The server's own find_route confirmed every distance
(16/16/16/16/24/27) and printed no "Not enough fuel!" on any of them.**
Method: assets.db `agent_hulls` + `catalog_ships.base_speed` + BFS over
knowledge.db `connections` (graph validated against find_route on an
independent 7-jump route).

**⭐ BUT SIZE THE PIN ON DEPARTURE FUEL, NOT TANK CAPACITY.** A margin table
built from `fuel_max` is wrong: the autopilot departs on **whatever fuel the
agent happens to be carrying**, and these had been burning it on local
missions. Actual departures vs requirement — barnard_44 87/32, bellatrix
83/16, zaniah 76/16, gliese_581 77/24, algol 71/32, xamidimura 61/32, and
**gsc_0008 58 against a 54-fuel route: a 4-fuel margin** before the 1-2 fuel
in-system approach. Six were fine; the seventh was one bad rounding from
repeating engineer-5. Read `agent_hulls.fuel_current`, not `fuel_max`.

**The pin path has no fuel gate** — `cmd/tools/station-probe` checks each leg
against measured fuel before committing to it, and the pin does not. That is
the gap worth closing.

**treasure_cache_trading_post SELLS FUEL**, so the crossing is a one-time cost:
every arrival topped off on docking (120/120, 120/120, 130/130). An agent only
has to *reach* the giver.

**The wallets are no longer thin either**: the nine went from 89-489 credits
to **65,012-78,226**, earned on unpinned local missions. gift-burst
recapitalization is moot for this path.

Deploying them is the campaign's actual payoff: each marketbot is named for a
system holding one of the nine strongholds and is excluded from `mb-fleet.yaml`
as "tasked for stations they can't reach yet". Nine dark markets, four of them
(korr/dross/mera + one) **never scanned at all**.
[[reference_pirate_base_registry]]

## The pin is the hazard — only 4 of 33 are pinned (SUPERSEDED, see above)

The whole chain is sold from **`treasure_cache_trading_post` alone** (giver Dara Kesh;
`across_the_line` and `supply_run` exist at no other base). But `station:` is a
standing order to travel there NOW, and **the autopilot cannot survive a long trip**:
it tops the tank off at the ORIGIN only, then departs even when its own estimate says
the route needs more fuel than the tank holds (prints `WARNING: Not enough fuel!` and
jumps anyway); mid-route `autopilotRefuelIfNeeded` burns **cargo fuel_cells only**
(<10%) and **never docks to buy fuel between hops**. That is exactly how engineer-5
stranded on 2026-07-29. Measured 2026-08-11 vs `data/spacemolt-knowledge.db`, roster
distances to the giver are **3, 4, 13, 14, 16, 17, 24, 26 jumps** — pinning the tail
would repeat engineer-5 twenty times.

Wave 1 (pinned, 3 jumps): **miner-1, miner-2** (funded) + **marketbot_alhena,
marketbot_sheratan** (broke — see below). Everything else runs UNPINNED and stages in
place on local delivery/exploration, which is what the 40-worker mission-learn pool
already does safely.

## ⭐✅ 2026-08-12 PROVEN IN PRODUCTION: baseline 10 DOES admit you to a stronghold

marketbot_alhena flew 21 jumps to **Voss Redoubt and docked** — the campaign's
core premise, confirmed live rather than inferred. `market.db` then held **2,232
order rows for `voss_redoubt`**, a stronghold market the fleet had never had
current data for. Deployment needs no new machinery: the `unlock` role already
runs `update_market` hourly, so a graduate PINNED at a stronghold turns a dark
market into a reporting one. (`resident` cannot be used to move one — the
pin-travel path lives only in `mission.go`, so a resident just sits.)

**⚠️ PIN ALIAS DEFECT (open).** `mission.go:1144` compares
`st.Player.DockedAtBase == deps.HomeStation`, but `missionNavToBase` takes the
pin as a **POI** id while `DockedAtBase` reports the **BASE** id. For a
dual-named station those never match: alhena, docked at Voss Redoubt, logs
`returning to pinned station voss_redoubt (at "voss_redoubt_station")` on every
dry pass, re-travels to a POI it is already at, and never parks. Costs churn,
not function — capture is on the schedule regardless. Fix: resolve one id to the
other via `kb.GetBaseByPOI` (already used at `mission.go:1204`) in the arrival
check. Affects all 12 dual-named stations
([[reference_station_id_aliases]]).

## ⭐ THE BOOTSTRAP IS CIRCULAR — `a_word_in_private` is the ONLY way in

Every mission that grants smuggling XP is a smuggling courier (*Courier Run*,
*Special Delivery*, *Off the Books*, *Border Job*) and **the server gates all of
them at smuggling ≥ 1**: `skill_required: Smuggling missions require smuggling
level 1`. You need the skill to earn the skill. Verified 2026-08-12: of 78
smuggling-XP grants in the fleet logs, every one went to an agent that already
held the unlock.

**`a_word_in_private` ("A Word in Private", +500cr) is the break in the loop.**
It is not typed smuggling, so it needs no skill. Exactly the four wave-1 agents
that unlocked completed it, on 2026-08-11 between 12:32 and 12:58 — alhena,
miner-1, miner-2, sheratan — at treasure_cache under the `unlock` role, with no
special handling. Earlier takers: fighter-1, fighter-2, random-npc (2026-07-29,
mb fleet). It is repeatable across agents, four inside 26 minutes.

**So a pinned agent that is not progressing is usually just waiting for board
rotation, not stuck.** The board serves level-1-gated couriers most of the time;
the runner tries them, gets `skill_required`, marks them "already attempted this
session" (`pkg/worker/mission.go:932`) and moves on — noisy but self-limiting,
NOT a livelock. Rejection count is a good staleness signal: gliese_581 43,
pirate-11 42, pirate-14 41 (all still locked) vs miner-1 just 6 (unlocked).

Do NOT read "no acceptable missions" at treasure_cache as a defect.

## ⭐ THE CHAIN SUPPLIES ITS OWN CARGO — a thin wallet is NOT a blocker

**Operator-corrected 2026-08-11, and the code already agreed.** The mission provider
hands the goods over on accept: every `deliver_item` leg arrives fully stocked via
`ProvidedItems`, so `BuyQty = max(Qty - Provided, 0)` is **0** and the agent buys
nothing. `pkg/worker/mission_select.go` goes further — a smuggling mission that tells
you to source the contraband yourself is a **HARD REJECT at selection**, because
contraband has no sell orders on any market and such a run could never be completed.

So a wallet only ever pays **fuel** on this path. Do not read "deliver_item" as
"purchase" — I did, and wrongly declared the nine marketbots blocked. It also means
explorer-1's "15,135cr thin-wallet canary" framing was never testing the binding
constraint.

**Remaining real blocker:** two agents at 0 fuel that cannot move at all — miner-10
(0/130) and pirate-3 (0/150). Both hold credits (8k / 111k), so a station refuel
clears it.

## Recapitalization: `scripts/gift-burst.py` (SHIPPED `a1b89787`)

Not needed for the chain, but useful fuel headroom for staging wave 2.
`scripts/gift-burst.py --fleet data/overmind/unlock-fleet.yaml` selects everyone under
20k, resolves usernames from `credentials.json`, and emits a ready play_as script
(leads with `dock`, ends with `quit`). Sized 2026-08-11: **15 recipients × 50,000 =
750,000**, ~3.8% of johnny_cab's 19.96M.

**Sender = johnny_cab**, but it is SUPERVISED (shuttle fleet, single agent) — stop
that overmind first, which beats the SIGSTOP dance because it removes the 90s
`SilenceTimeout` pressure. It sits AT Fortress Blackthorn undocked, so a bare `dock`
suffices, no travel. Recipients need not be present: a credit gift lands in the
wallet. Verify from the SENDER (`credits_sent`, get_status deltas) — recipient
credits in the status file are cached and lag badly.
[[reference_send_gift_and_play_as_mechanics]]

## Not in the pool, on purpose

**random-7** shares one game account ("Agent McAgentFace") with **random-6**, which is
actively mission-running in mission-learn. Tolerable when random-7 only captured; two
workers both FLYING one account is the `session_replaced` thrash that got random-7
omitted from mission-learn originally. Needs its own account first. Also out: databot
(standalone via `cmd/databot`), craftsman-1 (already unlocked; overrides sidecar owns
it), pirate-6..10 (hunt fleet), explorer-3/engineer-2/engineer-5 (mission-learn), and
the two spent empire-crimson homoglyph test accounts.

## NOT YET DEPLOYED

The live `idle` overmind still runs `--fleet data/overmind/idle-fleet.yaml`, a path
that no longer exists — harmless while it runs (contents are in memory) but it will
**fail on SIGHUP** and it is still spawning `--role idle`. Relaunch:

```
rm -f data/overmind/unlock.sock && nohup ./bin/overmind \
  --socket data/overmind/unlock.sock --fleet data/overmind/unlock-fleet.yaml \
  --worker-bin bin/worker --status-file data/overmind/unlock-status.json \
  --history-file data/overmind/unlock-history.jsonl \
  --assets-db-path data/assets.db --stagger 10s \
  >> data/overmind/unlock-overmind.log 2>&1 &
```

Kill the old `idle.sock` overmind first. Note `bin/overmind-status` reads a fixed
`defaultSources()` list — add `unlock-status.json` there or the pool is invisible on
:8087 ([[reference_overmind_launch_commands]]).

**Watch for completion via the ledger:** `capture_faction` writes `agent_standings`,
so `select count(distinct player_id) from agent_standings where faction like 'pirate_%'
and baseline >= 10` is the campaign's progress counter (7 at start).

Related: [[reference_pirates_standing_key_drift]] (the same commit fixed a 4th drift
site — `smugglingUnlocked` could never return true) ·
[[reference_ship_jump_time_and_fuel_formulas]] · [[project_fleet_role_interchangeability]]

## ⭐🔴 2026-08-23: THE CAMPAIGN WAS STALLED — 15 OF 17 WORKERS RAN UNPINNED

Counter still **26 of 161**, unmoved since 08-21. Diagnosis: of the 17 live unlock
workers, **15 had `station: ""`**. The yaml header already states the rule
(measured 08-13: *pinned at the giver 2/2 unlocked; unpinned 0/45 unlocked*), but
the active roster lines had lost their pin — only `trader-10` still carried one,
and it was a QUEUED-NOT-OWNED line running in haul.

Evidence it is the pin and not the board: over 7 days the pool accepted
**exploration 96 / delivery 51 / smuggling 19**, and all 19 smuggling missions
belonged to just **hauler-0 (13) and miner-3 (6)** — the two with any smuggling
skill (L5 and L3). The other 15 have **zero** smuggling XP. `Categories` is a
pure allowlist (`slices.Contains`, no ranking), so an unpinned worker simply
takes whatever its LOCAL board offers, and nothing on a local board routes it to
the giver.

**Chain facts corrected from mission_templates:** `a_word_in_private` is
`type: delivery` with **no `chain_next`** — a standalone job worth 50 smuggling
XP, NOT the chain head. The chain is `no_questions_asked`(100 XP) ->
`across_the_line`(200 XP) -> `an_introduction`, and **all three have empty
`requirements`**. Nothing gates an agent except standing at the giver.
Treasure Cache is `is_stronghold=0` (Frontier, police 30), so a baseline -30
agent can dock there — no chicken-and-egg.

### 7 of 17 were fuel-dead at 0 credits — fixed

pirate-4 0/200, pirate-1 1/90, miner-3 2/150, pirate-14 2/150, miner-8 3/150,
pirate-3 4/150, pirate-2 5/280 — every one with **exactly 0 credits** and a
bounty (834-6,138). Property tax drains idle ships, and a broke agent cannot buy
fuel to earn. Fixed by gifting **100,000 cr each from hauler-0** (which held
19.5M of the pool's 20.05M). **`refuel` then worked on credits alone — 534 to
1,650 cr for a full tank**; no fuel cells needed at these stations (the
`no_fuel_cells` error is station-specific, not the general case). All 17 now
have fuel; 100k is far more than needed but buffers the weekly tax.

### ⚠️ PIN ON BURN RATE, NEVER ON TANK SIZE

Picking the six biggest tanks for wave 1 selected the six most strand-prone
agents. Real numbers (`ceil(scale^1.5 x speed)` vs BFS jumps to treasure_cache):

| hull | fuel/jump | tank | max jumps |
|---|---|---|---|
| threshold | 1 | 95 | 95 |
| cobble/drillship/prospector | 2 | 100-130 | 50-65 |
| **excavator** | **6** | 150 | **25** |
| deeprock_harvester | 6 | 200 | 33 |

pirate-3 (excavator, 26 jumps) needs **156 against a 150 tank — a guaranteed
strand**, exactly the engineer-5 repeat the header warns about. `sparrow` and
`shadow_dancer` are **absent from the ships catalog** (the known erasure
pattern) — treat unknown hulls as unpinnable.

**WAVE 1 PINNED 2026-08-23** (all 1-2 fuel/jump, current fuel already exceeds the
whole trip): prophet-1(17j, needs 17) miner-6(17, 34) miner-5(18, 36)
pirate-5(23, 46) pirate-13(24, 48) hauler-0(24, 48). Applied via `kill -HUP` on
the unlock overmind — 6 specs updated and relaunched, 17/17 healthy, no restarts.

Held back: the excavator/deeprock agents (miner-3, miner-8, pirate-3, pirate-4,
pirate-12, pirate-14, pirate-15, pirate-11) and the two uncatalogued hulls. They
need either a closer staging point or a hull swap before they can be pinned.
miner-3 carries 40 `fuel_cell` (bought 08-23) — the ONLY mid-route top-up the
autopilot can use, so cargo fuel cells are the lever for the 6/jump hulls.

Do not trust status-file credits when checking any of this:
[[reference_worker_heartbeat_credits_stale]]

### ⭐🔴 A FULL CARGO HOLD SILENTLY BLOCKS THE WHOLE CHAIN

pirate-13 and pirate-5 reached treasure_cache_trading_post, docked, refuelled to
130/130 — and then logged `missions: no acceptable missions on this board` every
pass, even though `get_missions` there plainly lists
`No Questions Asked - (no_questions_asked) - Chain Mission` plus a dozen
smuggling couriers worth 50-250 smuggling XP.

Hand-accepting revealed the real reason:

    no_cargo_space: Not enough cargo space. This mission provides items that
    require more cargo

Both were **100/100 full of mined ore** — they are mining hulls that had been
working. `no_questions_asked` HANDS you contraband (starshine), so a full hold
refuses it, and the selector reports the generic "no acceptable missions"
instead of the cargo reason. **Arriving at the giver is not enough; the hold
must have room.**

Fix used (deposit, never sell — the ore is worth keeping and one hold held 6
`energy_crystal`, a drone-refit Phase 1 input):

    play_as <agent>
      deposit <item> <qty>        # one per stack, until the hold is clear
      accept_mission no_questions_asked

Both then accepted: *"Good. The starshine is in your hold. Take it to the Grand
Exchange at Haven"*. **First two agents onto the chain this campaign.**

**Check `cargo_used` before pinning a mining hull.** The other wave-1 agents
(miner-5 1/100, miner-6 1/100, prophet-1 7/165, hauler-0 11/75) had room and
needed nothing. Any future wave drawn from miners will hit this.

### ⭐🟢 2026-08-23 20:00 — FIRST CHAIN COMPLETIONS OF THE CAMPAIGN

pirate-13 and pirate-5 both **completed `no_questions_asked`** (+800 cr each,
exactly as advertised, +100 smuggling XP), and prophet-1 reached the giver and
**picked the chain up BY ITSELF** — proof the automation works end-to-end once an
agent is pinned AND has a free hold. The worker resumes a hand-accepted chain
mission correctly (`missions: resuming held <id> (No Questions Asked) ->
grand_exchange_station`), so a manual accept is a safe kickstart.

Caveat seen immediately after: with chain mission 1 done, both workers took a
profitable Federation Trade Run rather than the next link (`across_the_line`,
also sold only at the giver). They are pinned, so they should return — but the
selector does NOT prioritise the chain, and that is the thing to watch on the
next pass. If the chain stalls again with agents standing at the giver, the fix
is selector priority, not location.

Livelock cleanup the same evening: **30 agents freed** (29 batched + random-5),
0 failures, verified by "no `held for next pass` line in either fleet for 2+
minutes". mission-learn 40/40 and unlock 17/17 healthy after.

## 2026-08-24 — WAVE 1 GRADUATED 6/6, AND THE ROTATION IS NOW THE CAMPAIGN

Every pinned wave-1 agent completed the full chain, all within ~4h of reaching
the giver: pirate-13 22:17, prophet-1 22:30, pirate-5 22:34, miner-6 23:11,
miner-5 23:23, hauler-0 23:53 (local). Server payload on `an_introduction`
carries `reputation_changes` of **+2 with all nine pirate factions** and the
text "enough standing ... to dock at their strongholds freely". Confirmed in
use: trader-10 logs `haul: pirate unlock held; stronghold routes are in play
this pass` and routes to Korr Fortress Station.

**Rewards are badly short-paid** (empire-treasury shortfall): `across_the_line`
paid 226-2,330 of 2,500. Irrelevant — we are buying reputation, not credits.

### The `no_cargo_space` trap (cost hours on 08-23)

A pinned agent at the giver logged `missions: no acceptable missions on this
board` while the board plainly listed the chain. The real error only appears on
a hand-accept: `no_cargo_space` — `no_questions_asked` HANDS you contraband, so
a full hold refuses it. Both agents were 100/100 of mined ore. **The selector
swallows the cargo reason and reports the generic "no acceptable missions".**
DEPOSIT, do not sell (one hold had 6 energy_crystal, a drone-refit input).

### Unpinned is measurably worthless

On 08-24 the 11 unpinned unlock natives burned **54,272 route-finds and 53,639
`no acceptable missions` board scans in one day** and unlocked nothing. Running
tally: **pinned 8/8, unpinned 0/45.**

### The rotation, and why it was stalled

haul<->unlock rotation is automatic (`bin/fleet-secondment --watch 5m`): a
hauler self-nominates, the reconciler swaps it in, and returns it once the
assets ledger shows the unlock. It broke on the yaml-comment bug —
[[reference_secondment_overrides_are_removed_sets]].

Two throughput limits found:
- **`--max-in-flight` defaults to 1**, so 18 nominated haulers queue behind one
  slot. At one graduate per slot that queue is months long.
- **Seconded agents arrived UNPINNED** (`station: ""`), i.e. into the 0/45 case.

Fixed 08-24: all 18 rotating entries pinned to `treasure_cache_trading_post`.
Safe because the hull attrition ([[project_haul_fleet_hull_attrition]]) left
them on scale-1 starters at **1-2 fuel/jump**; measured worst case is
salvager-9, 26 jumps x 2 = 52 fuel of a 120 tank. trader-7 was already AT the
giver; trader-8 has 15 of 90 and must refuel first.

**Do NOT pin the 11 unlock natives.** They are excavator/deeprock/prospector
hulls at **6 fuel/jump**, 18-26 jumps out: need 108-156 fuel against 150-200
tanks, i.e. arriving on fumes with no reroute margin. **miner-8 cannot make it
at all — 156 needed on a 150 tank**, the exact engineer-5 stranding. They need
cargo fuel_cells first (the only mid-route top-up). pirate-1 (`sparrow`) and
pirate-2 (`shadow_dancer`) are absent from the ships catalog, so their burn rate
is unknown — treat as unpinnable until the catalog has them.

### Operator actions still pending

    kill -HUP <unlock-overmind-pid>   # starts trader-1, now pinned
    kill -HUP <haul-overmind-pid>     # starts hauler-0, home since 07:35Z
    # then relaunch the reconciler with a wider slot:
    kill <fleet-secondment-pid>
    nohup ./bin/fleet-secondment --watch 5m --max-in-flight 6 \
      >> data/overmind/secondment-daemon.log 2>&1 &

Operator decision 08-24: **a short-term haul plateau is worth the long-term
safety and route access.** Note haul is already over-provisioned — 21 haulers
saturate the fat tier at 34.4% of predicted [[reference_haul_fleet_capacity_ceiling]] —
so dropping to ~15 should cost far less than proportionally.
