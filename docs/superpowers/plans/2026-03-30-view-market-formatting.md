# view_market Styled Output Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add human-readable table formatting to the `view_market` command in play_as CLI tool

**Architecture:** Add a `formatMarket` function following existing formatter patterns (formatCargo, formatStorage) that parses ViewMarketResponse JSON and renders it as a multi-row table showing best and second-best buy/sell orders.

**Tech Stack:** Go 1.24+, existing text/tabwriter package, existing ViewMarketResponse/ViewMarketItem types from pkg/game/serverapi/types.go

---

## File Structure

**Files to modify:**
- `cmd/tools/play_as/main.go` - Add `formatMarket()` function and integrate into `formatStyledResponse()`

**No new files created** - this is a pure feature addition to existing code.

---

### Task 1: Add formatMarket function signature and empty response handling

**Files:**
- Modify: `cmd/tools/play_as/main.go` (after formatStorage function, around line 360)

- [ ] **Step 1: Add formatMarket function with empty response handling**

```go
// formatMarket formats a view_market response as a multi-row table.
func formatMarket(raw []byte) string {
	var resp struct {
		Items []struct {
			ItemID       string `json:"item_id"`
			ItemName     string `json:"item_name"`
			BuyOrders    []struct {
				PriceEach float64 `json:"price_each"`
				Quantity  int     `json:"quantity"`
				Source    string  `json:"source,omitempty"`
			} `json:"buy_orders"`
			SellOrders []struct {
				PriceEach float64 `json:"price_each"`
				Quantity  int     `json:"quantity"`
				Source    string  `json:"source,omitempty"`
			} `json:"sell_orders"`
		} `json:"items"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Sprintf("Error parsing market data: %v", err)
	}

	if len(resp.Items) == 0 {
		return "No market data available"
	}

	// TODO: Build table
	return "Market formatting not yet implemented"
}
```

- [ ] **Step 2: Integrate into formatStyledResponse**

Find the `formatStyledResponse` function (around line 319) and add the case:

```go
func formatStyledResponse(raw []byte, command string) string {
	switch command {
	// ... existing cases ...
	case "view_market":
		return formatMarket(raw)
	// ... rest of cases ...
	}
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./cmd/tools/play_as/`
Expected: No compilation errors

- [ ] **Step 4: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat: add formatMarket skeleton and integrate with view_market"
```

---

### Task 2: Build basic table structure with headers

**Files:**
- Modify: `cmd/tools/play_as/main.go` (update formatMarket function)

- [ ] **Step 1: Update formatMarket to build table with headers**

Replace the TODO comment in formatMarket with:

```go
	import "text/tabwriter"

	// ... inside formatMarket after parsing ...

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)

	// Header row
	fmt.Fprintf(w, "Name\t| ID\t| Buy\t| Qty\t| Sell\t| Qty\t|\n")
	fmt.Fprintf(w, "----------------------+-----------------------+---------+------+----------+-----+\n")

	for _, item := range resp.Items {
		// TODO: Format item rows
		fmt.Fprintf(w, "%s\t| %s\t| TODO\t| TODO\t| TODO\t| TODO\t|\n", item.ItemName, item.ItemID)
	}

	w.Flush()
	return buf.String()
```

Add the import at top of file if not present:
```go
import (
	"text/tabwriter"
)
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./cmd/tools/play_as/`
Expected: No compilation errors

- [ ] **Step 3: Manual test**

Run: `./play_as view_market <item_id>`
Expected: Table with headers showing "TODO" placeholders

- [ ] **Step 4: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat: add table headers to formatMarket"
```

---

### Task 3: Format buy orders with sorting and station prefix

**Files:**
- Modify: `cmd/tools/play_as/main.go` (add helper function for formatting buy orders)

- [ ] **Step 1: Add helper to format and sort buy orders**

Add this function before formatMarket:

```go
// formatBuyOrders formats up to 2 buy orders with station prefix.
// Returns slice of formatted strings (price with prefix, quantity).
func formatBuyOrders(orders []struct {
	PriceEach float64 `json:"price_each"`
	Quantity  int     `json:"quantity"`
	Source    string  `json:"source,omitempty"`
}) []struct {
	price string
	qty   string
} {
	// Sort by price ascending (lowest first)
	sorted := make([]struct {
		PriceEach float64 `json:"price_each"`
		Quantity  int     `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}, len(orders))
	copy(sorted, orders)

	slices.Func(sorted, func(a, b struct {
		PriceEach float64 `json:"price_each"`
		Quantity  int     `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}) int {
		return cmp.Compare(a.PriceEach, b.PriceEach)
	})

	slices.SortFunc(sorted, func(a, b struct {
		PriceEach float64 `json:"price_each"`
		Quantity  int     `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}) int {
		return cmp.Compare(a.PriceEach, b.PriceEach)
	})

	// Take top 2
	result := make([]struct {
		price string
		qty   string
	}, 0, 2)
	for i := 0; i < len(sorted) && i < 2; i++ {
		prefix := ""
		if sorted[i].Source == "station" {
			prefix = "* "
		}
		result = append(result, struct {
			price string
			qty   string
		}{
			price: prefix + fmt.Sprintf("%.0f", sorted[i].PriceEach),
			qty:   fmt.Sprintf("%d", sorted[i].Quantity),
		})
	}

	return result
}
```

Add imports if needed:
```go
import (
	 cmp "cmp"
	 "slices"
)
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./cmd/tools/play_as/`
Expected: No compilation errors

- [ ] **Step 3: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat: add formatBuyOrders helper with sorting"
```

---

### Task 4: Format sell orders with sorting and station prefix

**Files:**
- Modify: `cmd/tools/play_as/main.go` (add helper function for formatting sell orders)

- [ ] **Step 1: Add helper to format and sort sell orders**

Add this function after formatBuyOrders:

```go
// formatSellOrders formats up to 2 sell orders with station prefix.
// Returns slice of formatted strings (price with prefix, quantity).
func formatSellOrders(orders []struct {
	PriceEach float64 `json:"price_each"`
	Quantity  int     `json:"quantity"`
	Source    string  `json:"source,omitempty"`
}) []struct {
	price string
	qty   string
} {
	// Sort by price descending (highest first)
	sorted := make([]struct {
		PriceEach float64 `json:"price_each"`
		Quantity  int     `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}, len(orders))
	copy(sorted, orders)

	slices.SortFunc(sorted, func(a, b struct {
		PriceEach float64 `json:"price_each"`
		Quantity  int     `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}) int {
		return cmp.Compare(b.PriceEach, a.PriceEach) // Descending
	})

	// Take top 2
	result := make([]struct {
		price string
		qty   string
	}, 0, 2)
	for i := 0; i < len(sorted) && i < 2; i++ {
		prefix := ""
		if sorted[i].Source == "station" {
			prefix = "* "
		}
		result = append(result, struct {
			price string
		qty   string
		}{
			price: prefix + fmt.Sprintf("%.0f", sorted[i].PriceEach),
			qty:   fmt.Sprintf("%d", sorted[i].Quantity),
		})
	}

	return result
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./cmd/tools/play_as/`
Expected: No compilation errors

- [ ] **Step 3: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat: add formatSellOrders helper with sorting"
```

---

### Task 5: Wire up formatters in main table building logic

**Files:**
- Modify: `cmd/tools/play_as/main.go` (update formatMarket to use helpers)

- [ ] **Step 1: Update formatMarket table building to use formatters**

Replace the table building loop in formatMarket with:

```go
	for _, item := range resp.Items {
		buys := formatBuyOrders(item.BuyOrders)
		sells := formatSellOrders(item.SellOrders)

		// Row 1: Best buy and sell
		buyPrice1, buyQty1 := "-", "-"
		if len(buys) > 0 {
			buyPrice1 = buys[0].price
			buyQty1 = buys[0].qty
		}

		sellPrice1, sellQty1 := "-", "-"
		if len(sells) > 0 {
			sellPrice1 = sells[0].price
			sellQty1 = sells[0].qty
		}

		fmt.Fprintf(w, "%s\t| %s\t| %s\t| %s\t| %s\t| %s\t|\n",
			item.ItemName, item.ItemID,
			buyPrice1, buyQty1,
			sellPrice1, sellQty1,
		)

		// Row 2: Second best buy and sell (if exists)
		if len(buys) > 1 || len(sells) > 1 {
			buyPrice2, buyQty2 := "-", "-"
			if len(buys) > 1 {
				buyPrice2 = buys[1].price
				buyQty2 = buys[1].qty
			}

			sellPrice2, sellQty2 := "-", "-"
			if len(sells) > 1 {
				sellPrice2 = sells[1].price
				sellQty2 = sells[1].qty
			}

			fmt.Fprintf(w, "\t| \t| %s\t| %s\t| %s\t| %s\t|\n",
				buyPrice2, buyQty2,
				sellPrice2, sellQty2,
			)
		}
	}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./cmd/tools/play_as/`
Expected: No compilation errors

- [ ] **Step 3: Manual test with actual market data**

Run: `./play_as view_market <item_with_orders>`
Expected: Formatted table showing buy/sell orders with proper sorting

- [ ] **Step 4: Run golangci-lint**

Run: `golangci-lint run cmd/tools/play_as/`
Expected: No new findings

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat: implement full table formatting for view_market"
```

---

### Task 6: Test edge cases

**Files:**
- Modify: `cmd/tools/play_as/main.go` (manual testing)

- [ ] **Step 1: Test empty buy orders**

Run: `./play_as view_market <item_with_only_sell_orders>`
Expected: Shows "-" in buy columns

- [ ] **Step 2: Test empty sell orders**

Run: `./play_as view_market <item_with_only_buy_orders>`
Expected: Shows "-" in sell columns

- [ ] **Step 3: Test single orders (no second row)**

Run: `./play_as view_market <item_with_single_orders>`
Expected: Only one row shown for that item

- [ ] **Step 4: Test station vs non-station orders**

Run: `./play_as view_market <item_with_mixed_sources>`
Expected: Station orders show with "*" prefix

- [ ] **Step 5: Test many orders (should truncate to 2)**

Run: `./play_as view_market <item_with_many_orders>`
Expected: Only shows best 2 buy and 2 sell orders

- [ ] **Step 6: Final verification**

Run: `go build ./... && go test ./...`
Expected: All tests pass, no compilation errors

- [ ] **Step 7: Final commit if any adjustments needed**

```bash
git add cmd/tools/play_as/main.go
git commit -m "fix: edge case handling in view_market formatting"
```

---

## Self-Review Checklist

**Spec Coverage:**
- ✓ Table format with Name/ID/Buy/Qty/Sell/Qty columns
- ✓ Up to one extra row per item for second orders
- ✓ "-" for empty buy/sell fields
- ✓ "*" prefix for station-sourced orders
- ✓ Buy orders sorted ascending (lowest first)
- ✓ Sell orders sorted descending (highest first)
- ✓ Integration with formatStyledResponse

**Placeholder Scan:**
- ✓ No "TODO", "TBD", or incomplete sections
- ✓ All code blocks complete with actual implementation
- ✓ Exact file paths provided
- ✓ Error handling included (JSON parse error, empty items)

**Type Consistency:**
- ✓ Response struct matches JSON structure from spec
- ✓ Helper functions use consistent struct types
- ✓ Function names match between definition and usage

**Edge Cases Covered:**
- ✓ Empty response
- ✓ No buy orders
- ✓ No sell orders
- ✓ Single order (no second row)
- ✓ Mixed station/non-station sources
- ✓ Many orders (truncation to 2)
