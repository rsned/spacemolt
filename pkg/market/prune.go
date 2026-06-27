package market

import (
	"context"
	"database/sql"
	"time"
)

// PruneOrders deletes market_orders rows whose capture bucket is older than the
// given cutoff, returning the number of rows removed.
//
// SQLite has no row TTL, so a retention window is enforced by calling this on a
// schedule (see cmd/tools/market-prune). Space is reclaimed lazily — freed pages
// are reused by later inserts, so the file stabilizes at roughly the
// retention-window peak rather than shrinking. Call Vacuum to shrink the file on
// disk.
func (c *Collector) PruneOrders(ctx context.Context, olderThan time.Time) (int64, error) {
	cutoff := olderThan.UTC().Format(time.RFC3339)
	var deleted int64
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM market_orders WHERE bucket_utc < ?`, cutoff)
		if err != nil {
			return err
		}
		n, aerr := res.RowsAffected()
		if aerr != nil {
			return aerr
		}
		deleted = n
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// Vacuum rebuilds the database file, reclaiming the space freed by deletes. It
// holds an exclusive lock for the full rewrite, so only run it when no other
// process is reading or writing the database.
func (c *Collector) Vacuum(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, `VACUUM`)
	return err
}
