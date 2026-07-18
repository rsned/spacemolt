package market

import (
	"context"
	"fmt"
	"time"
)

// CurrentBuyOrder is one buy order from the most recent capture of its
// (station, item) pair — the market.db view of live demand. Source is the
// wire's order source ("station" = Station Manager, "worker" = one of our
// fleet's own standing orders, "" = another player).
type CurrentBuyOrder struct {
	StationID  string
	SystemID   string
	ItemID     string
	ItemName   string
	PriceEach  float64
	Quantity   float64
	MyQuantity float64
	Source     string
	CapturedAt time.Time
}

// LoadCurrentBuyOrders returns the buy orders that are LIVE in each station's
// most recent capture, across all stations. This is the demand report's data
// source: unlike the knowledge-DB buy-order ledger (written only on a worker's
// full docked view_market), market.db is fed continuously by the capture fleet,
// so "current" here is typically minutes old. Relies on the partial index
// idx_orders_buy_station_item_cap (side='buy') — without it this query walks the
// full table.
//
// Liveness gate: a naive MAX(captured_at) per (station, item) resurrects the
// last time an order was EVER seen, so an order that has since vanished from the
// book keeps showing up with a multi-hour-stale timestamp (and, being often the
// highest-value row, dominates the report). We instead keep a (station, item)
// row only when its latest capture is within one capture cycle (5 min < the
// ~10-min marketbot cadence, > the few-second intra-capture write stagger) of
// the STATION's latest capture — i.e. the item was present in the station's most
// recent snapshot. The station's latest capture is read from the already-joined
// stations.last_updated_utc (upsertStation stamps it with every snapshot, so it
// equals MAX(captured_at) for the station) — NOT a MAX(captured_at) GROUP BY
// station over market_orders, which forces a full scan of the whole buy side
// (~46M rows, 40+s) because captured_at trails item_id in the only buy index.
// The PK join to stations is effectively free and keeps `demand --item` at ~2s.
// Both sides go through datetime() so the "…Z" ISO strings and SQLite's
// space-form datetime output compare as normalized instants, not raw text (raw
// text would rank 'T' above ' ' and defeat the gate). Rarely-captured stations
// (e.g. pirate bases off the marketbot rotation) still surface their latest-
// known book — that IS their current snapshot — and the report flags it STALE
// past demandStaleAfter. COALESCE falls back to the row's own capture (gate
// passes) for any order whose station lacks a stations row.
//
// itemID, when non-empty, scopes the scan to that one item_id (pushed into the
// per-item latest CTE and the outer filter).
func (c *Collector) LoadCurrentBuyOrders(ctx context.Context, itemID string) ([]CurrentBuyOrder, error) {
	cteItem, outerItem := "", ""
	var args []any
	if itemID != "" {
		cteItem = " AND item_id = ?"
		outerItem = " AND o.item_id = ?"
		args = []any{itemID, itemID}
	}
	rows, err := c.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT station_id, item_id, MAX(captured_at) AS mx
			FROM market_orders
			WHERE side = 'buy'`+cteItem+`
			GROUP BY station_id, item_id
		)
		SELECT o.station_id, COALESCE(s.system_id, ''),
		       o.item_id, COALESCE(i.item_name, o.item_id),
		       o.price_each, o.quantity, COALESCE(o.my_quantity, 0),
		       COALESCE(o.source, ''), o.captured_at
		FROM market_orders o
		JOIN latest l ON l.station_id = o.station_id
		             AND l.item_id = o.item_id
		             AND l.mx = o.captured_at
		LEFT JOIN stations s ON s.station_id = o.station_id
		LEFT JOIN items i ON i.item_id = o.item_id
		WHERE o.side = 'buy' AND o.price_each > 0 AND o.quantity > 0`+outerItem+`
		  AND datetime(l.mx) >= datetime(COALESCE(s.last_updated_utc, l.mx), '-5 minutes')
		ORDER BY o.station_id, o.item_id, o.price_each DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query current buy orders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []CurrentBuyOrder
	for rows.Next() {
		var r CurrentBuyOrder
		var capStr string
		if err := rows.Scan(&r.StationID, &r.SystemID, &r.ItemID, &r.ItemName,
			&r.PriceEach, &r.Quantity, &r.MyQuantity, &r.Source, &capStr); err != nil {
			return nil, fmt.Errorf("scan current buy order: %w", err)
		}
		r.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current buy orders: %w", err)
	}
	return out, nil
}
