package knowledge

import "context"

// ReplaceStationBuyOrders replaces ALL buy orders for a station with the
// supplied set in one transaction. A full compact view_market read covers every
// item at the station, so replacing by station keeps the snapshot fresh and
// prunes items whose demand has vanished since the last read. Empty
// SystemID/ItemName/Source are stored as "" (never NULL) so loaders scan into
// plain strings.
func (kb *SQLiteKB) ReplaceStationBuyOrders(ctx context.Context, stationID string, orders []MarketBuyOrderRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM market_buy_orders WHERE station_id=?`, stationID); err != nil {
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

// RecordDemandHistory upserts one row per (station, item, bucket) into
// market_demand_history. Re-reading a station within the same bucket updates that
// row in place (last observation in the bucket wins); a new bucket appends a new
// row. Runs in one transaction so a station's samples are all-or-nothing.
func (kb *SQLiteKB) RecordDemandHistory(ctx context.Context, samples []DemandHistorySample) error {
	return kb.inTx(ctx, func(tx txer) error {
		for _, s := range samples {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_demand_history
					(station_id, system_id, item_id, item_name, bucket_utc, captured_utc,
					 best_price, total_qty, sm_best_price, sm_qty, order_count)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(station_id, item_id, bucket_utc) DO UPDATE SET
					system_id     = excluded.system_id,
					item_name     = excluded.item_name,
					captured_utc  = excluded.captured_utc,
					best_price    = excluded.best_price,
					total_qty     = excluded.total_qty,
					sm_best_price = excluded.sm_best_price,
					sm_qty        = excluded.sm_qty,
					order_count   = excluded.order_count`,
				s.StationID, s.SystemID, s.ItemID, s.ItemName, utc(s.BucketAt), utc(s.CapturedAt),
				s.BestPrice, s.TotalQty, s.SMBestPrice, s.SMQty, s.OrderCount); err != nil {
				return err
			}
		}
		return nil
	})
}
