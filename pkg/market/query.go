package market

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Stats represents database statistics for quick health checks.
type Stats struct {
	StationCount  int    `json:"station_count"`
	ItemCount     int    `json:"item_count"`
	OrderCount    int64  `json:"order_count"`
	OHLCVCount    int64  `json:"ohlcv_count"`
	LatestCapture string `json:"latest_capture"`
}

// GetStats returns database statistics (row counts + most recent capture time).
func (c *Collector) GetStats(ctx context.Context) (*Stats, error) {
	var stats Stats

	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM stations").Scan(&stats.StationCount); err != nil {
		return nil, fmt.Errorf("count stations: %w", err)
	}
	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&stats.ItemCount); err != nil {
		return nil, fmt.Errorf("count items: %w", err)
	}
	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_orders").Scan(&stats.OrderCount); err != nil {
		return nil, fmt.Errorf("count orders: %w", err)
	}
	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_ohlcv").Scan(&stats.OHLCVCount); err != nil {
		return nil, fmt.Errorf("count ohlcv: %w", err)
	}

	// MAX(captured_at) returns NULL when the table is empty — scan into a nullable.
	var latest sql.NullString
	if err := c.db.QueryRowContext(ctx, "SELECT MAX(captured_at) FROM market_orders").Scan(&latest); err != nil {
		return nil, fmt.Errorf("latest capture: %w", err)
	}
	stats.LatestCapture = latest.String // "" when NULL/empty

	return &stats, nil
}

// GetLatestOrders returns the most recent orders for a station, newest first.
func (c *Collector) GetLatestOrders(ctx context.Context, stationID string, limit int) ([]Order, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at
		FROM market_orders
		WHERE station_id = ?
		ORDER BY captured_at DESC
		LIMIT ?
	`, stationID, limit)
	if err != nil {
		return nil, fmt.Errorf("query orders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var orders []Order
	for rows.Next() {
		var o Order
		var capturedAtStr string
		if err := rows.Scan(&o.StationID, &o.ItemID, &o.Side, &o.PriceEach, &o.Quantity, &o.MyQuantity, &o.Source, &capturedAtStr); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		o.CapturedAt, _ = time.Parse(time.RFC3339, capturedAtStr)
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orders: %w", err)
	}

	return orders, nil
}

// GetLatestSnapshot returns the most recent captured market state for a
// station, reconstructed from the orders sharing the newest captured_at.
// Returns (nil, nil) when the station has no orders.
func (c *Collector) GetLatestSnapshot(ctx context.Context, stationID string) (*MarketSnapshot, error) {
	var latest string
	err := c.db.QueryRowContext(ctx,
		`SELECT MAX(captured_at) FROM market_orders WHERE station_id = ?`, stationID).Scan(&latest)
	if err == sql.ErrNoRows || latest == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest captured_at: %w", err)
	}

	snap := &MarketSnapshot{StationID: stationID}
	_ = c.db.QueryRowContext(ctx,
		`SELECT station_name, system_id, system_name FROM stations WHERE station_id = ?`, stationID).
		Scan(&snap.StationName, &snap.SystemID, &snap.SystemName)
	snap.CapturedAt, _ = time.Parse(time.RFC3339, latest)

	rows, err := c.db.QueryContext(ctx, `
		SELECT mo.station_id, mo.item_id, COALESCE(i.item_name, ''), mo.side, mo.price_each, mo.quantity, mo.my_quantity, mo.source, mo.captured_at
		FROM market_orders mo
		LEFT JOIN items i ON i.item_id = mo.item_id
		WHERE mo.station_id = ? AND mo.captured_at = ?
	`, stationID, latest)
	if err != nil {
		return nil, fmt.Errorf("query latest orders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var o Order
		var capStr string
		if err := rows.Scan(&o.StationID, &o.ItemID, &o.ItemName, &o.Side, &o.PriceEach, &o.Quantity, &o.MyQuantity, &o.Source, &capStr); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		o.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		snap.Orders = append(snap.Orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orders: %w", err)
	}
	return snap, nil
}

// HasSnapshotToday reports whether any order for the station was captured
// today (UTC).
func (c *Collector) HasSnapshotToday(ctx context.Context, stationID string) (bool, error) {
	startOfDay := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	var n int
	err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM market_orders WHERE station_id = ? AND captured_at >= ?`,
		stationID, startOfDay).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count today's orders: %w", err)
	}
	return n > 0, nil
}

