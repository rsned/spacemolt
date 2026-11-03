package knowledge

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// historyPruneBatchSize is how many rows one market-history delete transaction
// removes. Like market.pruneBatchSize it is a lock-duration budget: the first
// prune after retention is introduced faces tens of millions of rows, and a
// single statement would hold the KB write lock against every fleet process.
const historyPruneBatchSize = 50_000

// historyPruneBatchPause yields the write lock between batches so fleet writers
// (sightings, demand captures) can interleave; SQLite has no fairness queue.
const historyPruneBatchPause = game.SleepQuick

// PruneMarketHistory deletes market_demand_history and market_supply_history
// rows whose hourly bucket is older than olderThan, returning the rows removed
// from each table. Deletes are batched (see historyPruneBatchSize). Freed pages
// are reused by later inserts; the file only shrinks after a VACUUM.
func (kb *SQLiteKB) PruneMarketHistory(ctx context.Context, olderThan time.Time) (demand, supply int64, err error) {
	demand, err = kb.pruneHistoryTable(ctx, "market_demand_history", olderThan)
	if err != nil {
		return demand, 0, err
	}
	supply, err = kb.pruneHistoryTable(ctx, "market_supply_history", olderThan)

	return demand, supply, err
}

// pruneHistoryTable batch-deletes rows of table older than olderThan. table is
// interpolated into the SQL, so only this file's constant names may be passed.
func (kb *SQLiteKB) pruneHistoryTable(ctx context.Context, table string, olderThan time.Time) (int64, error) {
	cutoff := olderThan.UTC().Format(time.RFC3339)
	query := `DELETE FROM ` + table + ` WHERE rowid IN (
		SELECT rowid FROM ` + table + ` WHERE bucket_utc < ? LIMIT ?)`

	var total int64
	for {
		var deleted int64
		err := kb.inTx(ctx, func(tx txer) error {
			res, err := tx.ExecContext(ctx, query, cutoff, historyPruneBatchSize)
			if err != nil {
				return err
			}
			deleted, err = res.RowsAffected()

			return err
		})
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < historyPruneBatchSize {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(historyPruneBatchPause):
		}
	}
}
