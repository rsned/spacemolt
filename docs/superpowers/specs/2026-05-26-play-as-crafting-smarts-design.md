# play_as Crafting Smarts — Design

**Date:** 2026-05-26
**Status:** Approved, ready for implementation plan
**Scope:** Two new REPL commands in `cmd/tools/play_as` plus a new `pkg/craftplan` package

## Problem

`play_as` already exposes the live `craft <recipe-id> [qty]` action and a `recipes` query, but the operator has no help deciding **what** to craft or **how to close the gap** to a target recipe. The crafting DB ships a pre-computed `bill_of_materials` (18,881 rows) and the server's `get_recipes` returns inputs + `required_skills` per recipe — both already in the codebase but unused by the REPL.

The user has 168,863 items spread across four stations and a deep recipe graph. Manually figuring out "what can I build right now" or "what am I missing for X" against that surface is impractical.

## Goals

1. **`craftable`** — list every recipe the operator can build right now from cargo + current-station storage, skill-gated and station-legal.
2. **`plan <id> [qty]`** — gap analysis: what the recipe needs vs. what the operator has; print the literal `craft` command when ready.
3. Both commands should work without any new server API; rely on existing `get_recipes` + `get_cargo` + `view_storage` + `view_faction_storage`.
4. Keep the logic out of `cmd/tools/play_as/main.go` (already huge) and unit-testable without a live client.

## Non-Goals (v1)

- Multi-target planning (`plan recipe1 5 recipe2 10`).
- Market-aware profit hints (margin, ROI sorting).
- Persistent watches ("notify me when storage hits buildable for X").
- Cross-station / global inventory rollup. The in-game `craft` command only consumes local materials; v1 mirrors that.
- Per-alternative recipe selection in `plan` (`--via <recipe-id>`). v1 picks the lowest-skill alternative for chained crafts.

These are explicit YAGNI cuts. They can layer cleanly on the v1 API.

## Architecture

### New package: `pkg/craftplan`

Pure logic — no dependency on `pkg/game.Client` concrete types. The boundary makes it testable with a synthetic `Source` and keeps `play_as` honest about what it's calling.

```go
package craftplan

// Source provides every fact the engine needs. play_as wires a real
// implementation backed by game.GameClient + *sql.DB; tests use fakes.
type Source interface {
    Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error)
    Inventory(ctx context.Context, includeFaction bool) (Inventory, error)
    Skills(ctx context.Context) (map[string]int, error)
    CurrentStationID(ctx context.Context) (string, error)
    IllegalAt(ctx context.Context, stationID string) (map[string]bool, error) // recipe_id → true
    BOM(ctx context.Context, recipeIDs []string) (map[string][]BOMRow, error) // recipe_id → flattened
}

type Inventory struct {
    Cargo, Storage, Faction map[string]int // item_id → quantity
}

type BOMRow struct {
    BaseItemID  string
    Quantity    int
    RecipePath  []string // ordered recipe_ids, from recipe_path JSON
}

type Engine struct{ src Source }

func (e *Engine) Craftable(ctx context.Context, opts CraftableOpts) ([]CraftableRow, error)
func (e *Engine) Plan(ctx context.Context, opts PlanOpts) (*PlanResult, error)
```

### `cmd/tools/play_as`

Adds a thin source adapter (`playAsSource`) wrapping the existing `globalClient` + a `*sql.DB` opened against the crafting DB (same path resolution as `bulk-buy-order`). Two new REPL command handlers (`case "craftable"`, `case "plan"`) parse flags via `parseFlagArgs` (now safe under `flagInt`/`flagString`) and call into the engine. Rendering uses `text/tabwriter` matching the existing `formatNearby` / `formatMarket` style.

### Data sources, by purpose

| Purpose | Source | Notes |
|---|---|---|
| Recipe catalog + direct inputs + `required_skills` | Server `get_recipes` | Authoritative. Cached per-session keyed by agentID; `--refresh` invalidates. |
| Current materials | `get_cargo`, `view_storage`, `view_faction_storage` | Fetched each invocation. Faction is opt-in via `--include-faction`. |
| Player skill levels | `client.GetState().Player.Skills` | Already kept fresh by the client. |
| Recursive BOM | Crafting DB `bill_of_materials` | Only needed for `--reachable`. Missing DB degrades gracefully. |
| Station legality | Crafting DB `illegal_recipes` | Joined against current station id. |
| Item names / categories for display | Crafting DB `items` | Optional polish; recipe.Name from server is the primary label. |

If the crafting DB is missing or unreadable, both commands still work in their default (direct) mode. `--reachable` returns a clear `BOM unavailable: <reason>. Install/update the crafting DB or omit --reachable.` and exits the REPL command with no panic.

## Commands

### `craftable`

```
craftable                              # default: immediately buildable, compact table
craftable --reachable                  # include recipes reachable via intermediate crafts
craftable --category Weapons           # substring filter on recipe category (case-insensitive)
craftable --search lance               # substring filter on recipe.Name and output item names
craftable --include-faction            # also count faction storage as available materials
craftable --detail                     # per-recipe drill-down, no table
craftable --detail --recipe X          # detail for one specific recipe regardless of buildable
craftable --refresh                    # bypass session recipe-catalog cache
craftable --max N                      # cap table at N rows (default 100)
```

