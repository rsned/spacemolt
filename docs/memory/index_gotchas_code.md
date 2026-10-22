---
name: index-gotchas-code
description: Code & data gotchas: SimpleHandler drops pushes, rawjson keys, test traps, GameClock, play_as statusline, sleep constants, shared kb DB, deploy verification
metadata:
  type: reference
---

# Gotchas — code & data

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🔴 [SimpleHandler drops EVERY push](reference_simplehandler_drops_every_push.md) — combat/death/warnings decoded then discarded; invisible without set_debug. Fixed in play_as only (`fe600938`); 15 other binaries incl. the worker still blind
- ⭐ [storeRawJSON key drift](reference_rawjson_key_drift.md) — cache keys reachable only via the action switch die when a reply omits `action`. Audit the rest
- [Missions() vacuous-test trap](reference_missions_vacuous_test_trap.md) — bare `&game.State{}` early-returns before the code under test
- [GameClock drifts forward](reference_gameclock_forward_drift.md) — syncs FORWARD only; no tight timeouts from it
- [Exploration = content survey](reference_exploration_is_content_survey.md) — the map graph is fixed and known
- ⭐🔴 [play_as statusline's last field is the GAME TICK, not credits](reference_play_as_statusline_last_field_is_the_tick.md) — misread it 09-09 and planned two ship buys on imaginary money; agent_profile.credits also goes stale
- [Actual Sleep constants](reference_sleep_constants_actual.md) — CLAUDE.md's table is STALE
- [spacemolt-kb shares the SQLite DB](reference_spacemolt_kb_shared_db.md) — poi_metadata_planets/stars are owned by the kb repo
- [Ironlight Combine = the dev's faction](reference_ironlight_combine_dev_faction.md) — its charter describes our marketbot pattern
- [POI merge provenance](project_poi_merge_provenance.md) — weak scans no longer clobber rich data
- [Worker log tagging](project_worker_log_tagging.md) — `[worker:<id>]` v0.2.3 deployed; carrier-tier logging v0.2.4 committed, not deployed
- [Deploy verification](reference_deploy_verification.md) — SIGUSR1 drain ABORTS if workers stay busy; verify by process start time vs binary mtime
