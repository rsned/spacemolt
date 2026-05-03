# `sellable` — sellability report for the docked station

Date: 2026-05-03
Status: Designed; awaiting implementation plan.

## Goal

Pair what the agent has on hand at the current docked station against that station's live market order book, and surface a single per-item report of "what can I sell right now, for how much." Today the operator has to mentally reconcile a long `view_storage` / `get_cargo` listing against a hundreds-of-rows `view_market` order book — the friction is high enough that profitable cargo sits in storage.

The report is **read-only and live**. v1 fetches fresh data on every invocation and prints; opt-in execution comes later.

## Scope

**In scope (v1):**

- New `play_as` REPL command: `sellable [--detail|-d] [--min-proceeds N]`.
- Inventory considered: ship cargo + station storage at the current docked station.
- Market considered: the current docked station's live `view_market` order book.
- Output honors the existing `--format styled|raw|json` selection.

**Out of scope (deferred to follow-ups):**

- Opt-in execution (e.g., `sellable --execute`). Will be its own brainstorm once v1 is shipped and we see how it reads.
- Remote storage aggregation across stations (`--include-remote-storage`).
- Cross-station market planning ("where should I take my legacy ore?") — that's a different feature and will need the SQLite KB.
- Listing-fee modeling for `auto_list=true` flows. v1 only models instant fills.

## Architecture and flow

The command lives in `cmd/tools/play_as/sellable.go` (separate file from `main.go`) and is wired into `executeCommand`'s switch. It's a thin orchestrator over a pure-function builder, which is what gets tested.

When invoked:

1. **Pre-check.** If `state.Doc == false`, fail fast with `"sellable: must be docked at a station with a market service"`. No network calls.
2. **Fetch (sequential, three sync queries).**
   - `client.GetListings(ctx)` — current station's full order book (`view_market` with no payload).
   - `client.GetCargo(ctx)` — ship cargo.
   - `client.ViewStorage(ctx)` — current station storage.
   Each is a no-tick query; total round-trip ≈ 3–6s. The first error aborts the command and surfaces verbatim — no partial reports.
3. **Build the plan** via `buildSellablePlan(market, cargo, storage)` — pure function, fully unit-testable without a network.
4. **Render** via the styled formatter or the JSON formatter, depending on `format`.

No KB writes, no command queue, no auto-execute.

## Approaches considered

- **Sequential calls (chosen).** Three sync queries. Linear code, consistent snapshot.
- **Parallel via `errgroup`.** ~1–2s instead of 3–6s. Rejected: added a concurrency pattern that doesn't appear elsewhere in `play_as` for marginal time savings on a one-shot interactive command.
- **Reuse cached `state.Ship.Cargo`.** Saves one round trip but cargo can be seconds stale (a `mine` mid-loop would skew the report). Rejected: the integrity-of-snapshot win from a fresh `get_cargo` is worth the extra ~1s.

## CLI surface

```
sellable [--detail|-d] [--min-proceeds N]
```

- `--detail` / `-d` — also print the per-buyer fill breakdown for every multi-buyer item. Off by default.
- `--min-proceeds N` — hide rows whose total proceeds are below `N`. Off by default. Applied after computation, so the row is correct first, then filtered.

The global `--format` flag (set via `set_format`) selects styled / raw / json.

### Default styled output

Sorted alphabetically by `item_id`. Every item with `cargo + storage > 0` is shown, including zero-sellable rows so the operator knows what to take elsewhere.

```
Sellable @ nova_terra_central — 13 items, est. proceeds 245,883 cr

ID                | Name           | Cargo | Storage | Sellable | Avg Price | Proceeds
------------------+----------------+-------+---------+----------+-----------+---------
aluminum_ore      | Aluminum Ore   |  4865 |    1000 |     4492 |     17.91 |  80,420
aluminum_sheet    | Aluminum Sheet |  1166 |       0 |     1166 |      8.50 |   9,911
carbon_ore        | Carbon Ore     |   539 |       0 |        0 |         — |       0
...
steel_plate       | Steel Plate    |     7 |       0 |        7 |     26.00 |     182
                                                                              ─────────
                                                                Total:        245,883
```

- `Sellable` is filled from **cargo only**. `sell` works against cargo; storage items must be `withdraw_items`'d first to be sold. The `Storage` column is informational so the operator can see what's available to withdraw.
- `Avg Price` is the proceeds-weighted average across the fills. `—` when nothing's sellable.
- Header line shows item count and total proceeds; footer repeats the total.

### `--detail` addendum

For each multi-buyer item, an extra block:

```
aluminum_ore — 4492 / 4865 sellable, 80,420 cr
   676 @ 26.00 = 17,576 cr
  1570 @ 20.00 = 31,400 cr
  2246 @ 14.00 = 31,444 cr
```

Single-buyer items remain inline in the main table; no extra block.

### JSON output

Structured plan, not a server response:

```json
{
  "station_id": "nova_terra_central",
  "item_count": 13,
  "total_proceeds": 245883,
  "items": [
    {
      "item_id": "aluminum_ore",
      "name": "Aluminum Ore",
      "cargo": 4865,
      "storage": 1000,
      "sellable_qty": 4492,
      "total_proceeds": 80420,
      "avg_price": 17.91,
      "fills": [
        {"price": 26.0, "qty": 676,  "proceeds": 17576},
        {"price": 20.0, "qty": 1570, "proceeds": 31400},
        {"price": 14.0, "qty": 2246, "proceeds": 31444}
      ]
    }
  ]
}
```

## Data shapes

### `view_market` (current station, all items, in one call)

```json
{
  "items": [
    {
      "item_id": "steel_plate",
      "item_name": "Steel Plate",
      "best_buy": 26,
      "buy_orders": [
        {"price_each": 26, "quantity": 676,  "source": "station"},
        {"price_each": 20, "quantity": 1570, "source": "station"},
        {"price_each": 14, "quantity": 2246, "source": "station"}
      ]
    }
  ]
}
```

The `source` field is intentionally ignored in v1 — fills walk strictly by `price_each` desc.

### `get_cargo` and `view_storage`

```json
{"items": [{"item_id": "steel_plate", "name": "Steel Plate", "quantity": 7, "size": 1}]}
```

`size` is captured but unused in v1 (the report is about counts and credits, not cargo-volume planning).

## Merge algorithm

**Inputs:** `market[item_id]`, `cargo[item_id]`, `storage[item_id]` — three maps.

**Inventory union.** Iterate `cargo` and `storage`, producing `inventory[item_id] = {name, cargo_qty, storage_qty}`. Name resolution: cargo's name first, fall back to storage's, then `market[item_id].item_name`, then the bare `item_id`.

**Per-item fill walk** — pure function, the testing centerpiece:

```
fillItem(cargo_qty, buy_orders) →
    sellable_qty, total_proceeds, avg_price, fills[]
```

1. If `cargo_qty == 0` or `buy_orders` is nil/empty → return zeros and an empty `fills`.
2. Stable-sort `buy_orders` by `price_each` desc. Ties keep the server's order.
3. Walk top-down. For each order: `take = min(remaining_cargo, order.quantity)`. If `take > 0`, append a fill `{price: order.price_each, qty: take, proceeds: take * price}`; subtract `take` from remaining; stop when remaining reaches 0.
4. `sellable_qty = sum(fills.qty)`, `total_proceeds = sum(fills.proceeds)`, `avg_price = total_proceeds / sellable_qty` (only when `sellable_qty > 0`).

**Sort and filter.** Sort the produced rows by `item_id` ascending. Apply `--min-proceeds` filter after the rows are built.

## Error handling and edge cases

- **Not docked** → fail fast before any network call.
- **Docked at a non-market station** (storage service but no exchange) → `view_market` returns empty `items` or an error. Surface `"sellable: no market at this station"`.
- **Empty inventory** (no cargo, no station storage) → print `(no cargo or storage at this station)`, no table.
- **Inventory exists, no matching market entries** → table renders with all rows showing `Sellable = 0`.
- **Defensive — orders with `price_each == 0` or `quantity == 0`** → silently skipped.
- **Defensive — duplicate `item_id` entries within `cargo` or `storage`** → quantities summed.
- **Any of the three fetches errors** → first error aborts; surface the underlying server error verbatim. No partial output.

## Testing

Tests live in `cmd/tools/play_as/sellable_test.go`.

**Pure-function layer (table-driven, the bulk of coverage):**

- single buyer, exact match (cargo == order qty)
- single buyer, cargo less than order qty
- single buyer, cargo greater than order qty (1 fill, leftover unsold)
- multi-buyer ladder, all consumed (cargo > total demand) — exercises sort + walk + weighted avg
- multi-buyer ladder, cargo exhausts mid-ladder
- ties at the same price preserve stable order
- zero cargo → empty result
- zero buy orders → empty result
- defensive: order with `price_each == 0` or `quantity == 0` skipped
- defensive: duplicate cargo / storage entries summed
- name fallback chain (cargo → storage → market `item_name` → `item_id`)

**Render layer.** Feed a fixed plan into the styled formatter; assert the output bytes — one snapshot for the default view, one with `--detail`. Same fixture into the JSON formatter; assert the structured shape and totals match.

**Integration / end-to-end.** Skipped for v1. The orchestrator is ~30 lines of "call three things then call the pure builder"; a fake `GameClient` scripting three responses is more harness than the code merits. Revisit if real logic ever lands in the orchestrator.

**Gates.** `go build ./...`, `go test ./cmd/tools/play_as/...`, `golangci-lint run ./cmd/tools/play_as/`.

## Future work

- Opt-in execute (`--execute` or post-report `y/n` prompt) — own brainstorm.
- `--include-remote-storage` aggregating storage across all stations the agent has stockpiles at.
- Cross-station market planner using KB-cached `market_snapshots` — separate feature.
- Cargo-volume awareness once we have a use case for it (the `size` field).
