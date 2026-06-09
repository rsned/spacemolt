package knowledge

import "context"

// ReplaceStationSellOrders replaces ALL sell orders for a station with the
// supplied set in one transaction — the supply-side mirror of
// ReplaceStationBuyOrders. A full compact view_market read covers every item at
// the station, so replacing by station keeps the snapshot fresh and prunes
// items whose supply has vanished since the last read.
func (kb *SQLiteKB) ReplaceStationSellOrders(ctx context.Context, stationID string, orders []MarketSellOrderRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM market_sell_orders WHERE station_id=?`, stationID); err != nil {
			return err
		}
		for _, o := range orders {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_sell_orders
					(station_id, system_id, item_id, item_name, price_each, quantity, my_quantity, source, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?)`,
				o.StationID, o.SystemID, o.ItemID, o.ItemName, o.PriceEach, o.Quantity, o.MyQuantity, o.Source, utc(o.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// RecordSupplyHistory upserts one row per (station, item, bucket) into
// market_supply_history — the mirror of RecordDemandHistory. Re-reading a
// station within the same bucket updates that row in place (last observation in
// the bucket wins); a new bucket appends a new row.
func (kb *SQLiteKB) RecordSupplyHistory(ctx context.Context, samples []SupplyHistorySample) error {
	return kb.inTx(ctx, func(tx txer) error {
		for _, s := range samples {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_supply_history
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

// LoadMarketSellOrders returns every captured sell order across all stations,
// ordered by station, item, then price ascending (cheapest first) — the mirror
// of LoadMarketBuyOrders.
func (kb *SQLiteKB) LoadMarketSellOrders(ctx context.Context) ([]MarketSellOrderRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, system_id, item_id, item_name, price_each, quantity, my_quantity, source, captured_utc
		FROM market_sell_orders
		ORDER BY station_id, item_id, price_each ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MarketSellOrderRow
	for rows.Next() {
		var r MarketSellOrderRow
		var capStr string
		if err := rows.Scan(&r.StationID, &r.SystemID, &r.ItemID, &r.ItemName,
			&r.PriceEach, &r.Quantity, &r.MyQuantity, &r.Source, &capStr); err != nil {
			return nil, err
		}
		r.CapturedAt = parseUTC(capStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadSupplyHistory returns supply-history samples for itemID, optionally
// narrowed to a single stationID (""=all). Rows are ordered chronologically
// (oldest first) within each station; when limit>0, only the most recent
// `limit` buckets of each station are returned — the mirror of LoadDemandHistory.
func (kb *SQLiteKB) LoadSupplyHistory(ctx context.Context, itemID, stationID string, limit int) ([]SupplyHistorySample, error) {
	query := `
		SELECT station_id, system_id, item_id, item_name, bucket_utc, captured_utc,
		       best_price, total_qty, sm_best_price, sm_qty, order_count
		FROM market_supply_history
		WHERE item_id=?`
	args := []any{itemID}
	if stationID != "" {
		query += ` AND station_id=?`
		args = append(args, stationID)
	}
	query += ` ORDER BY station_id ASC, bucket_utc ASC`

	rows, err := kb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var all []SupplyHistorySample
	for rows.Next() {
		var s SupplyHistorySample
		var bucketStr, capStr string
		if err := rows.Scan(&s.StationID, &s.SystemID, &s.ItemID, &s.ItemName,
			&bucketStr, &capStr, &s.BestPrice, &s.TotalQty, &s.SMBestPrice, &s.SMQty, &s.OrderCount); err != nil {
			return nil, err
		}
		s.BucketAt = parseUTC(bucketStr)
		s.CapturedAt = parseUTC(capStr)
		all = append(all, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if limit > 0 {
		all = capSupplyPerStation(all, limit)
	}
	return all, nil
}

// capSupplyPerStation keeps only the most recent `limit` samples of each station
// from a slice already ordered by (station_id ASC, bucket_utc ASC).
func capSupplyPerStation(samples []SupplyHistorySample, limit int) []SupplyHistorySample {
	var out []SupplyHistorySample
	for i := 0; i < len(samples); {
		j := i
		for j < len(samples) && samples[j].StationID == samples[i].StationID {
			j++
		}
		group := samples[i:j]
		if len(group) > limit {
			group = group[len(group)-limit:]
		}
		out = append(out, group...)
		i = j
	}
	return out
}