**Default sort:** `can_make DESC, depth ASC, recipe_id ASC`. With `--reachable` this means a `direct` row beats a `+1 crafts` row at the same `can_make`. Without `--reachable` every row is `direct` so the depth tier is a no-op.

### `plan`

```
plan <recipe-id> [qty]                 # default qty=1; direct ingredients gap
plan <item-id>   [qty]                 # resolves item_id → recipe (lowest-skill alternative)
plan <id> 10 --reachable               # flat base-material shortfall via BOM
plan <id> --include-faction
plan <id> --detail                     # already detailed; --detail also pulls intermediate paths
```

**Ambiguity:** if `<id>` matches both a recipe_id and an item_id, the recipe wins. Documented in `--help`. If an item has multiple recipes (BOM `has_alternatives = 1`), pick the lowest `max(required_skills.values)` recipe; ties broken alphabetically by recipe_id.

## Algorithms

### 1. Direct buildable (default)

For each recipe `r` in `get_recipes()`:

1. **Skill gate:** every `(skill, lvl) ∈ r.required_skills` must satisfy `player.Skills[skill] >= lvl`. Missing skill → treat as level 0. Fail → skip.
2. **Legality gate:** `r.id ∉ illegalAt(currentStation)`. Fail → skip.
3. **Material gate:** for each input `(item_id, qty)`:
   - `have = cargo[item_id] + storage[item_id] (+ faction[item_id] if --include-faction)`
   - `max_for_input = have / qty`
4. `can_make = min(max_for_input)` across all inputs. If `can_make ≥ 1`, include row.

Empty-input recipes (e.g. some refining starts): `can_make = math.MaxInt`, rendered `∞`.

### 2. Reachable buildable (`--reachable`)

Uses the crafting DB's `bill_of_materials` table.

1. Same skill + legality gates as direct.
2. Fetch BOM rows for the candidate recipe set in one query: `SELECT … FROM bill_of_materials WHERE target_id IN (?, ?, …) AND target_type = 'item'`. The DB also contains `target_type IN ('ship', 'facility')` rows for ship blueprints and facility construction — those are out of scope for v1 and explicitly filtered out.
3. Group by `base_item_id`; for each base material, `total_have = cargo + storage [+ faction]`; `max_for_base = total_have / qty`.
4. `can_make = min(max_for_base)` across all base materials.
5. Depth = count of unique recipe_ids parsed from the BOM `recipe_path` JSON column (de-duplicated). `direct` if equal to 1 and matches `r.id`; else `+N crafts` where N = depth - 1.

Recipes that pass the direct algorithm are tagged `direct`; reachable-only ones get the `+N crafts` tag. Both kinds appear in the table.

### 3. Gap analysis (`plan`)

**Direct mode:** walk `r.inputs`. For each:
- `need = qty * batches`
- `short = max(0, need - have_total)`
- Per-bucket display: `have_cargo`, `have_storage`, optional `have_faction`.

Footer:
- All `short == 0` → `✓ ready to craft` + `→ craft <recipe-id> <qty>`.
- Else → `✗ N input(s) short` with the largest offenders listed inline.

**Reachable mode:** walk BOM rows for `r.id`. Same per-row math but against base materials only. A second mini-table lists "intermediate crafts needed" derived from parsing unique `recipe_path` entries into `(recipe_id, primary_output_item_id)` via the catalog.

## Output Format

### `craftable` (compact)

```
12 recipes buildable at market_prime_exchange (cargo + storage; skill-gated; legal)

RECIPE                              OUTPUT                    CATEGORY            CAN_MAKE  TIME
─────────────────────────────────── ───────────────────────── ─────────────────── ────────  ────
alloy_titanium_ingot                titanium_alloy x2         Refining                  47    6s
assemble_advanced_repair_kit        advanced_repair_kit x1    Consumables               31   12s
…
(showing 12 / 12; sort: can_make desc. Pass --max N to widen, --detail to drill in.)
```

### `craftable --reachable`

```
38 recipes reachable at market_prime_exchange (12 direct, 26 via 1+ intermediate craft)

RECIPE                       OUTPUT                CATEGORY     CAN_MAKE  VIA         TIME
──────────────────────────── ───────────────────── ──────────── ────────  ──────────  ────
alloy_titanium_ingot         titanium_alloy x2     Refining           47  direct        6s
build_emergency_warp_device  warp_device x1        Equipment           4  +2 crafts    45s
…
```

### `plan <recipe-id> 5` (direct, shortage)

```
plan: assemble_advanced_repair_kit x5  (Consumables, 12s/batch)

ITEM             NEED    CARGO   STORAGE  SHORT
──────────────── ────    ─────   ───────  ─────
circuit_board       5        0         3      2 ✗
flex_polymer       15        0        20      –
titanium_alloy     15        2        18      –

summary: ✗ 1 input short (need 2 circuit_board)
hint:    plan circuit_board --reachable
```

