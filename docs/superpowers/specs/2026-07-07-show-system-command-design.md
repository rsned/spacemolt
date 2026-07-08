# `show_system <id>` play_as helper — Design

**Date:** 2026-07-07
**Status:** Approved, ready for implementation plan

## Problem

`get_system` shows the **current** system by hitting the game server. We have
detailed data on *remote* systems too — accumulated in `knowledge.db` from
exploration and catalog imports — but no play_as command to inspect what we
know about a system by id without traveling there.

## Goal

Add a local play_as REPL command:

```
show_system <id>
```

that renders what the knowledge base holds for the system `<id>`, in a layout
that mirrors `get_system` but enriched with the extra data we store (per-POI
resources, station services, star/planet class, hidden flag, and data
freshness). It is a pure KB read — **no server round-trip, no tick cost.**

## Non-Goals (YAGNI)

- No SpaceBase / facility / market drill-down for station POIs (services tags
  only).
- No JSON output mode. `find_item` — the closest sibling — prints directly to
  stdout and ignores the format flag; `show_system` matches that.
- No fetching / refreshing from the server. If the KB has nothing, we say so.

## Command & Dispatch

- New dispatch case in `cmd/tools/play_as/main.go`, alongside `find_item` /
  `plan_route`:
  ```go
  case "show_system", "show-system":
      return runShowSystem(ctx, parts[1:])
  ```
- Add `"show_system"` to the completer command list in `completer.go`.
- New file `cmd/tools/play_as/show_system.go` holding `runShowSystem` and its
  helpers.

Signature: `func runShowSystem(ctx context.Context, args []string) error`.
It uses the package globals `globalKB` (reads) and `globalClock` (current tick
for freshness) — the same globals `nearest` uses — so no `client` param is
needed.

## Data Reads (all from `globalKB`, `pkg/knowledge`)

- `GetSystem(ctx, id) (*System, error)` — name, empire, `PoliceLevel`,
  `SecurityStatus`, description, `Connections` (`[]SystemConnection`, fields
  `SystemID` + `Distance` only — **no name**), `LastVisitedTick`,
  `LastUpdatedTick`, and the `Visited()` method (true iff `LastVisitedTick > 0`).
- `GetPOIs(ctx, id) ([]POI, error)` — each POI's `Name`, `ID`, `Type`, `Class`,
  `Services []string`, `Resources []game.POIResource` (fields `ResourceID`,
  `Richness`, `Remaining`), `Hidden bool`.
- `GetSystems(ctx) ([]System, error)` — loaded once to build an `id -> name`
  map. Used to label connection rows (which carry only ids) and, on the
  not-found path, to compute suggestions. A connection to an id absent from the
  map renders with the id in the name slot.

## Output Format

```
Nexus Prime (nexus_prime) | Solarian
Security: 3 - high_sec   | Visited (tick 48213, ~2 hours ago)
A bustling core-world hub.

Connections:
  Sol       | sol        | 4 LY
  Procyon   | procyon    | 7 LY

POIs:
Name          | ID        | Type      | Class  | Resources / Services
---------------------------------------------------------------------
Nexus Station | nexus_stn | station   |        | refuel, repair, market
Asteroid Belt | belt_a    | asteroid  |        | iron(0.8), copper(0.5)
Alpha Star    | star_a    | star      | G2 V   |
```

Rules:

- **Header:** `Name (id) | Empire` (empire title-cased, like `formatSystem`).
- **Freshness line:** right-hand side of the security line.
  - Visited (`Visited()` true): `Visited (tick <LastVisitedTick>, <formatAge(now - LastVisitedTick)>)`.
  - Not visited: `Unexplored (map-import only)` — and the security value is
    suffixed `(untrusted)` because `PoliceLevel` is a map-import default until a
    real visit. This matches the semantics noted at `main.go:5340` and the
    `System.Visited()` contract.
  - `formatAge` and `globalClock.Tick()` are reused from `nearest.go`.
- **Description:** printed on its own line when non-empty.
- **Connections:** name / id / distance, `(none)` when empty. The name is
  resolved from the `id -> name` map built from `GetSystems`; a connection whose
  target isn't in the KB shows its id in the name slot. Distances are the
  `SystemConnection.Distance` in LY, same as `formatSystem`.
- **POIs table:** columns Name, ID, Type, Class, and a combined
  "Resources / Services" column, each width-aligned to the widest cell.
  - The last column shows **resources** (`id(richness)`, comma-joined, richness
    trimmed via the existing `trimFloat`) when the POI has any; otherwise its
    **services** (comma-joined). Asteroids/planets fall to resources; stations
    to services. Empty when neither.
  - `Class` is blank for POIs without one (stations).
  - Hidden POIs get a ` (hidden)` suffix on the Name cell.
  - `(none)` when the system has no POIs.

## Not-Found Handling → error + suggestions

If `GetSystem` returns `nil` / error (unknown id):

```
System "nexis_prime" not found in knowledge base.
Did you mean: nexus_prime, nova_terra?
```

- Load all systems once via `GetSystems(ctx)`.
- `suggestSystems(query string, systems []knowledge.System) []string` returns up
  to 3 candidate ids, ranked by: exact-substring match on id or name first, then
  Levenshtein distance ≤ 2 against id and name. Case-insensitive. Self-contained
  and pure (no I/O) so it is directly unit-testable.
- If there are no candidates, omit the "Did you mean" line.

## Error / Edge Cases

- Missing arg: `usage: show_system <id>`.
- `globalKB == nil`: `show_system: knowledge base not available`.
- `globalClock == nil` or a zero/negative age: `formatAge` already returns
  `"unknown"` for negative ticks; guard the tick read so freshness degrades to
  `Visited (tick <n>)` without an age rather than crashing.
- System exists but `GetPOIs` errors: render the header/connections and print
  `POIs: (unavailable: <err>)` rather than aborting the whole command.

## Testing (`show_system_test.go`, no network)

Renderer is factored so the KB reads and the string building are separable:
a pure function
`renderSystem(sys *knowledge.System, pois []knowledge.POI, nameByID map[string]string, nowTick int64) string`
is what tests assert on (`nameByID` is the connection-label lookup).

- `renderSystem` table cases: visited vs unexplored (untrusted security),
  empty POIs, empty connections, a POI with resources, a POI with services,
  a hidden POI, empty description.
- `suggestSystems` cases: exact substring, typo within Levenshtein 2, no match
  (empty result), ranking/limit to 3.

## Files Touched

- `cmd/tools/play_as/show_system.go` (new) — `runShowSystem`, `renderSystem`,
  `suggestSystems`.
- `cmd/tools/play_as/show_system_test.go` (new).
- `cmd/tools/play_as/main.go` — one dispatch case.
- `cmd/tools/play_as/completer.go` — add `"show_system"` to the command list.
