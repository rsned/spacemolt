package market

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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

// arbCandidate is an in-memory opportunity before persistence.
type arbCandidate struct {
	fromStation, toStation, itemID        string
	buyPrice, sellPrice, qty, gross       float64
}

// ScanArbitrage detects cross-station buy-low/sell-high spreads from the latest
// market captures and persists them to arbitrage_opportunities, expiring any
// previously-available rows first (claimed/completed rows persist). Logistics
// (fuel/distance/ticks) are deferred to Phase 4b: fuel_cost=0, travel_ticks=0,
// cargo_required=quantity, notes='logistics:deferred'. Reads happen outside the
// write transaction so the write lock is held only briefly (important with ~40
// capturing agents); a capture landing mid-scan is harmless since opportunities
// are advisory.
func (c *Collector) ScanArbitrage(ctx context.Context, opts ScanOptions) (ScanResult, error) {
	if opts.MinProfit == 0 {
		opts.MinProfit = 1000
	}
	if opts.MinPrice == 0 {
		opts.MinPrice = 10
	}
	if opts.MinQuantity == 0 {
		opts.MinQuantity = 1
	}
	if opts.ExpiresIn == 0 {
		opts.ExpiresIn = 6 * time.Hour
	}
	if opts.Limit == 0 {
		opts.Limit = 500
	}

	itemIDs, err := c.scanItemSet(ctx, opts.Items)
	if err != nil {
		return ScanResult{}, err
	}

	now := time.Now().UTC()
	var candidates []arbCandidate
	for _, itemID := range itemIDs {
		prices, err := c.GetItemStationPrices(ctx, itemID)
		if err != nil {
			return ScanResult{}, fmt.Errorf("scan item %s: %w", itemID, err)
		}
		for _, src := range prices { // src = where you BUY (a sell/ask)
			if !src.HasSell || src.BestAsk < opts.MinPrice || src.AskQty < opts.MinQuantity {
				continue
			}
			for _, dst := range prices { // dst = where you SELL (a buy/bid)
				if dst.StationID == src.StationID {
					continue
				}
				if !dst.HasBuy || dst.BestBid < opts.MinPrice || dst.BidQty < opts.MinQuantity {
					continue
				}
				if dst.BestBid <= src.BestAsk {
					continue
				}
				qty := min(src.AskQty, dst.BidQty)
				gross := (dst.BestBid - src.BestAsk) * qty
				if gross < opts.MinProfit {
					continue
				}
				candidates = append(candidates, arbCandidate{
					fromStation: src.StationID,
					toStation:   dst.StationID,
					itemID:      itemID,
					buyPrice:    src.BestAsk,
					sellPrice:   dst.BestBid,
					qty:         qty,
					gross:       gross,
				})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].gross > candidates[j].gross })
	if len(candidates) > opts.Limit {
		candidates = candidates[:opts.Limit]
	}

	expiresAt := now.Add(opts.ExpiresIn).Format(time.RFC3339)
	discoveredAt := now.Format(time.RFC3339)

	var res ScanResult
	err = c.writeRetry(ctx, func(tx *sql.Tx) error {
		exp, err := tx.ExecContext(ctx, `UPDATE arbitrage_opportunities SET status='expired' WHERE status='available'`)
		if err != nil {
			return fmt.Errorf("expire opportunities: %w", err)
		}
		expired := 0
		if n, err := exp.RowsAffected(); err == nil {
			expired = int(n)
		}
		inserted := 0
		for _, cand := range candidates {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO arbitrage_opportunities
				  (from_station_id, to_station_id, item_id, action_type, buy_price, sell_price,
				   quantity, gross_profit, fuel_cost, travel_ticks, cargo_required, status,
				   expires_at, discovered_at, discovered_by, notes)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				cand.fromStation, cand.toStation, cand.itemID, "buy_then_sell",
				cand.buyPrice, cand.sellPrice, cand.qty, cand.gross,
				0.0, 0, cand.qty, "available", expiresAt, discoveredAt, "arbitrage_scanner", "logistics:deferred")
			if err != nil {
				return fmt.Errorf("insert opportunity: %w", err)
			}
			inserted++
		}
		res.Expired = expired
		res.Inserted = inserted
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}
	res.GeneratedAt = now
	return res, nil
}

