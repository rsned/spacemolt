package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseDemandRows turns a compact view_market response (no item_id) into
// MarketDemandRow values, keeping only items with actual buy demand. Uses
// best_buy, falling back to buy_price when best_buy is zero.
func parseDemandRows(raw []byte, stationID, systemID string, now time.Time) []knowledge.MarketDemandRow {
	if len(raw) == 0 || stationID == "" {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	var out []knowledge.MarketDemandRow
	for _, it := range resp.Items {
		price := it.BestBuy
		if price <= 0 {
			price = float64(it.BuyPrice)
		}
		qty := float64(it.BuyQuantity)
		if price <= 0 || qty <= 0 {
			continue
		}
		out = append(out, knowledge.MarketDemandRow{
			StationID:    stationID,
			SystemID:     systemID,
			ItemID:       it.ItemID,
			ItemName:     it.ItemName,
			BestBuyPrice: price,
			BuyQuantity:  qty,
			CapturedAt:   now,
		})
	}
	return out
}

// captureDemand persists the compact buy-order demand from the client's most
// recent view_market response. Best-effort: silently no-ops when the KB is
// absent, there is no market data, or the player is not at a station.
func captureDemand(client game.GameClient, ctx context.Context) { //nolint:unused // wired in Task 10
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
	rows := parseDemandRows(client.GetRawJSON("market"), state.CurrentPOI, state.CurrentSystem, time.Now())
	if len(rows) == 0 {
		return
	}
	_ = sqlite.UpsertMarketDemand(ctx, rows)
}
