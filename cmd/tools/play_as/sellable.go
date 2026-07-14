package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// sellableOptions mirrors the flags accepted on the `sellable` REPL command.
type sellableOptions struct {
	detail      bool
	minProceeds int64
}

// runSellable is the REPL entry point for the `sellable` command. It is the
// only function `executeCommand` calls — every other piece of this file is
// either pure (testable without a network) or a renderer.
func runSellable(client game.GameClient, ctx context.Context, opts sellableOptions, format outputFormat) error {
	state := client.GetState()
	if state == nil || !state.Doc {
		return fmt.Errorf("sellable: must be docked at a station with a market service")
	}

	// 1. view_market — full order book at this station.
	if err := client.GetListings(ctx); err != nil {
		return fmt.Errorf("sellable: view_market: %w", err)
	}
	marketRaw := client.GetRawJSON("market")
	var marketResp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if len(marketRaw) > 0 {
		if err := json.Unmarshal(marketRaw, &marketResp); err != nil {
			return fmt.Errorf("sellable: parse market: %w", err)
		}
	}
	// The demand ledger is fed opportunistically whenever we read a market.
	captureMarket(client, ctx)

	// 2. get_cargo — ship cargo.
	if err := client.GetCargo(ctx); err != nil {
		return fmt.Errorf("sellable: get_cargo: %w", err)
	}
	cargoRaw := client.GetRawJSON("cargo")
	var cargoResp struct {
		Cargo []storageItem `json:"cargo"`
	}
	if len(cargoRaw) > 0 {
		if err := json.Unmarshal(cargoRaw, &cargoResp); err != nil {
			return fmt.Errorf("sellable: parse cargo: %w", err)
		}
	}

	// 3. view_storage — current station's storage.
	if err := client.ViewStorage(ctx); err != nil {
		return fmt.Errorf("sellable: view_storage: %w", err)
	}
	storageRaw := client.GetRawJSON("storage")
	var storageResp struct {
		BaseID string        `json:"base_id"`
		Items  []storageItem `json:"items"`
	}
	if len(storageRaw) > 0 {
		if err := json.Unmarshal(storageRaw, &storageResp); err != nil {
			return fmt.Errorf("sellable: parse storage: %w", err)
		}
	}

	stationID := storageResp.BaseID
	if stationID == "" {
		stationID = state.CurrentPOI
	}
	plan := buildSellablePlan(stationID, marketResp.Items, cargoResp.Cargo, storageResp.Items)

	// Apply --min-proceeds filter after computation. ItemCount and
	// TotalProceeds are recomputed from the filtered set so the header,
	// footer, and visible rows all agree.
	if opts.minProceeds > 0 {
		filtered := plan.Items[:0]
		var total float64
		for _, r := range plan.Items {
			if int64(r.TotalProceeds) >= opts.minProceeds {
				filtered = append(filtered, r)
				total += r.TotalProceeds
			}
		}
		plan.Items = filtered
		plan.ItemCount = len(filtered)
		plan.TotalProceeds = total
	}

	switch format {
	case formatStyled:
		fmt.Print(renderSellableStyled(plan, opts.detail))
	default:
		fmt.Print(renderSellableJSON(plan))
	}
	return nil
}

// sellableFill records one match of cargo against a single buy order.
type sellableFill struct {
	Price    float64 `json:"price"`
	Qty      float64 `json:"qty"`
	Proceeds float64 `json:"proceeds"`
}

// sellableRow is one item's full sellability picture: what's on hand, what
// can be moved at the current station's market, and the per-buyer fills.
type sellableRow struct {
	ItemID        string         `json:"item_id"`
	Name          string         `json:"name"`
	Cargo         float64        `json:"cargo"`
	Storage       float64        `json:"storage"`
	SellableQty   float64        `json:"sellable_qty"`
	TotalProceeds float64        `json:"total_proceeds"`
	AvgPrice      float64        `json:"avg_price"`
	Fills         []sellableFill `json:"fills,omitempty"`
}

