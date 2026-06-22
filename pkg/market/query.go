package market

import (
	"context"
	"database/sql"
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
		SELECT station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at
		FROM market_orders
		WHERE station_id = ? AND captured_at = ?
	`, stationID, latest)
	if err != nil {
		return nil, fmt.Errorf("query latest orders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var o Order
		var capStr string
		if err := rows.Scan(&o.StationID, &o.ItemID, &o.Side, &o.PriceEach, &o.Quantity, &o.MyQuantity, &o.Source, &capStr); err != nil {
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
