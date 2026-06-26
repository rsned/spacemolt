package market

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	// market_orders can hold tens of millions of rows, so COUNT(*) is a full-table scan
	// that takes minutes and stalls the dashboard. Read the AUTOINCREMENT high-water mark
	// (sqlite_sequence) instead — O(1), and equal to the live row count until rows are
	// pruned. COALESCE handles the never-inserted case (no sqlite_sequence row).
	if err := c.db.QueryRowContext(ctx,
		"SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='market_orders'), 0)").Scan(&stats.OrderCount); err != nil {
		return nil, fmt.Errorf("count orders: %w", err)
	}
	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_ohlcv").Scan(&stats.OHLCVCount); err != nil {
		return nil, fmt.Errorf("count ohlcv: %w", err)
	}

	// Latest capture via MAX over the indexed bucket column (idx_orders_bucket) is O(1);
	// MAX(captured_at) has no leading index and would scan the whole table. NULL (empty
	// table) scans into a nullable -> "".
	var latest sql.NullString
	if err := c.db.QueryRowContext(ctx, "SELECT MAX(bucket_utc) FROM market_orders").Scan(&latest); err != nil {
		return nil, fmt.Errorf("latest capture: %w", err)
	}
	stats.LatestCapture = latest.String // "" when NULL/empty

	return &stats, nil
}

