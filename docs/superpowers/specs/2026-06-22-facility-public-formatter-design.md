# `facility list`: public_facilities + production-row facility_id / rent_per_cycle / owner

**Date:** 2026-06-22
**Status:** Approved design, ready for implementation plan
**Author:** Robert Snedegar (with Claude)

## Motivation

The server's `facility list` response gained a new top-level `public_facilities`
array — production facilities owned by factions that are open for public rental.
The play_as formatter does not render it today. Separately, the shared
production-facility table omits two fields the operator wants back: the
`facility_id` (needed to act on a specific facility) and the per-cycle rent
(`rent_per_cycle`) when the facility carries one.

This adds a Public Facilities section to `facility list` output and enriches the
shared production renderer so all three production tables (Station, Faction, and
the new Public) carry `facility_id` and `rent_per_cycle` where present, plus an
owning-faction column for the Public section.

## Context (current code)

- `facility list` responses are stored as raw JSON under the `"facility"` key
  (`pkg/game/client.go:~4378`); the play_as formatter unmarshals inline.
- `formatFacilityList(raw []byte) string` (`cmd/tools/play_as/main.go:2346`)
  renders three sections: Station services/production, player Personal
  facilities, and Faction services/production. There is **no** public_facilities
  handling.
- Production rows render through the shared `renderProductionFacilityTable`
  (`cmd/tools/play_as/main.go:2090`), fed `productionFacility{name, prod}`
  (`:2081`). Columns today: Name | Type(⚙ recipe) | Fee/hr | Output/run | Cycle
  tick/run | Run cost | Queued runs | Backlog ticks | Public. It carries
  **neither** `facility_id` **nor** `rent_per_cycle`.
- `facilityProduction` (`:2062`) models the nested `production` block.
- `factionFacilityRow` (`:2167`) already has `FacilityID`, `RentPerCycle`,
  `CustomName`, and `displayName()` (custom_name-aware). Faction production rows
  thus already have id + rent available upstream — they're just not rendered.
- `globalKB knowledge.Base` (`:47`) is reachable from formatters; play_as casts
  it to `*knowledge.SQLiteKB` elsewhere (e.g. `passenger_catalog.go:24`). It is
  **nil** when play_as runs without `--db-path`.
- The `factions` table (migration `add_faction_dashboard_tables`) has a `tag`
  column; `knowledge.FactionRecord.Tag` mirrors it. There is **no** lightweight
  id→tag getter — only the heavy `LoadFactionView`.

## Design

### 1. New KB method: lightweight faction tag lookup

Add to `pkg/knowledge` (on `*SQLiteKB`, beside the other faction-store methods):

```go
// FactionTag returns the tag stored for a faction, or ok=false if the faction
// is unknown or has no tag recorded.
func (kb *SQLiteKB) FactionTag(ctx context.Context, factionID string) (tag string, ok bool, err error)
```

Implementation: `SELECT tag FROM factions WHERE faction_id = ?`; map `sql.ErrNoRows`
and an empty/NULL tag to `ok=false, err=nil`. No `Base`-interface change — the
formatter reaches `*SQLiteKB` via the `globalKB` cast, matching the existing
passenger/demand pattern.

### 2. Owner resolver in play_as

A helper that turns a `faction_id` into a display string:

```go
// factionOwnerDisplay resolves a faction_id to "[TAG]" using the knowledge base,
// falling back to the raw faction_id when the KB is unavailable or the tag is
// unknown. Results are cached per call site to avoid repeat lookups.
```

- Cast `globalKB` to `*knowledge.SQLiteKB`; if the cast fails (nil/non-SQLite),
  return the `faction_id` unchanged.
- Use `context.Background()` for the local read (avoids threading `ctx` through
  the formatter dispatch).
- Maintain a `map[string]string` cache within the `formatFacilityList` call so
  each distinct `faction_id` is resolved at most once.
- Empty `faction_id` → empty owner string.

### 3. Extend `productionFacility` and the shared renderer

Extend the input struct:

