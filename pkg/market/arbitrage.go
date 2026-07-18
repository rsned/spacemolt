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
	fromStation, toStation, itemID  string
	buyPrice, sellPrice, qty, gross float64
	sourceUnits                     float64
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
					sourceUnits: src.AskQty,
				})
			}
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].gross > candidates[j].gross })
	if len(candidates) > opts.Limit {
		candidates = candidates[:opts.Limit]
	}

	expiresAt := now.Add(opts.ExpiresIn).Format(time.RFC3339)
	discoveredAt := now.Format(time.RFC3339)

	var res ScanResult
	err = c.writeRetry(ctx, func(tx *sql.Tx) error {
		// Read the prior cycle's per-route streak BEFORE expiring, so a route still
		// present this cycle continues its count. Keyed by (item, from, to); a route
		// absent last cycle (not available or claimed) restarts at 1 on reinsert.
		prior, err := priorRouteCycles(ctx, tx)
		if err != nil {
			return err
		}
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
			cyclesSeen := prior[routeKey(cand.itemID, cand.fromStation, cand.toStation)] + 1
			_, err := tx.ExecContext(ctx, `
				INSERT INTO arbitrage_opportunities
				  (from_station_id, to_station_id, item_id, action_type, buy_price, sell_price,
				   quantity, source_units, gross_profit, fuel_cost, travel_ticks, cargo_required, cycles_seen, status,
				   expires_at, discovered_at, discovered_by, notes)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				cand.fromStation, cand.toStation, cand.itemID, "buy_then_sell",
				cand.buyPrice, cand.sellPrice, cand.qty, cand.sourceUnits, cand.gross,
				0.0, 0, cand.qty, cyclesSeen, "available", expiresAt, discoveredAt, "arbitrage_scanner", "logistics:deferred")
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

// routeKey identifies an arbitrage route for streak tracking across scan cycles.
func routeKey(itemID, fromStation, toStation string) string {
	return itemID + "\x00" + fromStation + "\x00" + toStation
}

// priorRouteCycles returns the max cycles_seen per route among opportunities still
// in play (available or claimed) — i.e. those present in the immediately-prior scan.
// A completed or expired route is absent, so its streak is broken and restarts at 1.
func priorRouteCycles(ctx context.Context, tx *sql.Tx) (map[string]int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT item_id, from_station_id, to_station_id, MAX(cycles_seen)
		FROM arbitrage_opportunities
		WHERE status IN ('available','claimed')
		GROUP BY item_id, from_station_id, to_station_id`)
	if err != nil {
		return nil, fmt.Errorf("read prior route cycles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var item, from, to string
		var cycles sql.NullInt64
		if err := rows.Scan(&item, &from, &to, &cycles); err != nil {
			return nil, fmt.Errorf("scan prior route cycles: %w", err)
		}
		out[routeKey(item, from, to)] = int(cycles.Int64)
	}
	return out, rows.Err()
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
// Returns true if claimed, false if it was already claimed/expired/gone — including
// a row still flagged 'available' whose expires_at has passed, which is what a
// released-but-stale row looks like once the scanner's sweep has missed it.
func (c *Collector) ClaimOpportunity(ctx context.Context, id int, agentID string) (bool, error) {
	claimed := false
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE arbitrage_opportunities SET status='claimed', claimed_by=?, claimed_at=?
			 WHERE id=? AND status='available' AND `+notExpiredSQL,
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
			`UPDATE arbitrage_opportunities SET status='completed', completed_at=?
			 WHERE id=? AND claimed_by=? AND status='claimed'`,
			time.Now().UTC().Format(time.RFC3339), id, agentID)
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
//
// Rows that are still 'available' but past their expires_at are never served: the
// scanner's sweep is the only thing that flips available -> expired, so a dead
// scanner would otherwise freeze the pool and keep handing out gaps that closed
// hours ago — and because stale rows keep their inflated gross_profit, they would
// outrank genuinely fresh ones in this very ORDER BY. Other statuses are returned
// untouched so completed/expired history stays readable for reporting.
func (c *Collector) GetOpportunities(ctx context.Context, status string, limit int) ([]ArbitrageOpportunity, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := c.db.QueryContext(ctx,
		arbitrageSelectJoin+`
		WHERE (? = '' OR ao.status = ?)
		  AND (ao.status <> 'available' OR `+notExpiredSQL+`)
		ORDER BY ao.gross_profit DESC
		LIMIT ?`, status, status, limit)
	if err != nil {
		return nil, fmt.Errorf("query opportunities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanOpportunityRows(rows)
}

// notExpiredSQL is the wall-clock freshness predicate for an opportunity row.
//
// It must compare via julianday, NOT as strings: expires_at is stored RFC3339
// ("2026-07-16T07:48:03Z") while SQLite's datetime('now') yields a space-separated
// form ("2026-07-16 14:40:11"). Comparing those as text puts 'T' (0x54) above
// ' ' (0x20), so every same-day expired row falsely reads as live — the failure that
// kept the haul fleet chasing dead gaps for 10 hours on 2026-07-16.
//
// expires_at is left unqualified so this reads correctly both in the aliased
// SELECT join and in ClaimOpportunity's bare UPDATE; neither joined table
// (stations, items) carries the column, so it stays unambiguous.
const notExpiredSQL = `julianday(expires_at) > julianday('now')`

// arbitrageSelectJoin is the SELECT column list + station/item joins shared by every
// multi-row opportunity read, so the column order stays in lockstep with
// scanOpportunityRows. Callers append their own WHERE/ORDER/LIMIT.
const arbitrageSelectJoin = `
		SELECT ao.id, ao.from_station_id, COALESCE(fs.station_name, ''), COALESCE(fs.system_name, ''),
		       ao.to_station_id, COALESCE(ts.station_name, ''), COALESCE(ts.system_name, ''),
		       ao.item_id, COALESCE(i.item_name, ''), ao.action_type, ao.status,
		       ao.buy_price, ao.sell_price, ao.quantity, ao.source_units, ao.gross_profit,
		       ao.fuel_cost, ao.travel_ticks, ao.cargo_required,
		       ao.claimed_by, ao.claimed_at, ao.completed_at, ao.cycles_seen, ao.expires_at, ao.discovered_at, ao.notes
		FROM arbitrage_opportunities ao
		LEFT JOIN stations fs ON fs.station_id = ao.from_station_id
		LEFT JOIN stations ts ON ts.station_id = ao.to_station_id
		LEFT JOIN items i ON i.item_id = ao.item_id`

// scanOpportunityRows scans rows from a SELECT of arbitrageSelectJoin's columns
// (id, joined station/system/item names, prices, and claim + lifecycle fields).
func scanOpportunityRows(rows *sql.Rows) ([]ArbitrageOpportunity, error) {
	var out []ArbitrageOpportunity
	for rows.Next() {
		var o ArbitrageOpportunity
		var claimedBy, claimedAt, completedAt, notes sql.NullString
		var cyclesSeen sql.NullInt64
		if err := rows.Scan(&o.ID, &o.FromStationID, &o.FromStationName, &o.FromSystemName,
			&o.ToStationID, &o.ToStationName, &o.ToSystemName,
			&o.ItemID, &o.ItemName, &o.ActionType, &o.Status,
			&o.BuyPrice, &o.SellPrice, &o.Quantity, &o.SourceUnits, &o.GrossProfit,
			&o.FuelCost, &o.TravelTicks, &o.CargoRequired,
			&claimedBy, &claimedAt, &completedAt, &cyclesSeen, &o.ExpiresAt, &o.DiscoveredAt, &notes); err != nil {
			return nil, fmt.Errorf("scan opportunity: %w", err)
		}
		o.ClaimedBy = claimedBy.String
		o.ClaimedAt = claimedAt.String
		o.CompletedAt = completedAt.String
		o.CyclesSeen = int(cyclesSeen.Int64)
		o.Notes = notes.String
		out = append(out, o)
	}
	return out, rows.Err()
}

// ReleaseOpportunity returns a claimed opportunity to the available pool: status back
// to 'available' and the claim cleared. Only the claiming agent can release its own
// claim. Returns true if a row was released. Haulers call this when they abandon an
// opportunity *before buying*, so an abandoned haul does not leak its claim out of the
// pool (the historical cause of hundreds of stuck 'claimed' rows).
func (c *Collector) ReleaseOpportunity(ctx context.Context, id int, agentID string) (bool, error) {
	released := false
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE arbitrage_opportunities SET status='available', claimed_by=NULL, claimed_at=NULL
			 WHERE id=? AND claimed_by=? AND status='claimed'`, id, agentID)
		if err != nil {
			return fmt.Errorf("release opportunity: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			released = n > 0
		}
		return nil
	})
	return released, err
}

// GetClaimedByAgent returns opportunities currently claimed by agentID, most-recently
// claimed first. A hauler calls this on startup to resume a haul it was mid-run on when
// its process restarted: the in-memory active opportunity does not survive a restart,
// but the claim row does.
func (c *Collector) GetClaimedByAgent(ctx context.Context, agentID string) ([]ArbitrageOpportunity, error) {
	rows, err := c.db.QueryContext(ctx,
		arbitrageSelectJoin+`
		WHERE ao.status='claimed' AND ao.claimed_by=?
		ORDER BY ao.claimed_at DESC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("query claimed opportunities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanOpportunityRows(rows)
}