// sellablePlan is the rendered/serialized result of `sellable`. Sort order
// of Items: ItemID ascending. Totals roll up across all rows.
type sellablePlan struct {
	StationID     string        `json:"station_id"`
	ItemCount     int           `json:"item_count"`
	TotalProceeds float64       `json:"total_proceeds"`
	Items         []sellableRow `json:"items"`
}

// buildSellablePlan unions cargo+storage by item_id, looks up each item's
// market order book, runs fillItem against cargo only (sell pulls from
// cargo, not storage), and emits a per-row plan plus rolled-up totals.
//
// Pure function: every input is plain data; no game.Client / context /
// network involvement. The orchestrator is responsible for fetching the
// inputs.
func buildSellablePlan(stationID string, market []serverapi.ViewMarketItem, cargo, storage []storageItem) sellablePlan {
	byID := make(map[string]serverapi.ViewMarketItem, len(market))
	for _, m := range market {
		byID[m.ItemID] = m
	}

	type acc struct {
		cargo   float64
		storage float64
		name    string
	}
	inv := make(map[string]*acc)
	get := func(id string) *acc {
		a, ok := inv[id]
		if !ok {
			a = &acc{}
			inv[id] = a
		}
		return a
	}
	for _, c := range cargo {
		a := get(c.ItemID)
		a.cargo += c.Quantity
		if a.name == "" {
			a.name = c.Name
		}
	}
	for _, s := range storage {
		a := get(s.ItemID)
		a.storage += s.Quantity
		if a.name == "" {
			a.name = s.Name
		}
	}

	plan := sellablePlan{StationID: stationID, Items: []sellableRow{}}
	for id, a := range inv {
		row := sellableRow{
			ItemID:  id,
			Cargo:   a.cargo,
			Storage: a.storage,
		}
		switch {
		case a.name != "":
			row.Name = a.name
		case byID[id].ItemName != "":
			row.Name = byID[id].ItemName
		default:
			row.Name = id
		}
		if mkt, ok := byID[id]; ok {
			// Walk cargo+storage: storage items can be withdrawn and sold,
			// so the operator's effective sellable pool at this station is
			// the union of both. Cargo and Storage stay split in the row
			// columns so the operator still knows where to withdraw from.
			qty, proceeds, avg, fills := fillItem(a.cargo+a.storage, mkt.BuyOrders)
			row.SellableQty = qty
			row.TotalProceeds = proceeds
			row.AvgPrice = avg
			row.Fills = fills
		}
		plan.Items = append(plan.Items, row)
	}
	slices.SortFunc(plan.Items, func(x, y sellableRow) int {
		switch {
		case x.ItemID < y.ItemID:
			return -1
		case x.ItemID > y.ItemID:
			return 1
		default:
			return 0
		}
	})
	plan.ItemCount = len(plan.Items)
	for _, r := range plan.Items {
		plan.TotalProceeds += r.TotalProceeds
	}
	return plan
}

