# Facility Catalog — Design (Spec A1)

**Date:** 2026-07-09
**Status:** Design, pending user review → implementation plan

## Where this fits (the bigger picture)

This is **Spec A1** of a larger effort: an overmind **crafting brain** that takes a
target item/ship/facility, computes the full material breakdown, subtracts what we already
hold, and distributes the remaining mine / haul / craft work across the agent fleet
(miner-1..10, craftsman-1..10, haulers).

That effort decomposes into:
- **A1 — Facility Catalog (this spec):** a galaxy-wide, marketbot-populated store of every
  public **production** facility and the recipe it makes, queryable as *"which stations can
  craft recipe_id X, at what tier and fee?"*
- **A2 — The Planner (later spec):** read-only pipeline (BOM → subtract faction-storage
  inventory → site crafts using this catalog → emit a JSON step-DAG for review).
- **B — The Executor (later spec):** dispatch the plan across the fleet, dependency
  scheduling, live UI.

A1 is built first because facility siting is the planner's hardest knowledge gap, the
catalog is independently useful to any tool, and it is small and testable in isolation.

## Game mechanics this encodes

- Facility **types**: production, personal, faction, infrastructure. Only **production**
  facilities are relevant here.
- A **production facility makes exactly one recipe** (bound `recipe_id`). Making N different
  intermediates requires N different facilities — this is *why* crafting is distributed.
- Facility **level/tier multiplies output rate**: `output × 3^(level-1)`.
- **Public** facilities (including other factions') are usable craft-for-hire for a **fee**.
  Capturing them galaxy-wide turns "where do I craft X" into a lookup.

## What already exists (and the gaps A1 closes)

- The worker `facilities` command (`pkg/worker/dispatch.go:162`) already calls
  `facility list` and runs `KBUpdateFacilities` (`pkg/worker/capture.go:564`).
- `FacilityListResponse` (`pkg/game/serverapi/responses.go:882`) exposes a dedicated
  `PublicFacilities []map[string]any` section alongside station/player/faction sections.
- Facility instances carry `recipe_id`, `level`, `labor_cost`, `rent_per_cycle`.

**Gaps:**
1. `KBUpdateFacilities` parses station/player/faction facilities but **drops
   `public_facilities` entirely** — precisely the other-faction craft-for-hire rows we want.
2. There is **no store** keyed for a cross-station "recipe → public facilities" query
   (`base_facilities` lacks `public`/`fee`/`owner`, and is FK-coupled to an observed `bases`
   row).
3. Marketbots run `kb_update`/`update_market` but **not `facilities`** — no galaxy sweep.

## Design

### 1. New table `public_facilities` (knowledge DB)

A dedicated table, separate from `base_facilities` (which is docking-observation data,
FK-coupled to `bases`). This decouples capture from base existence and keeps the catalog's
query surface clean.

```sql
CREATE TABLE IF NOT EXISTS public_facilities (
    station_id     TEXT NOT NULL,
    facility_id    TEXT NOT NULL,          -- instance id
    recipe_id      TEXT NOT NULL DEFAULT '',
    facility_name  TEXT DEFAULT '',
    category       TEXT DEFAULT '',        -- production / ... (we store, filter to production)
    level          INTEGER DEFAULT 1,
    labor_cost     INTEGER DEFAULT 0,      -- craft-for-hire fee (see cost note)
    owner_faction  TEXT DEFAULT '',
    public         BOOLEAN DEFAULT 1,
    details_json   TEXT DEFAULT '',        -- raw captured map, forward-compat
    last_seen_tick INTEGER DEFAULT 0,
    last_seen_utc  TEXT DEFAULT '',
    PRIMARY KEY (station_id, facility_id)
);
CREATE INDEX IF NOT EXISTS idx_public_facilities_recipe ON public_facilities(recipe_id);
```

Next migration number in `pkg/knowledge/sqlite_migrations.go`. Upsert on
`(station_id, facility_id)` so a re-sweep refreshes level/fee/recipe/last_seen.

**Cost note:** `labor_cost` is the intended fee field, but the exact cost semantics of a
public production facility (labor_cost vs. an output price vs. a per-job fee) must be
**confirmed against a live `public_facilities` payload** — the very first implementation
step. `details_json` preserves whatever fields appear so nothing is lost if the fee lives
elsewhere.

### 2. Capture — extend the facility sweep

Extend `KBUpdateFacilities` (or add a sibling `KBUpdatePublicFacilities`) to also parse
`FacilityListResponse.PublicFacilities` and upsert production+public entries into
`public_facilities` via a new KB method `UpsertPublicFacilities(ctx, stationID, rows)`.
Must be robust when no `bases` row exists (no `GetBase` precondition on this path). Keep it
docked-guarded (marketbots idle docked, so `facility list` returns the station's set).

Filter: keep entries that are **production** category and **public** (from the
`public_facilities` section, and/or any section entry with a truthy `public` field).

### 3. Query API

New KB method:
```go
type PublicFacility struct {
    StationID   string
    FacilityID  string
    RecipeID    string
    Name        string
    Level       int
    OutputRate  int    // derived: baseOutput × 3^(level-1); baseOutput from recipe if known, else level as proxy
    LaborCost   int
    OwnerFaction string
    LastSeenTick int
}
func (kb *SQLiteKB) FacilitiesForRecipe(ctx, recipeID string) ([]PublicFacility, error)
```
Uses `idx_public_facilities_recipe`; returns newest-seen per station, production+public only.

### 4. Marketbot scheduling

Add the `facilities` command to the marketbot role's recurring schedule (the mechanism that
already schedules `kb_update`/`update_market` for the mb fleet — `data/overmind/roles.yaml`
and/or the marketbots' `schedule.json`). Cadence: hourly or a few-hourly is ample (facility
config changes rarely, unlike prices). All 43 station-parked marketbots then keep the
catalog fresh galaxy-wide.

### 5. Human query command

`where_facility <recipe>` (play_as) — resolves the recipe (id or name, reuse existing recipe
resolution), calls `FacilitiesForRecipe`, and prints a table: station · tier · output rate ·
fee · owner · staleness. Independently useful; is the Planner's future siting input.

## Out of scope (A1)

- The Planner (A2) and Executor (B).
- A facility-type → recipes catalog for non-production types (irrelevant — production
  facilities are 1:1 with a recipe).
- Cross-station personal/faction *storage* inventory (that's the Planner's concern; faction
  storage is already persisted).
- Deciding *which* facility to use among alternatives (Planner's siting heuristic).

## Risks / unknowns

- **Public-facility payload shape unconfirmed.** No live sample of the `public_facilities`
  section exists in `data/game-api/latest/`. First implementation step: capture one and
  confirm field names (`recipe_id`, `level`, `labor_cost`/fee, `owner`, `public`, category).
  The design's field mapping is provisional until then.
- **Coverage = where marketbots sit.** 43 stations are swept; facilities at unstaffed
  stations won't appear. Acceptable — the catalog reports what it knows; the planner flags
  unknowns. (A future enhancement could have other idle agents opportunistically sweep.)
- **Output-rate base.** `3^(level-1)` is the multiplier; the per-run base output comes from
  the recipe's `Outputs`. If unavailable at query time, expose `level` and let the caller
  compute, rather than storing a stale derived rate.
