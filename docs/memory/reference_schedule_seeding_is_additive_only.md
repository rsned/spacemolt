---
name: reference_schedule_seeding_is_additive_only
description: "Removing a command from roles.yaml does NOT remove it from agents — seeding is additive and RetireCovered only handles finer-frequency duplicates, so a withdrawn command fires forever"
metadata:
  node_type: reference
  type: reference
---

## The gap

`standing.go` seeds each agent's `schedule.json` from its role:

```go
for _, se := range role.Schedule {
    if covered(se) { continue }        // already have it -> skip
    deps.Scheduler.Add(se.Every, se.Command, now)
}
for _, t := range deps.Scheduler.RetireCovered() { ... }
```

`RetireCovered` retires only a task made redundant by a **finer-frequency
duplicate of the SAME command** (resident's `update_market` hourly ->
ten_minutely, 2026-08-13). **A command deleted from roles.yaml is never
retired.** It stays in `schedule.json` and fires forever.

**Proven 2026-09-12:** the three ore-conversion commands were removed from
`resident`/`resident_gas`/`resident_ice`/`miner` and both fleets were
restarted. `miner-9` and `marketbot_010` still carried
`craft refine_steel 500` etc. afterwards — ~276 failed requests/hour across 92
agents, indefinitely.

## ⭐ Why the obvious fix is WRONG

"Retire any task not in the role" would delete legitimate per-agent tasks.
`miner-9` carries `capture_action_log`, which is NOT in the `miner` role — it
is one of the action-log canaries. There is no provenance field distinguishing
role-seeded tasks from deliberately-added ones, so absence from the role cannot
mean "delete".

## Options

1. **Explicit `retire:` list in the role** — safe and declarative; the only
   way to express "stop doing this" without guessing.
2. **Track provenance** (seeded-from-role vs added) and prune only the former.
   More correct, more invasive.
3. **One-off**: edit the `schedule.json` files with the workers STOPPED. A
   running worker holds its own copy and rewrites the file (it updates
   `last_run`), so editing live is futile.

## Operational note

Check `data/agents/<id>/schedule.json` after any roles.yaml change — the file,
not the yaml, is what the agent actually runs. A new hourly task is also
stamped `last_run = created_at`, so its first run is a full hour after the
restart; see [[project_ore_to_component_conversion]].
