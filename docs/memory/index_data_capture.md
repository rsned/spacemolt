---
name: index-data-capture
description: Data capture gaps: silent data loss modes, stale status files, ship_modules, asset/storage/action-log capture, wildlife observations
metadata:
  type: reference
---

# Data loss / capture gaps

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🔴 [Six ways we silently drop data](reference_capture_loss_taxonomy.md) — 18 KB tables have NEVER held a row; every mode looks like success. Detector spec'd, NOT built
- ⭐🔴 [Stale status files read as LIVE](reference_fleet_status_fossil.md) — `fleet-status.json` 17 days dead; `mining-status.json` is the same now
- ⭐🔴 [ship_modules has 0 rows, always](reference_ship_modules_never_captured.md) — we cannot see what is FITTED; hull capacity is the only proxy
- [capture_* drop crew/boarding stats](project_assets_personnel_capture_gap.md) — LATER TASK 08-30: agent_hulls/agent_stats/faction_profile columns spec'd; do before Layer B
- ⭐ [Fleet asset snapshots](project_fleet_asset_snapshots.md) — `agent_ships` = 0 rows ever; storage last captured 07-02
- [Worker fleet never wires WireStorageCapture](project_worker_storage_capture_gap.md) — scheduling view_storage on a role stores nothing
- [Action-log capture](project_action_log_capture.md) — 2 canaries live, fan-out pending
- [Per-death loss capture](project_per_death_loss_capture.md) — hull + manifest + cause + insurance; FOLLOW-UP, so detour/PvP losses are measurable
- [Survey anomaly capture](project_survey_anomaly_capture.md) — persist survey_system anomaly hints to the anomalies table
- [12 unregistered wildlife species + open battle API](reference_unregistered_wildlife_from_battle_api.md) — Kiln-Snail@furud is Phase-Lurker-common; Bulwark/Wormwood ARMED; api/battles needs no auth, category=pvp untapped
- ⭐🔴 [Wildlife 10-14: ids are not all `crt_` (leviathans are `lev_`), hull varies per INDIVIDUAL and GROWS](reference_wildlife_update_20261014_lev_ids_variable_hull.md) — the prefix gate silently discarded every leviathan scan; never gate capture on an id prefix; count individuals not rows
- ⭐ [wildlife_observations: per-individual, per-tick (migration 62)](project_wildlife_lifecycle_observations.md) — PK (creature_id, game_tick) collapses the double-look; count DISTINCT creature WITHIN a stage; stage ''=MATURE; order by hull, never a hardcoded ladder; 0 of 1,971 seen growing
- [Wildlife combat intelligence](project_wildlife_combat_intelligence.md) — capture wired `3dea78d7`, NOT scheduled; Leviathan kills a starter hull in 2 ticks
