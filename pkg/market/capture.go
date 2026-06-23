package market

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// parseViewMarket parses a raw view_market JSON response into Orders. The
// stationID is stamped on every order; system context is attached by the caller
// on the MarketSnapshot (systemID is not part of an individual order).
func parseViewMarket(raw []byte, stationID string, capturedAt time.Time) ([]Order, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var resp serverapi.ViewMarketResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}

	var orders []Order
	now := capturedAt.UTC()

	for _, item := range resp.Items {
		// Buy orders
		for _, o := range item.BuyOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			orders = append(orders, Order{
				StationID:  stationID,
				ItemID:     item.ItemID,
				ItemName:   item.ItemName,
				Category:   item.Category,
				Side:       "buy",
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}

		// Sell orders
		for _, o := range item.SellOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			orders = append(orders, Order{
				StationID:  stationID,
				ItemID:     item.ItemID,
				ItemName:   item.ItemName,
				Category:   item.Category,
				Side:       "sell",
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}
	}

	return orders, nil
}

// CaptureFromClient captures market data from a game client's last view_market
// response and persists it via the collector. It is a no-op (returns nil) when
// the client has no state, is not at a station, or has no cached market payload.
func CaptureFromClient(ctx context.Context, client game.GameClient, collector *Collector) error {
	state := client.GetState()
	if state == nil {
		return nil
	}

	// Must be at a station
	if state.CurrentPOI == "" {
		return nil
	}

	raw := client.GetRawJSON("market")
	if len(raw) == 0 {
		return nil
	}

	now := time.Now()
	orders, err := parseViewMarket(raw, state.CurrentPOI, now)
	if err != nil {
		return err
	}

	snapshot := MarketSnapshot{
		StationID:   state.CurrentPOI,
		StationName: state.CurrentPOI,
		SystemID:    state.CurrentSystem,
		SystemName:  state.CurrentSystem,
		Orders:      orders,
		CapturedAt:  now,
	}

	return collector.WriteSnapshot(ctx, snapshot)
}
