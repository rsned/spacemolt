# Customs map: learn where customs inspects, then route contraband around it

Date: 2026-11-23. Operator-approved approach: **B built as A first**: a record first, contraband routing second.

## Why

Customs positions were tracked by hand in memory notes, and that went wrong at least once: the "Alpha Centauri" patrol was actually in Mimosa. Server v0.734.1 (2026-11-23) now names the inspection system in every customs message. It also renamed the cutters by border sector and limited Crimson customs to the Rampart. The goal is for agents to work out from evidence where customs inspects and where it does not, and for contraband runs to avoid the inspected systems automatically, without hand-kept rules such as "Solarian via Timberline".

## What the data already says (recon 2026-11-23)

- `data/assets.db` `action_log_events` already holds server action-log events for about 70 players, via the scheduled `capture_action_log`. The relevant events:
  - `combat.customs_inspection` (1,175 events): `{system_id, poi_id, empire_id, outcome: clear|seized|evaded, officer_name, bounty}`.
  - `combat.customs_seizure` (125 events): `{system_id, empire_id, base_id, seized_items:["3x Void Dust",...], fine, total_value}`.
  - `navigation.jumped` (83k events): `{from_system, to_system, ...}`.
- Inspections happen in **19 specific systems**, not on every border crossing, and often some time after arrival (at a belt or station inside the system). The top ones are grumium 290, the_levy 264, gold_run 230, furud 110, the_experiment 75 and the_rampart 64. The unit is therefore **the system**, not the entry edge.
- Contraband is per item: KB `items.category = 'contraband'` (8 items). No per-empire legality field exists.
- Autopilot already supports a per-call system exclude set. `AutopilotDeps.ExtraAvoid` is merged with `avoid-systems.json`, and `avoidDetour` then BFS-detours around the set (`pkg/worker/autopilot.go:306-350`, `avoid_systems.go`). The hunt fleet already uses it for Lawless systems.

## Goals

1. **(A) Record.** A per-system customs picture, derived from data we already capture. Each system is classed as **monitored**, **observed clear** or **unknown**, with the evidence behind the class: arrivals, inspections, seizures, items seized and last inspection.
2. **(B) Route.** A ship carrying contraband plans its route around monitored systems.

## Non-goals

- No change for clean ships. The operator rule is that customs only matters for contraband, and a clean ship passes any customs.
- No new capture path. We do not add a jump-time hook in `Client.Jump`; the action log already records jumps and inspections.
- No edge-weighted pathfinding and no per-item legality model.

## Anti-goals (a failure even if it works)

- Contraband runs that fail outright because the avoid set is too broad. "Unknown" must not block routes.
- Stale evidence that keeps a system off-limits forever. For example, Crimson's windmere, blood_forge and frostfeld were inspected earlier on 11-23, before v0.734.1 moved Crimson customs to the Rampart only.

## Approaches considered

- **A only (record + report).** Cheapest, but routing stays manual.
- **B (record + contraband routing via the existing system-avoid seam). CHOSEN, built in two phases, A first.**
- **C (record + pre-flight warning only).** Safer, but someone still has to act on every warning by hand.
- Entry-edge model: we considered recording "entered empire X via system Y". The data showed inspections are tied to the system, not the crossing, and route planning can only exclude systems, so we dropped it.

## Design

### Phase A: customs map

- **`pkg/customs` (new):**
  - `Build(db, now, window, overrides) Map` reads `action_log_events` and returns one row per system: `{system_id, empire, arrivals, inspections, seizures, seized_items, last_inspection, class}`.
  - **arrivals** = `navigation.jumped` rows whose `to_system` is the system, within the window.
- **Classes:**
  - **monitored**: at least 1 `combat.customs_inspection` in the window, or forced on by an override. Ageing: a system is no longer monitored once it has `ClearAfterArrivals` (default 20) clean arrivals after its last inspection. Stale positions then clear without anyone editing a file, including cutters that v0.734.1 may have moved.
  - **observed clear**: arrivals ≥ `ClearMinArrivals` (default 10) and no inspection that still counts. A system that aged out of monitored is observed clear.
  - **unknown**: everything else.
