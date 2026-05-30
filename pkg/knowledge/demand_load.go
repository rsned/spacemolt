package knowledge

import "context"

// LoadMarketDemand returns the full ledger: the compact best-buy summary rows
// and the deep per-order rows. The report layer decides how to merge them.
func (kb *SQLiteKB) LoadMarketDemand(ctx context.Context) ([]MarketDemandRow, []MarketBuyOrderRow, error) {
	summary, err := kb.loadMarketDemandSummary(ctx)
	if err != nil {
		return nil, nil, err
	}
	deep, err := kb.loadMarketBuyOrders(ctx)
	if err != nil {
		return nil, nil, err
	}
	return summary, deep, nil
}

func (kb *SQLiteKB) loadMarketDemandSummary(ctx context.Context) ([]MarketDemandRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, system_id, item_id, item_name, best_buy_price, buy_quantity, captured_utc
		FROM market_buy_demand
		ORDER BY item_id, best_buy_price DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MarketDemandRow
	for rows.Next() {
		var r MarketDemandRow
		var capStr string
		if err := rows.Scan(&r.StationID, &r.SystemID, &r.ItemID, &r.ItemName,
			&r.BestBuyPrice, &r.BuyQuantity, &capStr); err != nil {
			return nil, err
		}
		r.CapturedAt = parseUTC(capStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadMarketBuyOrders(ctx context.Context) ([]MarketBuyOrderRow, error) {
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
