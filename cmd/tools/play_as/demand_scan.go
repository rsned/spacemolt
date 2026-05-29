package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseDeepOrders turns a single-item view_market response (items[0].buy_orders)
// into MarketBuyOrderRow values, skipping zero-price/zero-qty entries.
func parseDeepOrders(raw []byte, stationID, systemID, itemID string, now time.Time) []knowledge.MarketBuyOrderRow {
	if len(raw) == 0 {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Items) == 0 {
		return nil
	}
	it := resp.Items[0]
	name := it.ItemName
	var out []knowledge.MarketBuyOrderRow
	for _, o := range it.BuyOrders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		out = append(out, knowledge.MarketBuyOrderRow{
			StationID: stationID, SystemID: systemID, ItemID: itemID, ItemName: name,
			PriceEach: o.PriceEach, Quantity: o.Quantity, Source: o.Source, CapturedAt: now,
		})
	}
	return out
}

// runDemandScan does an explicit deep pass at the current station: for every
// item with buy demand in the compact summary, it fetches the full order book
// (view_market with item_id) and stores Source-classified rows. This is the
// only chatty path — one server call per item, paced by SleepQuick.
func runDemandScan(client game.GameClient, ctx context.Context) error { //nolint:unused // wired in Task 10
	if globalKB == nil {
		return fmt.Errorf("demand scan: no knowledge DB configured (start play_as with --db)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("demand scan: knowledge DB is not SQLite-backed")
	}
	state := client.GetState()
	if state == nil || state.CurrentPOI == "" {
		return fmt.Errorf("demand scan: must be docked at a station")
	}
	stationID, systemID := state.CurrentPOI, state.CurrentSystem

	// 1. Compact summary to discover which items have buy demand.
	if err := client.GetListings(ctx); err != nil {
		return fmt.Errorf("demand scan: view_market: %w", err)
	}
	captureDemand(client, ctx) // also refresh the compact ledger
	items := parseDemandRows(client.GetRawJSON("market"), stationID, systemID, time.Now())
	if len(items) == 0 {
		fmt.Println("demand scan: no buy demand at this station.")
		return nil
	}

	fmt.Printf("demand scan: deep-scanning %d items at %s…\n", len(items), stationID)
	scanned := 0
	for _, it := range items {
		if err := client.ViewMarket(ctx, map[string]any{"item_id": it.ItemID}); err != nil {
			fmt.Printf("  %s: %v (skipped)\n", it.ItemID, err)
			continue
		}
		orders := parseDeepOrders(client.GetRawJSON("market"), stationID, systemID, it.ItemID, time.Now())
		if err := sqlite.ReplaceMarketBuyOrders(ctx, stationID, it.ItemID, orders); err != nil {
			fmt.Printf("  %s: store failed: %v\n", it.ItemID, err)
			continue
		}
		scanned++
		time.Sleep(game.SleepQuick)
	}
	fmt.Printf("demand scan: captured full order depth for %d/%d items.\n", scanned, len(items))
	return nil
}
