---
name: project_empire_treasury_payout_collapse
description: Mission credits collapsed to ~37% of advertised from 2026-07-23 because the empire treasury ran dry (dev-confirmed); XP still paid in full. Realized-ratio gating shipped 3d66909.
metadata: 
  node_type: memory
  type: project
  originSessionId: 2671530b-761a-4dbe-b378-62f725016c20
  modified: 2026-08-10T03:46:42.542Z
---

**ROOT CAUSE (game dev, 2026-08-01, quoted): "The empire is apparently completely broke, and I did recently change something to prevent new money from being generated. The answer is that I probably need to raise taxes / tweak the economics."** So this was NEVER a client bug and never a mission-mechanics bug. Do not re-investigate it as one.

**The signature, and why it defeated five hypotheses.** Missions complete successfully, award the **FULL advertised skill XP**, and pay anywhere from 0 to 100% of the advertised credits. XP is free to mint; credits come from a purse that is empty. Ruled out with data before the dev answered: time-decay (a mission paid 100% at 11-15% of budget while another paid 0.6-45% at 5-6% — faster deliveries paid *worse*), partial delivery (payouts are not multiples of reward/qty — a qty-5 mission worth 2000 paid 59 and 7), confiscation (all-or-none per operator, and would fail the mission), shared-pool depletion (mission `53fb1030` paid all five agents the full 1400), and client mis-measurement (see the XP witness below).

**⭐ THE XP WITNESS — the technique that cracked it, reuse it.** `mission_templates.rewards_skill_xp` is the advertised XP; the server's `mission.completed` action-log entry reports the XP actually awarded. When those match EXACTLY — including multi-skill maps like `{exploration:70, navigation:55}` — the server demonstrably resolved the advertised template, so "different mission" / "partial completion" / "client misread the board" are all excluded, and any credit gap is real. Examples: Frontier Wayfinder Circuit advertised 20,000cr + `{exploration:70,navigation:55}` → paid **403cr** with that exact XP map; The Memorial 8,000 → **145**; Starfall Sector Survey 5,000 → **269**.

**⭐ `get_action_log` is the server's own audit trail and is far richer than the command response.** `mission.completed` entries carry `data.credits`, `data.skill_xp`, `items_received`, `reputation_changes`; `mission.accepted` carries `offer_id` (route-named board id) → `mission_id` (the `smuggling_courier_claim_*` INSTANCE id). Fetch with `get_action_log --page_size 100 --category mission --page N`. Reach it on a live fleet agent with the freeze dance (SIGSTOP worker → `bin/play_as <agent>` piped stdin ending in `quit` → SIGCONT); costs no restart. **Parsing gotcha:** the reply is a pretty-printed wrapper `{"entries":[...]}` — a naive brace-matcher consumes the outer object and finds zero entries; iterate `entries` instead.

