---
name: reference_v0572_boarding_personnel
description: Server v0.572.0 crew/marines/boarding/prizes — wire shapes, what the client+KB absorb (A+C, 2026-08-30), and the deferred command layer (B)
metadata:
  type: reference
---

**v0.572.0 (2026-08-30)** added crew + marines, boarding captures, and prizes.
Operator's call: absorb **A (client structs/pushes) + C (DB format)** first,
**B (the six commands) after A+C are rolled out**.

Wire facts (from `server_docs/openapi.20260830.json` + a live `get_ship`):
- `ship.personnel{fit_crew,fit_marines,injured_crew,injured_marines,version}` +
  `ship.crew_capacity`/`marine_capacity`; class gains `minimum_crew`,
  `capture_policy(+_reason)`, `latch_resistance`, `boarding_defense_bonus_pct`.
  Existing hulls were seeded at FULL complement (Survey Vessel 60/60, 6/6) —
  nothing is understaffed today; only combat casualties change that.
- Pushes: `ship_captured` (terminal boarding event, to captor+owner+bystanders),
  `prize_update` (private claimant), `personnel_update` (ally treated/moved
  our crew; carries our full personnel block).
- `battle_update.participants[].kind/is_npc` (`prize` = a moving intact hull),
  `boarding[]` alongside; `get_nearby.prizes[]/prize_count`;
  `get_status.prize_recoveries`; kill-log entries gain `cause`.
- New commands (NOT wrapped yet = Layer B): `recruit_personnel`,
  `treat_personnel`, `transfer_personnel`, `faction_personnel`, `claim_prize`,
  `service_prize`; `battle` gains `marines` + `board` stance + `self_destruct`.

Absorbed (A): `serverapi.Personnel`, Ship/ShipClass/PlayerStats/ShipModule
fields, three push structs + `State.LastCapture`/`LastPrizeUpdate`,
`personnel_update` applied to `State.Ship` only when `ship_id` matches,
`NearbyPrize`/`PrizeRecovery`, `BoardingStatus`, `CaptureLogEntry`; play_as
`status` header gets a Crew line when personnel is present.

Absorbed (C): `ships` catalog cols (`ensureShipClassPersonnelCols`, same
three-cohort pattern as prestige cols — see [[reference_ships_table_migration_trap]]);
migration 59 = `ship_captures` (PK boarding_operation_id, first observer
wins) + `seen_prize_events` (per-observation timeline, twin of
`seen_player_events`). Wired via `agent.WireBoardingObservers` (narrows Base
to `knowledge.BoardingRecorder`; MemoryKB/mocks → no-op) in worker, play_as,
auto-explorer. Catalog cols fill on the next `import-catalog-ships` run.

**New loss mode:** a boarding-fit hunter (MoltenOne-type, see
[[reference_moltenone_player_hunter_ip_block_kills]]) can now take a hauler
INTACT. `ship_captures` is where that shows up; `player_died` will NOT fire.

Deferred: B (commands + play_as handlers); a worker "treat injured crew when
docked at a medical station" idle step (needs B); `KillLogEntry.cause` has no
KB home until per-death loss capture exists ([[project_per_death_loss_capture]]).
`BuiltForAPIVersion` still v0.547.1 on purpose.


**v0.572.4 REMOVED the capacitor mechanic** — Ship.capacitor/max_capacitor,
absorbed with Layer A, were deleted again the next day. Struct + decode test
updated 2026-08-30. Ion weapons keep their shield bonuses; power fitting limits
unchanged. Also v0.572.2 removed pump/repair-arm THROUGHPUT limits (one
Refueling Pump now moves the full requested amount — tanker fleet upside) and
deleted the high-capacity pump/repair modules; v0.572.1 made shield-absorbed
hits casualty-free.
