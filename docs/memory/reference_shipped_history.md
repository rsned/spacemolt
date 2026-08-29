---
name: reference_shipped_history
description: "Index of already-built Spacemolt features with no pending action — treasury/shuttle, market intelligence, fitting calculator, demand ledger, ToT engine, and the smaller shipped tools"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 7da8be40-9f9f-4a30-8440-d70ba696b4ee
  modified: 2026-07-27T02:32:48.075Z
---

Features that are BUILT and need no further work. Consolidated out of MEMORY.md's "Done / Built", "Thought Engine", and parts of "Workflow / Agents" so the live index stays small.

**Why:** these lines were ~1.9KB of the memory index while answering only "did we already do X?" — a question that doesn't need to be resident in every session.
**How to apply:** check here before building something that sounds familiar; each entry points at the full note.

## Major
- **Treasury buffer + shuttle role** — 5% profit → treasury + auto-rescue; BUILT 2026-06-28. → [[project_treasury_and_shuttle]]
- **Market Intelligence** — `pkg/market` is the single source of volatile market data; merged 2026-06-21/22. → [[project_market_intelligence]]
- **Ship fitting calculator** — `pkg/fitting` + `cmd/tools/fit`. TODO still open: validate ceil rounding vs the server. → [[project_ship_fitting_calculator]]
- **Demand ledger & report** — compact-capture 2026-05-30 (`03d8c93`); faction-storage is phase 2. → [[project_demand_ledger]]

## Thought Engine (Tree-of-Thought)
Merged 2026-03-18 — `pkg/tot/` 3-stage pipeline. Uses Ollama `/api/chat` + `format:"json"` (**NOT** `/api/generate`). → [[project_tot_prompt_improvements]] [[project_tot_next_steps]]

## Agent architecture (settled)
- **LLM rollout plan** — all agents eventually ToT/LLM; currently only miner-1. → [[project_llm_rollout]]
- **agent-server is deprecated** — every feature migrated to spacemolt-server; don't add to it. → [[project_server_consolidation]]

## Smaller shipped tools
passenger [[project_passenger_feature]] · POI merge provenance [[project_poi_merge_provenance]] · survey anomaly capture [[project_survey_anomaly_capture]] · `faction_list --seed` [[project_faction_list_shuffle]] · reconnect wake-on-input [[project_reconnect_wake_on_input]] · faction info backfill [[project_faction_info_backfill]] · play_as scheduler [[project_play_as_scheduler]] · mbox spam folder [[project_mbox_spam_folder]] · sparring testbed [[project_spar_testbed]] · find_item [[project_find_item_command]] · price [[project_price_command_depthwalk]] · `my_claims` (play_as, `7dd0723b` 2026-08-15) — an agent's held/recent arbitrage claims from market.db, marking expired holds and stronghold deliveries; backed by `Collector.GetOpportunitiesByAgent` (all statuses, vs GetClaimedByAgent's held-now). Claim rows outlive the worker, so this is how you read a stranded agent's last intent [[reference_gsa_ship_recovery]]