**The drain curve (hand this to the dev; it dates the change and measures the rate):** % of advertised paid, by day — 07-21 **100%**, 07-22 **100%**, 07-23 84.8%, 07-24 64.9%, 07-26 64.9%, 07-28 50.9%, 07-29 62.6%, 07-30 41.6%, 07-31 **36.6%** (the floor), 08-01 **74.5%**. **Extended 2026-08-09 (supersedes the partial-day 08-01=42.4% first recorded here): 08-02 50.9%, [08-03..08-05 = the outage, no data], 08-06 69.8%, 08-07 62.8%, 08-08 59.0%, 08-09 48.4%.** So NO recovery and no further decay either — it oscillates ~48-70% with no trend, meaning the gate still does real work and the 1.0 fallback has not latched. Regenerate with: `sqlite3 data/market.db "SELECT date(finished_at) day, COUNT(*), SUM(expected_reward), SUM(credits_earned), ROUND(100.0*SUM(credits_earned)/SUM(expected_reward),1) pct FROM mission_results WHERE outcome='completed' AND expected_reward>0 AND template_id NOT LIKE 'procurement_%' GROUP BY day ORDER BY day;"** ⭐ FREIGHT IS IMMUNE — a shipper-escrow contract paid 10,000/10,000 on 08-09 [[reference_freight_unpriced_cargo_prime_gate]]. Full payment through the 22nd, first dip the 23rd. Scale: **427 underpaid completions, 15 agents, 964,366 credits short**, and it is NOT smuggling-specific — smuggling 363/564 underpaid, exploration 41/139, delivery 23/140 (all draw the same purse). Exclude `procurement_*` rows: pro-rata partial delivery is legitimate there.

**⭐ FIXED / SHIPPED `3d66909` — realized-ratio gating.** `Collector.MissionPayoutRatio(ctx, window)` (pkg/market/mission_payout_ratio.go) returns paid/advertised over completed rows in a 24h window; `buildMissionCandidate` takes a `payoutRatio` and scores `net = reward*ratio - itemCost - fuelCost`. **`c.Reward` deliberately keeps the ADVERTISED figure** — `mission_results.expected_reward` is the ratio's own input, so discounting it there feeds the ratio into itself and spirals.
- **The 1.0 fallback is load-bearing, do not "fix" it to be pessimistic.** Below `MissionPayoutRatioMinSamples`=8 the ratio reports 1.0 (face value). If the discount ever stops the fleet taking missions, sampling stops too — a pessimistic default would latch the gate off forever with no way to observe a recovery. Face-value fallback makes it re-probe as samples age out.
- **Smuggling below L3 is EXEMPT** (`smugglingBuyingXP`, mission_standing.go): XP is still paid in full and the level is the whole point (L3 unlocks the chain-2 reputation mission). Bounded instead by `missionSmugglingXPBudget`=25,000/agent. At L3+ smuggling joins realized-ratio gating. Operator intent: "once the server fixes the issue, smuggling will be just as good as delivery missions again."
- 🔴 `smugglingLoss` is IN-MEMORY and resets on every worker restart, so the 25k cap is per-SESSION not per-lifetime. Persisting it was flagged and deliberately not done.

**Cost while it lasted:** mission fleet earned 36,312cr against **60,999cr of fuel** in one 3h sample — net negative. Freight/haul/passenger revenue is player-sourced, NOT treasury-sourced, so it is unaffected — diversifying into cargo hedges this exact failure.

**⭐⭐ IT IS PER-EMPIRE, AND THE PAYER IS THE MISSION'S *ORIGIN* EMPIRE (measured 2026-08-09).**
The galaxy-wide daily average (~48-60%) is a MIX, not a haircut — the distribution
is strongly BIMODAL: over 08-06..08-09, 611 missions paid 99-100% and 224 paid
under 5%. What separates them is the empire whose board issued the mission:

| origin empire | n | avg % | paid full | paid ~nil |
|---|---|---|---|---|
| solarian | 248 | 79.2 | 179 | 30 |
| nebula | 532 | 73.5 | 331 | 65 |
| voidborn | 294 | 45.2 | 82 | 87 |
| **outerrim** | 112 | **27.8** | 12 | 40 |

Holds controlled for reward size (1k-2k band: solarian 79.1, nebula 69.7,
voidborn 39.3, outerrim 26.4), so it is not a reward-size confound. DESTINATION
empire shows no such effect — outerrim as a destination pays 90.2% while
outerrim as an origin pays 26.4% — which pins causation to the origin, i.e. the
purse you accepted from. Time of day is NOT a factor (47.7-84.7% by hour, no
periodicity).

Explains every extreme case: pirate-6 (outerrim, frontier_station) was paid
**23cr of 1,000 = 2.3%**; a forum bug reporter, also outerrim, got 0cr on one
mission and 53 of 5,500 on another.

**🔴 CONSEQUENCE FOR THE SHIPPED GATE:** `MissionPayoutRatio` (pkg/market/
mission_payout_ratio.go) is GLOBAL — no empire filter — so one blended ratio is
applied everywhere. It over-discounts solarian/nebula (the fleet refuses
missions that would pay) and under-discounts outerrim/voidborn (the fleet takes
missions that lose money). **Make the ratio per-origin-empire.** Route
mission-runners to solarian/nebula boards meanwhile.

Query to reproduce: join `mission_results.from_base_id` to `kb.bases.id` for
`empire` (ATTACH data/spacemolt-knowledge.db).

**🔴🔴 ORIGIN EMPIRE IS NOT THE WHOLE STORY — a controlled 4-agent run (2026-08-09
evening) shows enormous PER-COMPLETION variance inside one empire.** pirate-7..10
all ran `first_hunt_belt_grazers` (advertised 1,000cr), all accepted at the SAME
board (`mobile_capital`, outerrim), all completed at the SAME station
(`void_gate_outpost`), inside **4 minutes 40 seconds**:

| agent | completed (UTC) | credits | % |
|---|---|---:|---:|
| pirate-9 | 03:40:23 | 33 | 3.3 |
| pirate-7 | 03:41:53 | 10 | 1.0 |
| pirate-8 | 03:43:53 | 4 | 0.4 |
| pirate-10 | 03:45:03 | **851** | **85.1** |

XP was `{weapons:50, xenobiology:15}` — full — on all four, so the XP witness
excludes every "wrong mission / partial completion" reading. The empire table
above still holds in AGGREGATE, but it cannot explain 0.4% and 85.1% from one
board minutes apart. **Do not present origin empire as the discriminator for a
single completion** (the 08-09 forum reply does exactly that — its aggregate
claim is sound, its per-mission framing overstates).

**A drain-and-refill purse was hypothesised and REFUTED by these timestamps:**
the 33→10→4 descent looks like depletion, but the 851 followed the SHORTEST
gap of the four (70s), not a long one. Time-since-last-claim does not predict
the payout. Unexplained candidates not yet tested: claimant wealth (pirate-10
was much the poorest at 3,979cr vs 12.6k-22k) and plain per-completion
stochasticity consistent with the bimodal distribution. **n=1 on the recovery —
do not build a gate on either idea without more samples.**

**Watch for recovery:** the dev intends to raise taxes / retune. When the ratio climbs back toward 1.0 the gate reopens by itself. `missions: empire paying N% of advertised (M recent completions)` is the live read in the worker log.

Related: [[project_mission_learning_pool]] · [[project_smuggling_enablement]] · [[reference_empire_tax_day]]
