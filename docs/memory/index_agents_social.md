---
name: index-agents-social
description: Agents, play_as & social: bounties/tax, citizenship, send_gift, play_as usage/scheduler, passengers, treasury/shuttle, smuggling, missions, mbox, LLM/ToT
metadata:
  type: reference
---

# Agents, play_as, social

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🔴 [A BOUNTY IS UNPAID TAX](reference_tax_bounties_and_rates.md) — exactly the shortfall; weekly Sunday levy; Crimson 10% income/1% property
- [Bounties are not from combat](reference_agent_bounties_not_combat.md) — every 0-credit agent has one
- [Bounty auto-pays on entering the empire's space](reference_bounty_auto_pays_on_entering_territory.md) — trap: 0 credits → no fuel → stranded
- ⭐🔴 [Citizenship mechanics](reference_citizenship_mechanics.md) — `empire` is ORIGIN; crimson EXCLUSIVE, outerrim NOT; voidborn free
- ⭐ [send_gift + play_as mechanics](reference_send_gift_and_play_as_mechanics.md) — sender DOCKED, recipient = USERNAME, play_as needs `quit`
- ⭐ [Handoff gifts from STORAGE](reference_handoff_gifts_from_storage.md) — `send_gift source=storage` skips cargo; old handoff pass misread size-2 overflow as "storage empty"
- [play_as via go run](feedback_play_as_go_run.md) — always `go run ./cmd/tools/play_as`, never bin/
- [play_as scheduler](project_play_as_scheduler.md) — cron-lite hourly/daily/weekly commands
- [Passenger feature](project_passenger_feature.md) — carrying commands + formatters; RawCommand blocking change
- [Treasury + shuttle role](project_treasury_and_shuttle.md) — 5% faction deposit/rescue; shuttle canary pending
- [Smuggling enablement](project_smuggling_enablement.md) — treasure_cache tutorial chain, smuggling-2 courier gate, mission capture
- [Mission learning pool](project_mission_learning_pool.md) — phase 2: categories beyond delivery, exploration first
- [Mission category coverage](project_mission_category_coverage.md) — v1 = single-leg deliver_item only; trade is the biggest rejected bucket
- [Mission scan rate backoff](project_mission_scan_rate_backoff.md) — QUEUED: stop re-polling boards every tick when dry/parked
- [Captains-log task resume](project_captains_log_task_resume.md) — FUTURE: server-persistent notes so workers re-orient after restart
- [Action-log analyzer](project_action_log_analyzer.md) — FUTURE: event timeline from get_action_log
- [mbox spam folder](project_mbox_spam_folder.md) — per-agent blocklist for noisy senders
- [LLM rollout](project_llm_rollout.md) — only miner-1 on ToT today
- [ToT next steps](project_tot_next_steps.md) — prompt tuning, async planning, UI polish
- [ToT prompt improvements](project_tot_prompt_improvements.md) — short-term memory, role-aware context, KB integration
- [Shared knowledge dirs](project_shared_knowledge.md) — FUTURE: per-org shared directories instead of per-agent copies
- [Pairwise jump cache](project_pairwise_jump_cache.md) — FOLLOW-UP: all-pairs distance cache for routing
- [Stronghold reach page](project_stronghold_reach_page.md) — kb did-you-know page SHIPPED 07-26