// FindBestPrices returns the best prices for an item on the given side across
// all stations, using each station's most recent order for that item.
// side "sell" ranks ascending (cheapest first); "buy" ranks descending.
func (c *Collector) FindBestPrices(ctx context.Context, itemID, side string, limit int) ([]BestPrice, error) {
	order := "ASC"
	if side == "buy" {
		order = "DESC"
	}
	// Latest order per station for this item+side, then rank by price.
	query := `
		SELECT mo.station_id, COALESCE(s.station_name, mo.station_id),
		       COALESCE(s.system_id, ''), COALESCE(s.system_name, ''),
		       mo.price_each, mo.quantity, mo.side, mo.captured_at
		FROM market_orders mo
		JOIN (
			SELECT station_id, MAX(captured_at) AS mx
			FROM market_orders
			WHERE item_id = ? AND side = ?
			GROUP BY station_id
		) latest ON latest.station_id = mo.station_id AND latest.mx = mo.captured_at
		LEFT JOIN stations s ON s.station_id = mo.station_id
		WHERE mo.item_id = ? AND mo.side = ?
		ORDER BY mo.price_each ` + order + `
		LIMIT ?`
	rows, err := c.db.QueryContext(ctx, query, itemID, side, itemID, side, limit)
	if err != nil {
		return nil, fmt.Errorf("query best prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []BestPrice
	for rows.Next() {
		bp := BestPrice{ItemID: itemID}
		var capStr string
		if err := rows.Scan(&bp.StationID, &bp.StationName, &bp.SystemID, &bp.SystemName,
			&bp.Price, &bp.Quantity, &bp.ListingType, &capStr); err != nil {
			return nil, fmt.Errorf("scan best price: %w", err)
		}
		bp.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		out = append(out, bp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate best prices: %w", err)
	}
	return out, nil
}

// StoreAnalysis inserts an LLM market-analysis record.
func (c *Collector) StoreAnalysis(ctx context.Context, a MarketAnalysis) error {
	insights, _ := json.Marshal(a.TopInsights)
	xp, _ := json.Marshal(a.XPGained)
	data, _ := json.Marshal(a.AnalysisData)
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO analyses (station_id, station_name, system_id, system_name,
				game_tick, captured_at, agent_id, mode, skill_level, scanning_range,
				stations_in_range, items_scanned, top_insights, total_items, total_pages,
				page, hint, xp_gained, analysis_data)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.StationID, a.StationName, a.SystemID, a.SystemName,
			a.GameTick, a.CapturedAt.UTC().Format(time.RFC3339), a.AgentID, a.Mode,
			a.SkillLevel, a.ScanningRange, a.StationsInRange, a.ItemsScanned,
			string(insights), a.TotalItems, a.TotalPages, a.Page, a.Hint,
			string(xp), string(data))
		return err
	})
}

// GetLatestAnalysis returns the most recent analysis for a station, or (nil, nil).
func (c *Collector) GetLatestAnalysis(ctx context.Context, stationID string) (*MarketAnalysis, error) {
	var a MarketAnalysis
	var capStr, insights, xp, data string
	err := c.db.QueryRowContext(ctx, `
		SELECT station_id, station_name, system_id, system_name, game_tick, captured_at,
		       agent_id, mode, skill_level, scanning_range, stations_in_range, items_scanned,
		       top_insights, total_items, total_pages, page, hint, xp_gained, analysis_data
		FROM analyses WHERE station_id = ?
		ORDER BY captured_at DESC LIMIT 1`, stationID).
		Scan(&a.StationID, &a.StationName, &a.SystemID, &a.SystemName, &a.GameTick, &capStr,
			&a.AgentID, &a.Mode, &a.SkillLevel, &a.ScanningRange, &a.StationsInRange, &a.ItemsScanned,
			&insights, &a.TotalItems, &a.TotalPages, &a.Page, &a.Hint, &xp, &data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest analysis: %w", err)
	}
	a.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
	_ = json.Unmarshal([]byte(insights), &a.TopInsights)
	_ = json.Unmarshal([]byte(xp), &a.XPGained)
	_ = json.Unmarshal([]byte(data), &a.AnalysisData)
	return &a, nil
}
