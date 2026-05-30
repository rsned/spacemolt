package knowledge

import "context"

// LoadDemandHistory returns demand-history samples for itemID, optionally
// narrowed to a single stationID (""=all stations). Rows are ordered
// chronologically (oldest first) within each station. When limit>0, only the
// most recent `limit` buckets of each station are returned (0=no cap).
func (kb *SQLiteKB) LoadDemandHistory(ctx context.Context, itemID, stationID string, limit int) ([]DemandHistorySample, error) {
	query := `
		SELECT station_id, system_id, item_id, item_name, bucket_utc, captured_utc,
		       best_price, total_qty, sm_best_price, sm_qty, order_count
		FROM market_demand_history
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

	var all []DemandHistorySample
	for rows.Next() {
		var s DemandHistorySample
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
		all = capPerStation(all, limit)
	}
	return all, nil
}

// capPerStation keeps only the most recent `limit` samples of each station from
// a slice already ordered by (station_id ASC, bucket_utc ASC).
func capPerStation(samples []DemandHistorySample, limit int) []DemandHistorySample {
	var out []DemandHistorySample
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

// LoadMarketBuyOrders returns every captured buy order across all stations,
// ordered by station, item, then price descending.
func (kb *SQLiteKB) LoadMarketBuyOrders(ctx context.Context) ([]MarketBuyOrderRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, system_id, item_id, item_name, price_each, quantity, source, captured_utc
		FROM market_buy_orders
		ORDER BY station_id, item_id, price_each DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MarketBuyOrderRow
	for rows.Next() {
		var r MarketBuyOrderRow
		var capStr string
		if err := rows.Scan(&r.StationID, &r.SystemID, &r.ItemID, &r.ItemName,
			&r.PriceEach, &r.Quantity, &r.Source, &capStr); err != nil {
			return nil, err
		}
		r.CapturedAt = parseUTC(capStr)
		out = append(out, r)
	}
	return out, rows.Err()
}
