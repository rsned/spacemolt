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
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}
	}
	return out
}

// parseStationSellOrders turns a compact view_market response into per-order
// MarketSellOrderRow values across all items — the supply-side mirror of
// parseStationBuyOrders. Skips orders with non-positive price or qty.
func parseStationSellOrders(raw []byte, stationID, systemID string, now time.Time) []knowledge.MarketSellOrderRow {
	if len(raw) == 0 || stationID == "" {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	var out []knowledge.MarketSellOrderRow
	for _, it := range resp.Items {
		for _, o := range it.SellOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			out = append(out, knowledge.MarketSellOrderRow{
				StationID:  stationID,
				SystemID:   systemID,
				ItemID:     it.ItemID,
				ItemName:   it.ItemName,
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}
	}
	return out
}

// demandHistoryBucket is the time-bucket granularity for demand-history samples.
// Captures within the same bucket upsert the same row (last observation wins).
const demandHistoryBucket = time.Hour

// demandFreshness is how recently a station's demand must have been captured for
// a new capture to be skipped. Prevents many agents sharing a station from
// re-writing the same data minutes apart.
const demandFreshness = 5 * time.Minute

// isFresh reports whether a capture at `last` is recent enough (strictly within
// `window` of `now`) that re-capturing should be skipped.
func isFresh(last, now time.Time, window time.Duration) bool {
	return now.Sub(last) < window
}

// aggregateDemandHistory collapses per-order buy demand into one
// DemandHistorySample per (station, item): best price and total quantity across
// all orders, plus the Station-Manager split (source=="station"). BucketAt is
// `now` truncated to the bucket size; CapturedAt is `now`. Output preserves
// first-seen order for deterministic rendering and tests. Orders with
// non-positive price or quantity are skipped.
func aggregateDemandHistory(orders []knowledge.MarketBuyOrderRow, now time.Time, bucket time.Duration) []knowledge.DemandHistorySample {
	type acc struct {
		stationID, systemID, itemID, itemName string
		best, total, smBest, smQty            float64
		count                                 int
	}
	key := func(s, i string) string { return s + "\x00" + i }
	order := []string{}
	m := map[string]*acc{}
	for _, o := range orders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		k := key(o.StationID, o.ItemID)
		a, ok := m[k]
		if !ok {
			a = &acc{stationID: o.StationID, systemID: o.SystemID, itemID: o.ItemID, itemName: o.ItemName}
			m[k] = a
			order = append(order, k)
		}
		a.total += o.Quantity
		a.count++
		if o.PriceEach > a.best {
			a.best = o.PriceEach
		}
		if o.Source == "station" {
			a.smQty += o.Quantity
			if o.PriceEach > a.smBest {
				a.smBest = o.PriceEach
			}
		}
	}
	bucketAt := now.UTC().Truncate(bucket)
	out := make([]knowledge.DemandHistorySample, 0, len(order))
	for _, k := range order {
		a := m[k]
		out = append(out, knowledge.DemandHistorySample{
			StationID:   a.stationID,
			SystemID:    a.systemID,
			ItemID:      a.itemID,
			ItemName:    a.itemName,
			BucketAt:    bucketAt,
			CapturedAt:  now,
			BestPrice:   a.best,
			TotalQty:    a.total,
			SMBestPrice: a.smBest,
			SMQty:       a.smQty,
			OrderCount:  a.count,
		})
	}
	return out
}

// aggregateSupplyHistory collapses per-order sell supply into one
// SupplyHistorySample per (station, item) — the mirror of aggregateDemandHistory.
// Unlike demand (where "best" is the highest price), supply BestPrice is the
// LOWEST (cheapest) sell price across all orders, and SMBestPrice is the
// cheapest Station-Manager (source=="station") price. Orders with non-positive
// price or quantity are skipped.
func aggregateSupplyHistory(orders []knowledge.MarketSellOrderRow, now time.Time, bucket time.Duration) []knowledge.SupplyHistorySample {
	type acc struct {
		stationID, systemID, itemID, itemName string
		best, total, smBest, smQty            float64
		count                                 int
	}
	key := func(s, i string) string { return s + "\x00" + i }
	order := []string{}
	m := map[string]*acc{}
	for _, o := range orders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		k := key(o.StationID, o.ItemID)
		a, ok := m[k]
		if !ok {
			a = &acc{stationID: o.StationID, systemID: o.SystemID, itemID: o.ItemID, itemName: o.ItemName}
			m[k] = a
			order = append(order, k)
		}
		a.total += o.Quantity
		a.count++
		// Cheapest wins for supply; best==0 means unset (prices are positive).
		if a.best == 0 || o.PriceEach < a.best {
			a.best = o.PriceEach
		}
		if o.Source == "station" {
			a.smQty += o.Quantity
			if a.smBest == 0 || o.PriceEach < a.smBest {
				a.smBest = o.PriceEach
			}
		}
	}
	bucketAt := now.UTC().Truncate(bucket)
	out := make([]knowledge.SupplyHistorySample, 0, len(order))
	for _, k := range order {
		a := m[k]
		out = append(out, knowledge.SupplyHistorySample{
			StationID:   a.stationID,
			SystemID:    a.systemID,
			ItemID:      a.itemID,
			ItemName:    a.itemName,
			BucketAt:    bucketAt,
			CapturedAt:  now,
			BestPrice:   a.best,
			TotalQty:    a.total,
			SMBestPrice: a.smBest,
			SMQty:       a.smQty,
			OrderCount:  a.count,
		})
	}
	return out
}

// captureMarket persists the full source-classified market data from the
// client's most recent (full, no-item_id) view_market response: buy-order demand
// into the demand tables and sell-order supply into the supply tables, each
// replacing the station's entire order set. Best-effort: silently no-ops when
// the KB is absent, there is no market data, or the player is not at a station.
// A single freshness gate (keyed on the demand ledger) guards both sides, since
// they come from the same read.
func captureMarket(client game.GameClient, ctx context.Context) {
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
	now := time.Now()
	raw := client.GetRawJSON("market")
	buyOrders := parseStationBuyOrders(raw, state.CurrentPOI, state.CurrentSystem, now)
	sellOrders := parseStationSellOrders(raw, state.CurrentPOI, state.CurrentSystem, now)
	if len(buyOrders) == 0 && len(sellOrders) == 0 {
		return
	}
	// Skip all writes if this station's market was captured recently (possibly by
	// another agent) — avoids redundant work across many agents sharing a station.
	if last, ok, err := sqlite.LatestDemandCapture(ctx, state.CurrentPOI); err == nil && ok && isFresh(last, now, demandFreshness) {
		return
	}
	_ = sqlite.ReplaceStationBuyOrders(ctx, state.CurrentPOI, buyOrders)
	_ = sqlite.RecordDemandHistory(ctx, aggregateDemandHistory(buyOrders, now, demandHistoryBucket))
	_ = sqlite.ReplaceStationSellOrders(ctx, state.CurrentPOI, sellOrders)
	_ = sqlite.RecordSupplyHistory(ctx, aggregateSupplyHistory(sellOrders, now, demandHistoryBucket))
}