```go
type productionFacility struct {
	name         string
	prod         *facilityProduction
	facilityID   string  // "" when unknown
	rentPerCycle *int64  // nil when the facility carries no per-cycle rent
	owner        string  // "" except for public facilities
}
```

`renderProductionFacilityTable` gains three **conditional** trailing columns,
each emitted only when at least one row in the table has a value for it:

- **Facility ID** — populated for every production facility, so it renders
  whenever any row has an id (the normal case).
- **Rent/cycle** — rendered when ≥1 row has a non-nil `rentPerCycle`; rows
  without one show blank in that column. (Faction production has it; Station and
  Public may not.) Reuse `formatCredits`.
- **Owner** — rendered when ≥1 row has a non-empty `owner`; only the Public
  section populates it, so Station/Faction tables stay visually unchanged.

Conditional columns keep the three call sites consistent through one renderer
while matching the per-table reality. Width calc and the two-line header/divider
extend to include whichever optional columns are active.

### 4. Call-site wiring

- **Faction production** (`renderFactionFacilities`, `:2210`): populate
  `facilityID: f.FacilityID` and `rentPerCycle: &f.RentPerCycle` (the
  `factionFacilityRow` already carries both; `name` stays `f.displayName()`).
  No owner.
- **Station production** (`:2516` area): populate `facilityID` from the station
  facility's `facility_id`; `rentPerCycle` only if the station facility carries
  one (else nil). No owner. Name prefers `custom_name` if the station facility
  has one, else `name`.
- **Public facilities** (new): decode `public_facilities` into a slice of a
  struct holding `name`, `custom_name`, `facility_id`, `faction_id`, `type`,
  `category`, `level`, `recipe_id`, and `production` (`*facilityProduction`).
  For each entry **with** a production block, build a `productionFacility{
  name: custom_name-or-name, prod, facilityID, rentPerCycle (if present),
  owner: factionOwnerDisplay(faction_id) }`. Entries without a production block
  are skipped from the production table (logged-free; YAGNI — the sample shows
  all public facilities are production).

### 5. New "Public Facilities" section in `formatFacilityList`

Rendered only when `public_facilities` is non-empty, **shown by default**
(independent of `--show_station_facilities`). It calls
`renderProductionFacilityTable` with heading `Public Facilities (N)`. Place the
section after the Faction section.

### 6. serverapi struct (completeness)

If `serverapi.FacilityListResponse` is consumed by any non-test code, add
`PublicFacilities []map[string]any \`json:"public_facilities"\``. If grep shows
it is unused for facility-list rendering, skip it (don't add dead fields).

## Testing

`cmd/tools/play_as/facility_format_test.go` (inline JSON fixtures):

- **New:** a `facility list` payload with a `public_facilities` array → output
  contains a "Public Facilities (N)" heading, the Facility ID column, the
  Rent/cycle column where present, and an Owner column.
- **New:** owner resolution — with `globalKB` set to an in-memory `SQLiteKB`
  seeded with a faction (`StoreFaction` with a `Tag`), the owner renders as the
  tag; with `globalKB` nil it renders the raw `faction_id`. Save/restore
  `globalKB` around the test.
- **Updated:** existing production-table tests
  (`TestFormatFacilityList_ShowsProductionDetails`,
  `TestFormatFacilityFactionList_ProductionSplit`) — expected output now includes
  the Facility ID and Rent/cycle columns for faction production rows.
- **New (KB):** a `pkg/knowledge` test for `FactionTag` — returns the tag for a
  stored faction, `ok=false` for an unknown id.

## Constraints

- `go build ./...` clean, `go test ./...` green, no new `golangci-lint` findings.
- `ticks_per_run` stays a `float64` (live server sends fractional values).
- Graceful degradation: no `--db-path` / nil KB must not break facility list —
  owner falls back to `faction_id`.

## Non-goals

- No new top-level command; `public_facilities` rides the existing `facility list`.
- No rent/economics calculation for public facilities beyond displaying the
  fields the server provides.
- No change to the Personal or Station-service (non-production) tables.
- No `Base`-interface change; the tag lookup lives on `*SQLiteKB`.
