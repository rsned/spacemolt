package market

import (
	"context"
	"database/sql"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// pruneBatchSize is how many market_orders rows one delete transaction removes.
//
// The size is a lock-duration budget, not a throughput knob. market_orders is
// the capture firehose (~0.85-2.4M rows/hour across the fleet), so a retention
// change or an outage can leave millions of rows to delete; doing that in one
// statement holds the write lock for its whole duration and every marketbot
// capture fails SQLITE_BUSY meanwhile.
const pruneBatchSize = 50_000

// pruneBatchPause yields the write lock between batches. Without it this loop
// re-acquires immediately and starves the very writers the batching protects,
// because SQLite has no write-fairness queue.
const pruneBatchPause = game.SleepQuick

// PruneOrders deletes market_orders rows whose capture bucket is older than the
// given cutoff, returning the number of rows removed.
//
// SQLite has no row TTL, so a retention window is enforced by calling this on a
// schedule (see cmd/tools/market-prune). Space is reclaimed lazily — freed pages
// are reused by later inserts, so the file stabilizes at roughly the
// retention-window peak rather than shrinking. Call Vacuum to shrink the file on
// disk.
//
// The delete is BATCHED, and that is load-bearing rather than an optimisation.
// Twice now a single-statement delete has taken the whole fleet down with it:
// 2026-07-30 (20.4M rows, ~10 min, 64.5 GB written, 144 SQLITE_BUSY failures and
// zero market rows captured) and 2026-08-18 (5.76M rows, 272 s, 489 SQLITE_BUSY
// on the marketbots alone). Both times the trigger was the same — downtime
// longer than the retention window makes EVERY row older than the cutoff, so a
// routine scheduled prune silently becomes a whole-table delete.
//
// When the keep-set is empty or nearly so, deleting is the wrong tool entirely:
// rebuild the file instead (see cmd/tools/market-rebuild), which writes only the
// rows worth keeping and needs no VACUUM. That path requires every writer to be
// down, so it belongs at cold start, before the fleets are launched.
func (c *Collector) PruneOrders(ctx context.Context, olderThan time.Time) (int64, error) {
	cutoff := olderThan.UTC().Format(time.RFC3339)

	var total int64
	for {
		var deleted int64
		err := c.writeRetry(ctx, func(tx *sql.Tx) error {
			// rowid is the batching key: an unqualified DELETE ... LIMIT is not
			// available unless SQLite was built with SQLITE_ENABLE_UPDATE_DELETE_LIMIT,
			// and the subquery form works on any build.
			res, err := tx.ExecContext(ctx, `
				DELETE FROM market_orders
				WHERE rowid IN (
					SELECT rowid FROM market_orders WHERE bucket_utc < ? LIMIT ?
				)`, cutoff, pruneBatchSize)
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
			// Report what was already committed: the batches are independent, so
			// a failure partway through has still done real work, and returning 0
			// would make the caller log a no-op prune.
			return total, err
		}
		total += deleted
		if deleted < pruneBatchSize {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(pruneBatchPause):
		}
	}
}

// Vacuum rebuilds the database file, reclaiming the space freed by deletes. It
// holds an exclusive lock for the full rewrite, so only run it when no other
// process is reading or writing the database.
func (c *Collector) Vacuum(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, `VACUUM`)

	return err
}
