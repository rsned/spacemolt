package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseStationBuyOrders turns a compact view_market response (no item_id) into
// per-order MarketBuyOrderRow values across all items, carrying Source. The
// compact response already contains complete, source-tagged buy_orders, so no
// per-item deep call is needed. Skips orders with non-positive price or qty.
func parseStationBuyOrders(raw []byte, stationID, systemID string, now time.Time) []knowledge.MarketBuyOrderRow {
	if len(raw) == 0 || stationID == "" {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	var out []knowledge.MarketBuyOrderRow
	for _, it := range resp.Items {
		for _, o := range it.BuyOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			out = append(out, knowledge.MarketBuyOrderRow{
				StationID:  stationID,
				SystemID:   systemID,
				ItemID:     it.ItemID,
				ItemName:   it.ItemName,
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				Source:     o.Source,
				CapturedAt: now,
			})
		}
	}
	return out
}

// captureDemand persists the full source-classified buy-order demand from the
// client's most recent (full, no-item_id) view_market response, replacing the
// station's entire order set. Best-effort: silently no-ops when the KB is
// absent, there is no market data, or the player is not at a station.
func captureDemand(client game.GameClient, ctx context.Context) {
	if globalKB == nil {
		return
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return
	}
	state := client.GetState()
	if state == nil {
		return
	}
	orders := parseStationBuyOrders(client.GetRawJSON("market"), state.CurrentPOI, state.CurrentSystem, time.Now())
	if len(orders) == 0 {
		return
	}
	_ = sqlite.ReplaceStationBuyOrders(ctx, state.CurrentPOI, orders)
}