// scanItemSet returns the items to scan: the allowlist when non-empty, otherwise
// every distinct item_id present in market_orders (only traded items can yield a
// spread).
func (c *Collector) scanItemSet(ctx context.Context, allow []string) ([]string, error) {
	if len(allow) > 0 {
		return allow, nil
	}
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT item_id FROM market_orders`)
	if err != nil {
		return nil, fmt.Errorf("query traded items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan traded item: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ClaimOpportunity atomically claims an available opportunity for agentID.
// Returns true if claimed, false if it was already claimed/expired/gone.
func (c *Collector) ClaimOpportunity(ctx context.Context, id int, agentID string) (bool, error) {
	claimed := false
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE arbitrage_opportunities SET status='claimed', claimed_by=?, claimed_at=?
			 WHERE id=? AND status='available'`,
			agentID, time.Now().UTC().Format(time.RFC3339), id)
		if err != nil {
			return fmt.Errorf("claim opportunity: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			claimed = n > 0
		}
		return nil
	})
	return claimed, err
}

// CompleteOpportunity atomically marks a claimed opportunity completed, but only
// if agentID owns the claim. Returns false (no error) if not owned/claimed.
func (c *Collector) CompleteOpportunity(ctx context.Context, id int, agentID string) (bool, error) {
	completed := false
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE arbitrage_opportunities SET status='completed'
			 WHERE id=? AND claimed_by=? AND status='claimed'`, id, agentID)
		if err != nil {
			return fmt.Errorf("complete opportunity: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			completed = n > 0
		}
		return nil
	})
	return completed, err
}

// GetOpportunities returns opportunities ordered by gross_profit DESC, optionally
// filtered to a status ("" = all). Station/system/item names are joined for
// display. Returns an empty slice when none match.
func (c *Collector) GetOpportunities(ctx context.Context, status string, limit int) ([]ArbitrageOpportunity, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT ao.id, ao.from_station_id, COALESCE(fs.station_name, ''), COALESCE(fs.system_name, ''),
		       ao.to_station_id, COALESCE(ts.station_name, ''), COALESCE(ts.system_name, ''),
		       ao.item_id, COALESCE(i.item_name, ''), ao.action_type, ao.status,
		       ao.buy_price, ao.sell_price, ao.quantity, ao.gross_profit,
		       ao.fuel_cost, ao.travel_ticks, ao.cargo_required,
		       ao.claimed_by, ao.claimed_at, ao.expires_at, ao.discovered_at, ao.notes
		FROM arbitrage_opportunities ao
		LEFT JOIN stations fs ON fs.station_id = ao.from_station_id
		LEFT JOIN stations ts ON ts.station_id = ao.to_station_id
		LEFT JOIN items i ON i.item_id = ao.item_id
		WHERE (? = '' OR ao.status = ?)
		ORDER BY ao.gross_profit DESC
		LIMIT ?`, status, status, limit)
	if err != nil {
		return nil, fmt.Errorf("query opportunities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ArbitrageOpportunity
	for rows.Next() {
		var o ArbitrageOpportunity
		var claimedBy, claimedAt, notes sql.NullString
		if err := rows.Scan(&o.ID, &o.FromStationID, &o.FromStationName, &o.FromSystemName,
			&o.ToStationID, &o.ToStationName, &o.ToSystemName,
			&o.ItemID, &o.ItemName, &o.ActionType, &o.Status,
			&o.BuyPrice, &o.SellPrice, &o.Quantity, &o.GrossProfit,
			&o.FuelCost, &o.TravelTicks, &o.CargoRequired,
			&claimedBy, &claimedAt, &o.ExpiresAt, &o.DiscoveredAt, &notes); err != nil {
			return nil, fmt.Errorf("scan opportunity: %w", err)
		}
		o.ClaimedBy = claimedBy.String
		o.ClaimedAt = claimedAt.String
		o.Notes = notes.String
		out = append(out, o)
	}
	return out, rows.Err()
}
