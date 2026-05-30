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