- **Evidence that the system is the right unit (checked 2026-11-23):** pass-through ships are inspected. Traders, explorers and shuttles were inspected at grumium, the_levy and gold_run, often at the POI where they arrived, and most left the system within 10 minutes. These are not only miners working a belt.
- **Window:** default 14 days.
- **Overrides file `data/overmind/customs-overrides.json`:** `{"monitored": {"<sys>": "<reason>"}, "clear": {"<sys>": "<reason>"}}`. Operator knowledge beats evidence.
  - Seeded with `clear`: windmere, blood_forge and frostfeld ("v0.734.1: Crimson customs Rampart only").
- **Output `data/overmind/customs-monitored.json`:** `{"generated_at", "window_days", "systems": {"<id>": {"class", "empire", "arrivals", "inspections", "seizures", "last_inspection", "seized_items"}}}`. It is written atomically.
- **Command `cmd/tools/customs-map`:**
  - `customs-map build` writes the JSON.
  - `customs-map report` prints a table grouped by empire, showing the monitored systems and the observed-clear entries. It shows the free entries next to each empire's monitored ones, so "where is it NOT monitored" is answered directly.
- **Refresh:** hourly. It runs as a scheduled command on one always-on agent that already runs `capture_action_log`, as the dispatch verb `customs_map`. Following the house pattern (like `capture_intel`), this needs no new daemon.
- **Memory:** `reference_customs_mechanics.md` gains a pointer: "live truth = customs-map report", and the hand lists become history.

### Phase B: contraband routing

- **When:** at the start of every autopilot call (`autopilotOnce`, where `ExtraAvoid` is merged), if the ship's current cargo holds any item with KB category `contraband`.
- **What:** load `customs-monitored.json` (re-read per call, like `avoid-systems.json`) and add every **monitored** system to `ExtraAvoid` with reason `customs (contraband aboard)`. Observed-clear and unknown systems stay routable.
- **Exceptions:**
  - The origin system and the destination system are never added. We cannot avoid where we are or where we must go.
  - When the destination is monitored, log a line saying so, then proceed.
- **No route around them (operator decision 2026-11-23: fail and alert, keep the cargo):**
  - Autopilot returns a distinct error, `ErrNoCustomsFreeRoute`, wrapping `ErrRouteThroughAvoided`. The ship does not fly and keeps its cargo. Nothing falls back to the monitored route, and nothing unloads.
  - The worker logs one actionable line: `customs: no customs-free route to <dest> (monitored on every path: a,b,c)`.
  - The worker raises an operator-visible flag on the status/rescue view. It uses the existing worker-to-overmind signalling; the builder picks the lightest existing channel.
  - Hourly retries of the same task log quietly after the first failure, so the log isn't flooded.
  - The roughly 33 autopilot call sites need no per-role handling: they surface the error as a normal task failure.
- **Missing or unreadable JSON:** no change in behaviour, and one log line. This is the kill switch: deleting the file turns the feature off.
- **Log line on every contraband route:** `customs: contraband aboard (void_dust x3); avoiding N monitored systems: a,b,c`.

## Constraints

- Follow the CLAUDE.md sleep rule: no sleeps are added. Use the `settle` seam if any wait appears. pkg/worker must never call `time.Sleep`.
- Do not add a KB migration. The map is a derived JSON file, and assets.db is read only.
- Field names are as verified in the live `data_json` above. All values in `data_json` are strings.
- Pass `golangci-lint`, `go build ./...` and `go test ./...`.

## Left to the builder

- The exact SQL and Go shape of `Build`.
- Table formatting in `report`.
- Which agent runs the hourly `customs_map`. Pick an always-on CJ resident that already has `capture_action_log`, for example miningbot_0000 or craftsman-3.
- Test fixtures: an in-memory assets.db with synthetic events.

## Calls I made (operator may override)

- Classes are per system; there is no entry-edge model.
- The window is 14 days, `ClearMinArrivals` is 10 and `ClearAfterArrivals` is 20.
- Unknown systems stay routable.
- A monitored destination or origin is allowed through with a warning.
- The Crimson stale systems are seeded as `clear` overrides.
- Refresh is hourly via a worker verb.
