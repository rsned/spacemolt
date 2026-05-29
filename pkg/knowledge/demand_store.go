package knowledge

import "context"

// UpsertMarketDemand inserts or updates the compact best-buy demand for each
// (station_id, item_id). Empty SystemID/ItemName are stored as "" (never NULL)
// so loaders can scan into plain strings.
func (kb *SQLiteKB) UpsertMarketDemand(ctx context.Context, rows []MarketDemandRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_buy_demand
					(station_id, system_id, item_id, item_name, best_buy_price, buy_quantity, captured_utc)
				VALUES (?,?,?,?,?,?,?)
				ON CONFLICT(station_id, item_id) DO UPDATE SET
					system_id      = excluded.system_id,
					item_name      = excluded.item_name,
					best_buy_price = excluded.best_buy_price,
					buy_quantity   = excluded.buy_quantity,
					captured_utc   = excluded.captured_utc`,
				r.StationID, r.SystemID, r.ItemID, r.ItemName, r.BestBuyPrice, r.BuyQuantity, utc(r.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceMarketBuyOrders replaces all deep-scan buy orders for one
// (station_id, item_id) with the supplied set.
func (kb *SQLiteKB) ReplaceMarketBuyOrders(ctx context.Context, stationID, itemID string, orders []MarketBuyOrderRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM market_buy_orders WHERE station_id=? AND item_id=?`, stationID, itemID); err != nil {
			return err
		}
		for _, o := range orders {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_buy_orders
					(station_id, system_id, item_id, item_name, price_each, quantity, source, captured_utc)
				VALUES (?,?,?,?,?,?,?,?)`,
				o.StationID, o.SystemID, o.ItemID, o.ItemName, o.PriceEach, o.Quantity, o.Source, utc(o.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}
