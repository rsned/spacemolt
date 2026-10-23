---
name: index-overmind-ops
description: "Overmind, ops & deploy: launch commands, fleet-watch, rollout queue, quiesce/park, drain, dashboards, login limits, market.db prune, bulk deletes, capability ledger"
metadata:
  node_type: memory
  type: reference
  originSessionId: ae0c813d-9d8e-4b7b-a04f-87f7158b2502
  modified: 2026-10-23T02:01:01.006Z
---

# Overmind / ops / deploy

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- [Overmind launch commands](reference_overmind_launch_commands.md) — all fleets; `rm -f` stale socks; /proc scan not `pgrep -f`; relaunch market-prune too
- ⭐ [fleet-watch health watcher](reference_fleet_watch.md) — alerts on log SILENCE + daemon census; relaunch with the fleets. Logs ~20GB unrotated
- [Pending rollout queue](project_pending_rollout_queue.md) — what is deployed per fleet; haul is the one left behind
- ⭐🔴 [Park an agent: quiesce.json](reference_worker_quiesce_park.md) — survives restarts; a park does NOT free the game session (play_as needs roster removal + SIGHUP)
- [Graceful drain](project_overmind_graceful_drain.md) — SIGUSR1/USR2/TERM
- [Dynamic exit/join](project_fleet_pool_dynamic_membership.md) — MERGED `8016cd8`
- [Fleet manager](project_overmind_fleet_manager.md) — NEXT = Phase 2
- [Fleet role interchangeability](project_fleet_role_interchangeability.md) — operator vision 08-01: surge any agent into Haul; brainstorm first
- [Generalist agent selector](project_generalist_agent_selector.md) — LONG-TERM: every agent weighs all income paths per juncture
- [Idle-agent income paths](project_idle_agent_income_paths.md) — A mission-runner specced; B mining next; C combat deferred
- [Dashboard v1 on :8091](project_overmind_dashboard_v1.md) — pkg/ovdash; MERGED 07-21
- [System view](project_overmind_system_view.md) — click system → orbital view with live dots; LIVE
- [Activity line](project_ovstatus_activity_line.md) — per-agent current-activity sub-line
- [Dashboard task summary](project_overmind_dashboard_task_summary.md) — FUTURE per-worker task line
- [Fleet version visibility](project_fleet_version_visibility.md) — current/behind per build
- ⭐🟢 [Clerk-token login + connection gate (10-23)](reference_clerk_token_login_and_connection_gate.md) — one API key, player_id via clerk-sync, 60 dials/min host-wide, --stagger dead, 4001 parks, play_as lease
- [Login rate limits](reference_login_rate_limits.md) — per-IP per-minute on /login (password path; largely bypassed by Clerk tokens since 10-23)
- [Login vs reconnect gating](reference_login_vs_reconnect_gating.md) — BOTH fresh logins and reconnects are gated (history of the old ungated era inside)
- [market.db pruning](reference_market_db_prune.md) — UNSUPERVISED; died 07-15 → 62GB; retry fix `a474d01d` undeployed
- [Bulk-delete scale lesson](feedback_bulk_delete_scale.md) — NEVER unbatched DELETE on a huge table; 6.5h without committing
- [Verify regen with stash](feedback_verify_regen_with_stash.md) — swap old code in with git stash, never `git checkout`
- [Worker DB pool cap = NEGATIVE RESULT](reference_worker_db_pool_negative_result.md) — didn't cut CPU; real culprit was GetReferenceAsk
- [Agent capability ledger](project_agent_capability_ledger.md) — pkg/assets, 16 commits on feat/agent-capability-ledger, UNMERGED
- [Capability ledger slices 5–6](project_agent_capability_ledger_storage_faction.md) — storage + faction + ovdash panel; reviewed, unmerged
- [Ship role naming scheme](project_ship_role_naming_scheme.md) — FUTURE: overmind re-allocates idle agents by hull name
- [Server consolidation](project_server_consolidation.md) — agent-server features migrated into spacemolt-server; DONE
- [Agent empire-band numbering](project_agent_empire_bands.md) — trailing number = empire band
- ⭐🔴 [Restart deploys NOTHING without a rebuild](reference_rebuild_before_restart.md) — 169 workers sat 55 commits stale; SIGUSR1 drains (5min poll, never force-aborts); one fleet at a time; schedules ARE editable
