package market

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UnbuyableTTL is how long an item stays blocked after the server rejects a buy
// for it. Long enough that the fleet stops paying for the same lesson every few
// minutes, short enough that a server-side fix (or a one-off hiccup that looked
// permanent) costs at most one wasted buy per item per week to discover.
const UnbuyableTTL = 7 * 24 * time.Hour

// MarkUnbuyable records that the server refused to trade itemID, blocking it from
// arbitrage scanning until the TTL lapses, and expires the open rows it has already
// produced. Re-reporting an already-blocked item extends the block and counts a hit,
// so the table doubles as a record of which items cost the fleet the most.
//
// Expiring the rows is cleanup, not the fix: the next scan rebuilds the whole pool
// from scratch, so the block in unbuyable_items is what actually keeps the item out.
func (c *Collector) MarkUnbuyable(ctx context.Context, itemID, agentID, reason string) error {
	if itemID == "" {
		return fmt.Errorf("mark unbuyable: empty item id")
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	until := now.Add(UnbuyableTTL).Format(time.RFC3339)
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO unbuyable_items
			  (item_id, reason, reported_by, hits, first_seen_utc, last_seen_utc, blocked_until)
			VALUES (?,?,?,1,?,?,?)
			ON CONFLICT(item_id) DO UPDATE SET
			  reason=excluded.reason,
			  reported_by=excluded.reported_by,
			  hits=unbuyable_items.hits+1,
			  last_seen_utc=excluded.last_seen_utc,
			  blocked_until=excluded.blocked_until`,
			itemID, reason, agentID, nowStr, nowStr, until); err != nil {
			return fmt.Errorf("mark unbuyable %s: %w", itemID, err)
		}
		note := fmt.Sprintf("unbuyable: %s (by %s)", reason, agentID)
		if _, err := tx.ExecContext(ctx, `
			UPDATE arbitrage_opportunities SET status='expired', notes=?
			 WHERE item_id=? AND (status='available' OR (status='claimed' AND claimed_by=?))`,
			note, itemID, agentID); err != nil {
			return fmt.Errorf("expire unbuyable opps %s: %w", itemID, err)
		}
		return nil
	})
}

// UnbuyableItems returns the set of item ids currently blocked from scanning.
// Rows whose block has lapsed are not returned (and are left in place, so their
// hit counts survive for diagnosis).
func (c *Collector) UnbuyableItems(ctx context.Context) (map[string]bool, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT item_id FROM unbuyable_items WHERE blocked_until > ?`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("query unbuyable items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan unbuyable item: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}
