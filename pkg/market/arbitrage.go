package market

import (
	"context"
	"fmt"
	"time"
)

// GetItemStationPrices returns one item's best ask and best bid per station, each
// computed from that station's latest capture. BestAsk is the cheapest sell order
// (where you would buy); BestBid is the highest buy order (where you would sell).
// AskQty/BidQty total the quantity of orders tying at that best price. Returns an
// empty slice when the item has no orders.
func (c *Collector) GetItemStationPrices(ctx context.Context, itemID string) ([]ItemStationPrice, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT o.station_id, COALESCE(s.station_name, o.station_id),
		       COALESCE(s.system_id, ''), COALESCE(s.system_name, ''),
		       o.side, o.price_each, o.quantity, o.captured_at
		FROM market_orders o
		JOIN stations s ON s.station_id = o.station_id
		JOIN (
			SELECT station_id, MAX(captured_at) AS mx
			FROM market_orders
			WHERE item_id = ?
			GROUP BY station_id
		) latest ON latest.station_id = o.station_id AND latest.mx = o.captured_at
		WHERE o.item_id = ?
		ORDER BY o.station_id`, itemID, itemID)
	if err != nil {
		return nil, fmt.Errorf("query item station prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	order := []string{}
	byStation := map[string]*ItemStationPrice{}
	for rows.Next() {
		var stID, stName, sysID, sysName, side, capStr string
		var price, qty float64
		if err := rows.Scan(&stID, &stName, &sysID, &sysName, &side, &price, &qty, &capStr); err != nil {
			return nil, fmt.Errorf("scan item station price: %w", err)
		}
		p, ok := byStation[stID]
		if !ok {
			p = &ItemStationPrice{StationID: stID, StationName: stName, SystemID: sysID, SystemName: sysName}
			byStation[stID] = p
			order = append(order, stID)
		}
		p.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		switch side {
		case "sell":
			switch {
			case !p.HasSell:
				p.BestAsk, p.AskQty, p.HasSell = price, qty, true
			case price < p.BestAsk:
				p.BestAsk, p.AskQty = price, qty
			case price == p.BestAsk:
				p.AskQty += qty
			}
		case "buy":
			switch {
			case !p.HasBuy:
				p.BestBid, p.BidQty, p.HasBuy = price, qty, true
			case price > p.BestBid:
				p.BestBid, p.BidQty = price, qty
			case price == p.BestBid:
				p.BidQty += qty
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate item station prices: %w", err)
	}
	out := make([]ItemStationPrice, 0, len(order))
	for _, stID := range order {
		out = append(out, *byStation[stID])
	}
	return out, nil
}
