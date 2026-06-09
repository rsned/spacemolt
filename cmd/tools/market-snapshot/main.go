// Command market-snapshot summarizes a raw view_market response snapshot:
// how many orders match a given source (e.g. "station") and the total credits
// it would cost to buy every unit of the matching sell orders.
//
// Usage:
//
//	market-snapshot [flags] <market.json>
//	cat market.json | market-snapshot [flags]
//
// Flags:
//
//	-source  order source to match (default "station"; empty matches all)
//	-side    "sell" (buy from the market) or "buy" (sell to the market) (default "sell")
//	-top     show the N costliest item types (default 10; 0 disables)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
)

// marketOrder is a single order line within an item's order book.
type marketOrder struct {
	PriceEach int    `json:"price_each"`
	Quantity  int    `json:"quantity"`
	Source    string `json:"source"`
}

// marketItem is one item's entry in a view_market response.
type marketItem struct {
	ItemID     string        `json:"item_id"`
	ItemName   string        `json:"item_name"`
	SellOrders []marketOrder `json:"sell_orders"`
	BuyOrders  []marketOrder `json:"buy_orders"`
}

// marketSnapshot is the subset of a view_market response we read.
type marketSnapshot struct {
	Base   string       `json:"base"`
	BaseID string       `json:"base_id"`
	Items  []marketItem `json:"items"`
}

// itemTotal accumulates the matching orders for one item type.
type itemTotal struct {
	itemID string
	orders int
	units  int
	cost   int
}

func main() {
	source := flag.String("source", "station", `order source to match (empty matches all sources)`)
	side := flag.String("side", "sell", `"sell" (buy from market) or "buy" (sell to market)`)
	top := flag.Int("top", 10, "show the N costliest item types (0 disables)")
	flag.Parse()

	if *side != "sell" && *side != "buy" {
		fmt.Fprintf(os.Stderr, "invalid -side %q: must be \"sell\" or \"buy\"\n", *side)
		os.Exit(2)
	}

	raw, err := readInput(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}

	var snap marketSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		fmt.Fprintf(os.Stderr, "parse market json: %v\n", err)
		os.Exit(1)
	}

	var (
		totalOrders, totalUnits, totalCost int
		perItem                            []itemTotal
	)
	for _, it := range snap.Items {
		orders := it.SellOrders
		if *side == "buy" {
			orders = it.BuyOrders
		}
		var t itemTotal
		t.itemID = it.ItemID
		for _, o := range orders {
			if *source != "" && o.Source != *source {
				continue
			}
			t.orders++
			t.units += o.Quantity
			t.cost += o.PriceEach * o.Quantity
		}
		if t.orders > 0 {
			perItem = append(perItem, t)
			totalOrders += t.orders
			totalUnits += t.units
			totalCost += t.cost
		}
	}

	srcLabel := *source
	if srcLabel == "" {
		srcLabel = "(any)"
	}
	if snap.Base != "" {
		fmt.Printf("Market snapshot: %s (%s)\n", snap.Base, snap.BaseID)
	}
	fmt.Printf("Side: %s   Source: %s\n\n", *side, srcLabel)
	fmt.Printf("Orders:         %s\n", comma(totalOrders))
	fmt.Printf("Distinct items: %s\n", comma(len(perItem)))
	fmt.Printf("Total units:    %s\n", comma(totalUnits))
	verb := "buy"
	if *side == "buy" {
		verb = "sell"
	}
	fmt.Printf("Total cost to %s all: %s cr\n", verb, comma(totalCost))

	if *top > 0 && len(perItem) > 0 {
		slices.SortFunc(perItem, func(a, b itemTotal) int { return b.cost - a.cost })
		n := min(*top, len(perItem))
		fmt.Printf("\nTop %d item types by cost:\n", n)
		fmt.Printf("  %-34s %8s %16s %20s\n", "item", "orders", "units", "cost")
		for _, t := range perItem[:n] {
			fmt.Printf("  %-34s %8s %16s %20s\n", t.itemID, comma(t.orders), comma(t.units), comma(t.cost))
		}
	}
}

// readInput reads the named file, or stdin when path is empty or "-".
func readInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// comma formats n with thousands separators (e.g. 1234567 -> "1,234,567").
func comma(n int) string {
	s := strconv.Itoa(n)
	neg := false
	if n < 0 {
		neg = true
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
