package market

import (
	"context"
	"database/sql"
	"fmt"
)

// RecomputeStats reports the outcome of RecomputeContaminatedOHLCV.
type RecomputeStats struct {
	Contaminated int // rows found with high_price >= NotForSalePrice
	Recomputed   int // rows rebuilt cleanly from surviving raw orders
	Deleted      int // contaminated rows with no recoverable raw orders, removed
}

// contaminatedBucket identifies one market_ohlcv row poisoned by the sentinel.
type contaminatedBucket struct {
	stationID, itemID, side, bucket string
}

// RecomputeContaminatedOHLCV repairs market_ohlcv rows whose aggregates were
// poisoned by the not-for-sale sentinel (high_price >= NotForSalePrice). For each
// such bucket it takes the latest surviving raw orders in market_orders and, if
// any remain, replaces the row with a sentinel-filtered recomputation; if the raw
// orders have been pruned (retention is short) or were all sentinel, the
// unrecoverable contaminated row is deleted instead. Clean rows are never touched.
//
// The operation is idempotent: a second run finds no contaminated rows. Work is
// committed in batches so a large repair does not hold one giant transaction.
func (c *Collector) RecomputeContaminatedOHLCV(ctx context.Context, batchSize int) (RecomputeStats, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	var stats RecomputeStats

	targets, err := c.contaminatedBuckets(ctx)
	if err != nil {
		return stats, err
	}
	stats.Contaminated = len(targets)

	for start := 0; start < len(targets); start += batchSize {
		end := min(start+batchSize, len(targets))
		batch := targets[start:end]
		if err := c.writeRetry(ctx, func(tx *sql.Tx) error {
			for _, b := range batch {
				orders, err := latestBucketOrders(ctx, tx, b)
				if err != nil {
					return err
				}
				rows := computeOHLCV(orders, b.bucket) // sentinel already filtered here
				if len(rows) == 0 {
					// No recoverable data: drop the unrecoverable contaminated row.
					if _, err := tx.ExecContext(ctx,
						`DELETE FROM market_ohlcv WHERE station_id=? AND item_id=? AND side=? AND bucket_utc=?`,
						b.stationID, b.itemID, b.side, b.bucket); err != nil {
						return fmt.Errorf("delete contaminated bucket: %w", err)
					}
					stats.Deleted++
					continue
				}
				if err := c.upsertOHLCV(tx, rows[0]); err != nil {
					return fmt.Errorf("upsert recomputed bucket: %w", err)
				}
				stats.Recomputed++
			}
			return nil
		}); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// contaminatedBuckets returns every market_ohlcv row touched by the sentinel.
func (c *Collector) contaminatedBuckets(ctx context.Context) ([]contaminatedBucket, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT station_id, item_id, side, bucket_utc
		FROM market_ohlcv
		WHERE high_price >= ?`, NotForSalePrice)
	if err != nil {
		return nil, fmt.Errorf("query contaminated buckets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []contaminatedBucket
	for rows.Next() {
		var b contaminatedBucket
		if err := rows.Scan(&b.stationID, &b.itemID, &b.side, &b.bucket); err != nil {
			return nil, fmt.Errorf("scan contaminated bucket: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// latestBucketOrders loads the raw orders from the most recent capture within a
// bucket, reproducing the original last-write-wins-per-hour OHLCV semantics. The
// returned orders carry station/item/side so computeOHLCV groups them into one row.
func latestBucketOrders(ctx context.Context, tx *sql.Tx, b contaminatedBucket) ([]Order, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT price_each, quantity FROM market_orders
		WHERE station_id=? AND item_id=? AND side=? AND bucket_utc=?
		  AND captured_at = (
		    SELECT MAX(captured_at) FROM market_orders
		    WHERE station_id=? AND item_id=? AND side=? AND bucket_utc=?
		  )`,
		b.stationID, b.itemID, b.side, b.bucket,
		b.stationID, b.itemID, b.side, b.bucket)
	if err != nil {
		return nil, fmt.Errorf("query latest bucket orders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var orders []Order
	for rows.Next() {
		o := Order{StationID: b.stationID, ItemID: b.itemID, Side: b.side}
		if err := rows.Scan(&o.PriceEach, &o.Quantity); err != nil {
			return nil, fmt.Errorf("scan bucket order: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}
