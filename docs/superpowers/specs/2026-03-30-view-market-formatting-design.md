# view_market Styled Output Formatting

**Date:** 2026-03-30
**Status:** Approved
**Author:** Claude + User

## Overview

Add styled table output to the `view_market` command in the `play_as` CLI tool. The formatter displays buy and sell orders in a compact, multi-row per item format.

## Problem Statement

The current `view_market` command outputs raw JSON, which is difficult to read when analyzing market data. Users need a human-readable table format that shows:
- Best and second-best buy orders (lowest prices)
- Best and second-best sell orders (highest prices)
- Station-sourced orders clearly marked
- Empty order books indicated

## Design

### Output Format

**Table Structure:**
```
Name                  | ID                    | Buy     | Qty  | Sell     | Qty |
----------------------+-----------------------+---------+------+----------+-----+
Prismatic Lens        | prismatic_lens        | * 8,800 |  248 | * 10,002 |   2 |
                      |                       | * 6,160 |  352 | * 10,102 |   7 |
Processed Null Matter | processed_null_matter |       1 | 1000 |        - |   - |
```

**Column Definitions:**
1. **Name** - Item display name (only shown on first row per item)
2. **ID** - Item identifier (only shown on first row per item)
3. **Buy** - Buy order price, prefixed with "*" if source="station"
4. **Qty** - Buy order quantity
5. **Sell** - Sell order price, prefixed with "*" if source="station"
6. **Qty** - Sell order quantity

### Display Rules

1. **Row 1 (mandatory):**
   - Columns 1-2: Item name and ID
   - Columns 3-4: Best buy order (lowest price) or "-" if none
   - Columns 5-6: Best sell order (highest price) or "-" if none

2. **Row 2 (conditional):**
   - Only shown if there is a 2nd buy order OR 2nd sell order
   - Columns 1-2: Blank
   - Columns 3-4: Second-best buy order or "-" if none
   - Columns 5-6: Second-best sell order or "-" if none

3. **Empty values:** Display "-" for missing buy or sell orders

4. **Station indicator:** Prefix price with "*" when order source is "station"

### Sorting Logic

- **Buy orders:** Sort by `price_each` ascending (lowest = best for buyers)
- **Sell orders:** Sort by `price_each` descending (highest = best for sellers)

## Implementation

### File: `cmd/tools/play_as/main.go`

**New Function:**
```go
// formatMarket formats a view_market response as a multi-row table.
func formatMarket(raw []byte) string {
    // Parse ViewMarketResponse
    // For each item:
    //   - Sort buy_orders by price ascending, take top 2
    //   - Sort sell_orders by price descending, take top 2
    //   - Build table rows
    // Return formatted table
}
```

**Integration Point:**
Add case to `formatStyledResponse()` function:
```go
case "view_market":
    return formatMarket(raw)
```

### Dependencies

- Uses existing `ViewMarketResponse` and `ViewMarketItem` types from `pkg/game/serverapi/types.go`
- Uses existing `MarketOrder` type for individual orders
- Follows pattern of other formatters (`formatCargo`, `formatStorage`, etc.)

## Edge Cases

1. **No orders for item:** Show "-" for both buy and sell columns
2. **Only buy orders, no sell:** Show buy data, "-" for sell
3. **Only sell orders, no buy:** Show "-" for buy, sell data
4. **Single buy order, multiple sell:** Show row 2 with blank buy, 2nd sell
5. **Multiple buy, single sell:** Show row 2 with 2nd buy, blank sell
6. **Orders without source field:** Display without "*" prefix
7. **Zero quantity orders:** Display normally (not filtered out)

## Testing Considerations

Test with:
- Items with no orders (empty arrays)
- Items with only buy orders
- Items with only sell orders
- Items with 10+ orders (should only show top 2)
- Items with mixed source types (station and non-station)
- Empty market response (no items array)