### `plan <recipe-id>` (direct, ready)

```
plan: alloy_titanium_ingot x10  (Refining, 6s/batch)

ITEM        NEED   CARGO   STORAGE  SHORT
──────────  ────   ─────   ───────  ─────
iron_ore      30      12       450      –
titanium_ore  20       0       380      –

summary: ✓ ready to craft
→ craft alloy_titanium_ingot 10
```

### `plan <recipe-id> --reachable`

```
plan: build_emergency_warp_device x1  (Equipment, 45s)

BASE MATERIAL    NEED    CARGO   STORAGE  SHORT
─────────────── ─────   ─────   ───────  ─────
copper_ore         20       0       210      –
energy_crystal     16       0         8      8 ✗
exotic_matter       6       0         0      6 ✗
…

intermediate crafts needed (in order):
  1. assemble_capacitor_bank        x1   → capacitor_bank
  2. assemble_warp_coil             x1   → warp_coil
  3. build_emergency_warp_device    x1   → warp_device

summary: ✗ 2 base materials short  (need 8 energy_crystal, 6 exotic_matter)
hint:    craftable --reachable --search energy_crystal
```

### Rendering rules

- All tables go through `text/tabwriter`, matching existing `formatNearby`/`formatMarket`.
- No ANSI colors. `✗` / `✓` glyphs carry semantic weight.
- `--max N` is a hard cap; default 100 for `craftable`, no cap for `plan` (rows are bounded by recipe input count).

## Edge Cases & Error Handling

| Condition | Behavior |
|---|---|
| Crafting DB missing/unreadable | Direct mode works; `--reachable` prints `BOM unavailable: <reason>` and exits the command cleanly. |
| `get_recipes` errors or returns empty | Print server error; suggest "dock first" if not docked. No panic. |
| Player not in faction + `--include-faction` | Silent no-op; `have_faction` column blank. |
| BOM table missing rows for a recipe | Fall back to direct algorithm for that recipe; tag row `direct-only` in the `via` column. |
| Recipe ID typo on `plan` | `No recipe 'foo'. Did you mean: …` with up to 5 fuzzy matches (Levenshtein) drawn from the catalog. |
| `qty ≤ 0` or non-integer | `qty must be a positive integer (got: '…')`. Matches existing `craft` validator shape. |
| Recipe with zero inputs | `can_make = ∞`; show `∞` in the column; `plan` says `✓ ready` with `→ craft …`. |
| `--include-faction` but not docked at faction-aligned station | Treat faction storage as empty; no error. |

## Testing

### `pkg/craftplan` unit tests (no client, no SQLite)

- `engine_direct_test.go` — table-driven: empty inventory, exact-enough, abundant, skill-gated, illegal-at-station, ∞-input recipes, faction toggle.
- `engine_reachable_test.go` — fake BOM for ore → ingot → device chain; verify depth, `can_make` floor, intermediate parsing.
- `plan_test.go` — gap math: missing item, partial-have, faction-only, qty multiplier, ready-to-craft path.
- `tiebreak_test.go` — sort stability, `--max` cap, recipe vs item_id resolution, multi-recipe alternative selection.

### `cmd/tools/play_as` integration sanity

One glue test loads captured JSON fixtures (`get_recipes`, `view_storage`) into the adapter, runs `Engine.Craftable`, and asserts the compact-table output matches a golden file. Keeps the rendering path covered without spinning up a real client.

## File Layout

```
pkg/craftplan/
  source.go         # Source interface, Inventory/BOMRow types
  engine.go         # Engine, CraftableOpts/PlanOpts, public entry points
  direct.go         # Direct buildable + plan gap math
  reachable.go      # BOM expansion algorithm
  resolve.go        # Recipe vs item_id ambiguity, fuzzy-match suggestions
  format.go         # Compact + detail renderers (tabwriter)
  engine_direct_test.go
  engine_reachable_test.go
  plan_test.go
  tiebreak_test.go
  testdata/
    fixtures.json   # synthetic recipe + inventory snapshots
    golden_*.txt    # expected outputs

cmd/tools/play_as/
  craftable.go      # new file: REPL handler + playAsSource adapter
  craftable_test.go # one golden-file integration test
  main.go           # add 2 case branches dispatching to craftable.go
```

## Open Questions / Future Work

- **Profit hints** (Approach C extras): a `--profit` flag joining BOM costs with `market_prices` from the crafting DB would rank recipes by margin. Cleanly layers on as a new column; not v1.
- **Watch mode**: `craftable --watch` could re-run every N ticks and diff. Useful for long sessions but not v1.
- **Multi-target plan**: `plan a 5 b 10` summing the BOMs. Easy follow-up since the engine already returns plain data.
- **`--via <recipe-id>` for alternatives**: only relevant when `has_alternatives = 1` is common; revisit after observing usage.

## Approval

Approved during 2026-05-26 brainstorming session: scope (both commands), inventory (here-and-now + faction), depth (direct first, `--reachable` opt-in), skill gating (hide locked + hide illegal), output format (compact + `--detail`), architecture (`pkg/craftplan`), and all algorithm + edge-case decisions above.