// renderSellableStyled formats a sellablePlan as a human-readable table.
// detail=true adds an expanded per-buyer fill block under each multi-buyer
// item; single-buyer items stay inline regardless. The empty-inventory
// branch returns the documented "(no cargo or storage at this station)"
// line so callers don't need to special-case it before printing.
func renderSellableStyled(plan sellablePlan, detail bool) string {
	if len(plan.Items) == 0 {
		return "(no cargo or storage at this station)\n"
	}

	idW, nameW := len("ID"), len("Name")
	cargoW, storageW := len("Cargo"), len("Storage")
	sellW, avgW, proceedsW := len("Sellable"), len("Avg Price"), len("Proceeds")
	for _, r := range plan.Items {
		idW = max(idW, len(r.ItemID))
		nameW = max(nameW, len(r.Name))
		cargoW = max(cargoW, len(formatFloat(r.Cargo)))
		storageW = max(storageW, len(formatFloat(r.Storage)))
		sellW = max(sellW, len(formatFloat(r.SellableQty)))
		if r.SellableQty > 0 {
			avgW = max(avgW, len(formatPrice(r.AvgPrice)))
		}
		proceedsW = max(proceedsW, len(formatCredits(r.TotalProceeds)))
	}
	// The grand total is a sum, so it can be wider than any single row's
	// proceeds. Size the column for it too, or the Total line overhangs the
	// table's right edge.
	proceedsW = max(proceedsW, len(formatCredits(plan.TotalProceeds)))

	var b strings.Builder
	fmt.Fprintf(&b, "Sellable @ %s — %d items, est. proceeds %s cr\n\n",
		plan.StationID, plan.ItemCount, formatCredits(plan.TotalProceeds))
	fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s | %*s | %*s | %*s\n",
		idW, "ID", nameW, "Name",
		cargoW, "Cargo", storageW, "Storage",
		sellW, "Sellable", avgW, "Avg Price", proceedsW, "Proceeds")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", idW), strings.Repeat("-", nameW),
		strings.Repeat("-", cargoW), strings.Repeat("-", storageW),
		strings.Repeat("-", sellW), strings.Repeat("-", avgW), strings.Repeat("-", proceedsW))

	for _, r := range plan.Items {
		avg := "—"
		if r.SellableQty > 0 {
			avg = formatPrice(r.AvgPrice)
		}
		fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s | %*s | %*s | %*s\n",
			idW, r.ItemID, nameW, r.Name,
			cargoW, formatFloat(r.Cargo), storageW, formatFloat(r.Storage),
			sellW, formatFloat(r.SellableQty),
			avgW, avg,
			proceedsW, formatCredits(r.TotalProceeds))
	}
	// Right-align the total under the Proceeds column. leadW spans every column
	// before Proceeds plus the five " | " separators between them, so "Total:"
	// ends flush against the last separator and the amount lines up with the
	// figures above it. (The old version padded a hardcoded width and then
	// printed the label *after* it, running the line past the table's edge.)
	leadW := idW + nameW + cargoW + storageW + sellW + avgW + 15
	fmt.Fprintf(&b, "  %s-+-%s\n",
		strings.Repeat("-", leadW), strings.Repeat("-", proceedsW))
	fmt.Fprintf(&b, "  %*s | %*s\n",
		leadW, "Total:", proceedsW, formatCredits(plan.TotalProceeds))

	if detail {
		for _, r := range plan.Items {
			if len(r.Fills) <= 1 {
				continue
			}
			fmt.Fprintf(&b, "\n%s — %s / %s sellable, %s cr\n",
				r.ItemID, formatFloat(r.SellableQty), formatFloat(r.Cargo),
				formatCredits(r.TotalProceeds))
			for _, f := range r.Fills {
				fmt.Fprintf(&b, "  %s @ %s = %s cr\n",
					formatFloat(f.Qty), formatPrice(f.Price), formatCredits(f.Proceeds))
			}
		}
	}
	return b.String()
}

// renderSellableJSON serializes a plan as pretty-printed JSON. Field tags
// on sellablePlan / sellableRow / sellableFill drive the wire shape.
// Returns "" on marshal error (impossible for the value types involved,
// but explicit to stay symmetric with the styled renderer).
func renderSellableJSON(plan sellablePlan) string {
	out, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return ""
	}
	return string(out) + "\n"
}

// formatPrice renders a price-each value with two decimals (matching the
// existing market-row style). formatFloat / formatCredits already exist
// in main.go / statusline.go and are reused here.
func formatPrice(p float64) string {
	return fmt.Sprintf("%.2f", p)
}

// fillItem adapts a station's market buy orders into order-book rungs and walks
// them via fillOrderBook (price-desc, min(remaining_cargo, order.quantity) from
// each). Tests live in sellable_test.go::TestFillItem; the walk itself is
// covered by orderbook_fill_test.go::TestFillOrderBook.
//
// Returns: total filled quantity, total proceeds (price*qty summed), the
// proceeds-weighted average price (0 when nothing filled), and the per-fill
// breakdown in fill order.
func fillItem(cargo float64, orders []serverapi.MarketOrder) (qty, proceeds, avg float64, fills []sellableFill) {
	rungs := make([]orderRung, 0, len(orders))
	for _, o := range orders {
		rungs = append(rungs, orderRung{Price: o.PriceEach, Qty: o.Quantity})
	}
	return fillOrderBook(cargo, rungs)
}
