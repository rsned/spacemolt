---
name: project_contraband_pipeline_resume
description: "RESUME POINT 2026-10-24 19:40 PDT for the contraband production pipeline: plans 1 (code) and 2 (rollout) are DONE and merged (main e96429e5); the moonshine line is live end to end; plan 3 (dashboard) not started. Live state, open watchers, known gaps, exact next steps."
metadata:
  type: project
---

**Where we are (2026-10-24 19:40 PDT, main = e96429e5, pushed):**
- Design: docs/superpowers/specs/2026-10-24-contraband-production-pipeline-design.md (D1–D9, approved).
- Plan 1 (worker/fleet code) merged d761593d: verbs/flags in [[reference_pipeline_worker_verbs_and_flags]].
- Plan 2 (rollout runbook) executed and merged: docs/superpowers/plans/2026-10-24-contraband-pipeline-rollout.md.
  SDD ledgers with every ruling: `.superpowers/sdd/2026-10-24-contraband-pipeline-rollout/progress.md` (git-ignored).
- Plan 3 (dashboard, D7) NOT written. The 24-h day-one metrics (produced = Σ `faction_pull: moved` in
  craft-overmind.log; sold = shopkeep re-posts after a disappearance + `market_orders.my_quantity` drops for
  marketbot_alhena/marketbot_treasure_cache) are due ~2026-10-25 19:00 PDT — record them on the status board.

**Live state to expect on resume:**
- 11 overminds, all post-merge binary. crft-mining (12 workers): miningbot_0000–0003 ice (interim secure sites
  Frontier icecap_drift / Altais / Deep Range → gifts to trader-9 at frontier_station / deep_range_outpost),
  miningbot_0020–0025 plasma gas at Timberline → alpha_centauri_colonial_station, trader-9 (resident at
  treasure_cache_trading_post; hourly deliver water_ice from frontier_station / deep_range_outpost / treasure_cache →
  voss_redoubt craftsman-10; hourly backhaul pirate_moonshine voss → marketbot_treasure_cache), trader-10 (resident at
  voss; daily buy_directed culture at Sirius to self + deliver to craftsman-10).
- Unlock rotation: wave 1 = miningbot_0004–0013 under the unlock fleet; 16 nominated in order 0014–0019, 0000, 0001,
  0020–0025, 0002, 0003; daemon `bin/fleet-secondment --watch 5m --home crft-mining … --away unlock … --max-in-flight 17`
  (17 = 10 bots + 7 seconded craftsmen; Away() counts ledger-wide — LOWER by 1 per craftsman returning).
  chlorine (0004–0011) + titanium (0012–0019) + spares (0026–0030) sit in crft-mining-overrides `removed` until
  graduation; their final pipelines (Glenhaven gas cloud / Ashmark reef → Treasure Cache → trader-9) are already in
  the yaml. Ice bots' FINAL pipelines (Glenhaven/Enif → Treasure Cache) must be edited in when they graduate.
- Crafters: craftsman-8 (Officer) `faction_pull` + `consign pirate_moonshine:marketbot_alhena:50`; craftsman-10
  `faction_pull` + `consign pirate_moonshine:trader-9:100`; both ten_minutely `craft distill_moonshine 60`.
  Faction stock at session end ≈ 1,100 ice / 250 culture → runs out in hours; then `cannot_craft` noise until the haul
  links land ice (trader-9) and culture (trader-10).
- Shopkeepers: marketbot_alhena (Voss) + marketbot_treasure_cache, `shopkeep pirate_moonshine:3000:1500`; state in
  data/agents/<id>/shopkeep.json. craftsman-10's unmanaged ask 50 @3,000 at Voss (44 left, 6 filled) and trader-8's
  72 @5,500 at Treasure Cache (order 3667a747) are still on the exchange. trader-9 holds ~164 moonshine at Voss
  awaiting its backhaul.
- miningbot_0000 ≈ 9,6xx credits (under the 10k floor; fuel spend) — fine.

**Known gaps / next code work (plan 3 or a 1b):** deliveries land in craftsman-10's PERSONAL storage so craftsman-8
starves once faction stock is gone → need a deliver/deposit mode targeting faction storage; haul loop loops forever on
`contraband_restricted` (treat like goods-gone); scanner priced a route with no backing bid; shopkeep untested
branches; fit-miningbots.sh SIGPIPE edge; stop conditions are manual (dashboard phase 3 automates).

**Gotchas learned this session:** 4001 does NOT park a worker (freeze ONLY the worker ≤60 s for play_as, never the
overmind >90 s); new yaml fields need an overmind RESTART not SIGHUP (pre-merge binaries drop them silently); a
subagent's kill/signal commands may be permission-denied → controller runs process ops; `ps|grep` self-matches its own
bash wrapper (anchor on the pid column); the mb overmind's args put `--fleet` before `--socket`; faction withdraw is
tick-deferred; one harvesting module per bot; Treasure Cache is the only ungated Empire contraband market.
