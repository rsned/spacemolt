---
name: feedback_standing_rules
description: "Operator's standing rules for this repo — staging, verifying unpushed work, haul launch flags, power_cell sizing, the pre-commit race gate. Candidates for CLAUDE.md; kept here so they survive index edits."
metadata:
  type: feedback
---

Standing rules the operator has stated. These were inline in `MEMORY.md` until
2026-08-29; they belong in the repo's `CLAUDE.md` eventually (they are rules,
not facts), but that is a checked-in file and a separate decision.

- **Dirty `data/*.json` is churn.** Stage explicitly; never `git add -A`.
- **Verify unpushed work** with `git log origin/main..HEAD`, not by memory.
- **Haul MUST launch with `--stagger 10s`.**
- **power_cell run-size = arbitrage buy-order DEPTH**, not hold size — see
  [[reference_haul_fleet_capacity_ceiling]].
- **pkg/worker pre-commit race gate (300s) — RESOLVED 2026-09-08.** Worker is
  now ~12s under -race (template-clone KBs + the `settle` seam + the
  `Client.Close` mutex fix) — [[reference_race_test_cost_sqlite_migrations]].
  Let the hook run; `--no-verify` is no longer justified by test time.
- **Never route a pirate-LOCKED agent to a stronghold** (algol, zaniah, ...);
  unlocked (baseline >= 10) may go — [[feedback_stronghold_routing_requires_pirate_unlock]].
- **Tankers have a built-in pump; a bought hull is NOT boarded until `switch_ship`**
  — [[reference_assist_tanker_migration]].
- **Memory is mirrored into the repo at `docs/memory/`** (since 2026-08-29,
  `838ce625`). After writing or editing memories, run `make memory-sync` and
  commit `docs/memory` explicitly so the backup and changelog stay current.
- **Bump `BuiltForAPIVersion` / `VersionID`** whenever response structs or
  command signatures change for a new server version — [[feedback_version_constant]].

**Why:** each of these was learned by breaking something (a 6.5h unbatched
DELETE, a fleet-wide login block, a stale binary on 21 haulers).
**How to apply:** treat them as preconditions, not suggestions; if one seems
wrong for the case at hand, ask the operator rather than skipping it.