// latestOrderBucket returns the most recent capture bucket (via idx_orders_bucket, O(1)),
// or "" when there are no orders. Dashboard reads anchor on it so they touch only the
// latest period's rows instead of scanning the full (tens-of-millions-row) history.
func (c *Collector) latestOrderBucket(ctx context.Context) (string, error) {
	var b sql.NullString
	if err := c.db.QueryRowContext(ctx, "SELECT MAX(bucket_utc) FROM market_orders").Scan(&b); err != nil {
		return "", fmt.Errorf("latest order bucket: %w", err)
	}
	return b.String, nil
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
	if errors.Is(err, sql.ErrNoRows) || latest == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest captured_at: %w", err)
	}

	snap := &MarketSnapshot{StationID: stationID}
	// Station metadata is best-effort: a missing stations row (ErrNoRows) is
	// fine, but a real query error should surface.
	if metaErr := c.db.QueryRowContext(ctx,
		`SELECT station_name, system_id, system_name FROM stations WHERE station_id = ?`, stationID).
		Scan(&snap.StationName, &snap.SystemID, &snap.SystemName); metaErr != nil && !errors.Is(metaErr, sql.ErrNoRows) {
		return nil, fmt.Errorf("query station metadata: %w", metaErr)
	}
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
	if errors.Is(err, sql.ErrNoRows) {
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

// GetMatrix returns a paginated items×stations matrix. For each matching item and
// each station, the cell aggregates the station's latest capture of that item:
// BestSell = cheapest ask, BestBuy = highest bid, VWAP/Volume over sell orders,
// OrderCount over both sides. Cells are dense-packed: every MatrixItem.Cells has
// one entry per station in Matrix.Stations order, so item×station pairs with no
// orders are filled with station-only metadata (HasSell/HasBuy both false), which
// callers render as "—".
func (c *Collector) GetMatrix(ctx context.Context, q MatrixQuery) (*Matrix, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 50
	}
	offset := (q.Page - 1) * q.Limit
	search := "%" + strings.ToLower(q.Search) + "%"

	// 1. Stations (columns).
	stations, err := c.stations(ctx)
	if err != nil {
		return nil, err
	}

	// 2. Total matching items (before pagination).
	var total int
	countErr := c.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM items
		WHERE (? = '' OR category = ?)
		  AND (? = '%%' OR LOWER(item_id) LIKE ? OR LOWER(item_name) LIKE ?)`,
		q.Category, q.Category, search, search, search).Scan(&total)
	if countErr != nil {
		return nil, fmt.Errorf("count matrix items: %w", countErr)
	}

	m := &Matrix{
		Stations:    stations,
		TotalItems:  total,
		Page:        q.Page,
		Limit:       q.Limit,
		GeneratedAt: time.Now().UTC(),
		Items:       []MatrixItem{},
	}
	if total == 0 || offset >= total {
		return m, nil
	}

	// 3. Paginated item IDs for this page.
	itemRows, err := c.db.QueryContext(ctx, `
		SELECT item_id, item_name, COALESCE(category,'') FROM items
		WHERE (? = '' OR category = ?)
		  AND (? = '%%' OR LOWER(item_id) LIKE ? OR LOWER(item_name) LIKE ?)
		ORDER BY item_id LIMIT ? OFFSET ?`,
		q.Category, q.Category, search, search, search, q.Limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query matrix items: %w", err)
	}
	type itemMeta struct{ id, name, category string }
	var pageItems []itemMeta
	for itemRows.Next() {
		var im itemMeta
		if err := itemRows.Scan(&im.id, &im.name, &im.category); err != nil {
			return nil, fmt.Errorf("scan matrix item: %w", err)
		}
		pageItems = append(pageItems, im)
	}
	if err := itemRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matrix items: %w", err)
	}
	_ = itemRows.Close()

	if len(pageItems) == 0 {
		return m, nil
	}

	// 4. Latest-capture aggregates for these items across all stations.
	ids := make([]any, 0, len(pageItems))
	for _, im := range pageItems {
		ids = append(ids, im.id)
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]

	// Anchor on the latest capture bucket so the join touches only the current period's
	// rows (idx_orders_bucket), not every order ever captured for these items.
	bucket, err := c.latestOrderBucket(ctx)
	if err != nil {
		return nil, err
	}
	args := []any{bucket}
	args = append(args, ids...)

	query := `
		SELECT o.station_id, COALESCE(s.station_name, o.station_id),
		       COALESCE(s.system_id, ''), COALESCE(s.system_name, ''),
		       o.item_id,
		       MIN(CASE WHEN o.side = 'sell' THEN o.price_each END) AS best_sell,
		       MAX(CASE WHEN o.side = 'buy'  THEN o.price_each END) AS best_buy,
		       COALESCE(SUM(CASE WHEN o.side = 'sell' THEN o.price_each * o.quantity END), 0) * 1.0 /
		       NULLIF(SUM(CASE WHEN o.side = 'sell' THEN o.quantity END), 0) AS vwap,
		       COALESCE(SUM(CASE WHEN o.side = 'sell' THEN o.quantity END), 0) AS volume,
		       COUNT(*) AS order_count, MAX(o.captured_at) AS captured_at
		FROM market_orders o
		JOIN stations s ON s.station_id = o.station_id
		JOIN (
			SELECT station_id, item_id, MAX(captured_at) AS mx
			FROM market_orders
			WHERE bucket_utc = ? AND item_id IN (` + placeholders + `)
			GROUP BY station_id, item_id
		) latest ON latest.station_id = o.station_id AND latest.item_id = o.item_id AND latest.mx = o.captured_at
		WHERE o.bucket_utc = ? AND o.item_id IN (` + placeholders + `)
		GROUP BY o.station_id, o.item_id`
	args = append(args, bucket)
	args = append(args, ids...)
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query matrix cells: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type cellKey struct{ station, item string }
	cells := make(map[cellKey]MatrixCell)
	for rows.Next() {
		var cc MatrixCell
		var itemID, capStr string
		var bestSell, bestBuy sql.NullFloat64
		var vwap sql.NullFloat64
		if err := rows.Scan(&cc.StationID, &cc.StationName, &cc.SystemID, &cc.SystemName,
			&itemID, &bestSell, &bestBuy, &vwap, &cc.Volume, &cc.OrderCount, &capStr); err != nil {
			return nil, fmt.Errorf("scan matrix cell: %w", err)
		}
		if bestSell.Valid {
			cc.BestSell = bestSell.Float64
			cc.HasSell = true
		}
		if bestBuy.Valid {
			cc.BestBuy = bestBuy.Float64
			cc.HasBuy = true
		}
		if vwap.Valid {
			cc.VWAP = vwap.Float64
		}
		cc.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		cells[cellKey{cc.StationID, itemID}] = cc
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matrix cells: %w", err)
	}

	// 5. Assemble rows aligned to stations order.
	for _, im := range pageItems {
		row := MatrixItem{ItemID: im.id, ItemName: im.name, Category: im.category}
		for _, st := range stations {
			if cc, ok := cells[cellKey{st.StationID, im.id}]; ok {
				row.Cells = append(row.Cells, cc)
			} else {
				row.Cells = append(row.Cells, MatrixCell{StationID: st.StationID, StationName: st.StationName, SystemID: st.SystemID, SystemName: st.SystemName})
			}
		}
		m.Items = append(m.Items, row)
	}
	return m, nil
}

// GetStationOrders returns the orders from a station's latest capture, optionally
// filtered to itemID, ordered by side then price. Returns an empty slice when the
// station has no orders.
func (c *Collector) GetStationOrders(ctx context.Context, stationID, itemID string) ([]Order, error) {
	// Anchor on the latest capture bucket so the station's latest-capture lookup touches
	// only the current period's rows, not the station's entire (multi-million-row) history.
	bucket, err := c.latestOrderBucket(ctx)
	if err != nil {
		return nil, err
	}
	query := `
		SELECT station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at
		FROM market_orders
		WHERE station_id = ? AND bucket_utc = ? AND captured_at = (
				SELECT MAX(captured_at) FROM market_orders WHERE station_id = ? AND bucket_utc = ?
			)` + itemFilter(itemID) + `
		ORDER BY side, price_each`
	args := []any{stationID, bucket, stationID, bucket}
	if itemID != "" {
		args = append(args, itemID)
	}
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query station orders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Order
	for rows.Next() {
		var o Order
		var capStr string
		if err := rows.Scan(&o.StationID, &o.ItemID, &o.Side, &o.PriceEach, &o.Quantity, &o.MyQuantity, &o.Source, &capStr); err != nil {
			return nil, fmt.Errorf("scan station order: %w", err)
		}
		o.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		out = append(out, o)
	}
	return out, rows.Err()
}

// itemFilter returns the SQL fragment narrowing to itemID when non-empty.
// The caller binds itemID as the next parameter in both branches.
func itemFilter(itemID string) string {
	if itemID == "" {
		return ""
	}
	return ` AND item_id = ?`
}

// stations returns all stations ordered by name.
func (c *Collector) stations(ctx context.Context) ([]Station, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT station_id, station_name, system_id, system_name, first_seen_utc, last_updated_utc FROM stations ORDER BY station_name`)
	if err != nil {
		return nil, fmt.Errorf("query stations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Station
	for rows.Next() {
		var s Station
		if err := rows.Scan(&s.StationID, &s.StationName, &s.SystemID, &s.SystemName, &s.FirstSeenUTC, &s.LastUpdatedUTC); err != nil {
			return nil, fmt.Errorf("scan station: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetItemPriceHistory returns an item's OHLCV buckets across stations, newest
// first. VWAP is the robust series (close is last-order-in-snapshot and can be
// noisy for thin items). Empty slice when the item has no history.
func (c *Collector) GetItemPriceHistory(ctx context.Context, itemID string, limit int) ([]ItemPricePoint, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT o.station_id, COALESCE(s.station_name, o.station_id), o.side, o.bucket_utc,
		       o.vwap, o.high_price, o.low_price, o.volume, o.trade_count
		FROM market_ohlcv o
		JOIN stations s ON s.station_id = o.station_id
		WHERE o.item_id = ?
		ORDER BY o.bucket_utc DESC
		LIMIT ?`, itemID, limit)
	if err != nil {
		return nil, fmt.Errorf("query item price history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ItemPricePoint
	for rows.Next() {
		var p ItemPricePoint
		if err := rows.Scan(&p.StationID, &p.StationName, &p.Side, &p.BucketUTC,
			&p.VWAP, &p.High, &p.Low, &p.Volume, &p.TradeCount); err != nil {
			return nil, fmt.Errorf("scan price point: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetCaptureHealth returns per-station capture history: distinct captured_at
// timestamps (newest first), count, and earliest/latest. Used to spot cadence
// gaps in collection.
func (c *Collector) GetCaptureHealth(ctx context.Context) ([]StationCaptures, error) {
	// Bound to the latest capture bucket: grouping (station, captured_at) over the whole
	// market_orders table (tens of millions of rows) is a multi-minute scan. The current
	// period's captures are what reveal whether collection is live.
	bucket, err := c.latestOrderBucket(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT s.station_id, s.station_name, s.system_id, s.system_name, o.captured_at
		FROM stations s
		JOIN market_orders o ON o.station_id = s.station_id AND o.bucket_utc = ?
		GROUP BY s.station_id, o.captured_at
		ORDER BY s.station_name, o.captured_at DESC`, bucket)
	if err != nil {
		return nil, fmt.Errorf("query capture health: %w", err)
	}
	defer func() { _ = rows.Close() }()
	order := []string{}
	byStation := map[string]*StationCaptures{}
	times := map[string][]string{}
	for rows.Next() {
		var stID, stName, sysID, sysName, cap string
		if err := rows.Scan(&stID, &stName, &sysID, &sysName, &cap); err != nil {
			return nil, fmt.Errorf("scan capture health: %w", err)
		}
		sc, ok := byStation[stID]
		if !ok {
			sc = &StationCaptures{StationID: stID, StationName: stName, SystemID: sysID, SystemName: sysName, Latest: cap}
			byStation[stID] = sc
			order = append(order, stID)
		}
		sc.Earliest = cap // rows are DESC per station, so last seen is earliest
		sc.Count++
		times[stID] = append(times[stID], cap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate capture health: %w", err)
	}
	out := make([]StationCaptures, 0, len(order))
	for _, stID := range order {
		sc := *byStation[stID]
		sc.CaptureTimes = times[stID]
		out = append(out, sc)
	}
	return out, nil
}
