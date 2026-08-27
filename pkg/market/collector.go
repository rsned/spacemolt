package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Config holds configuration for the market database.
type Config struct {
	DBPath       string
	WAL          bool
	MaxOpenConns int
	MaxIdleConns int
	BusyTimeout  time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		DBPath:       filepath.Join("data", "market.db"),
		WAL:          true,
		MaxOpenConns: 25,
		MaxIdleConns: 5,
		BusyTimeout:  15 * time.Second,
	}
}

// Collector handles market data collection.
type Collector struct {
	db *sql.DB
}

// Open creates a new collector with the market database.
func Open(cfg Config) (*Collector, error) {
	if cfg.DBPath == "" {
		cfg = DefaultConfig()
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = DefaultConfig().MaxOpenConns
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = DefaultConfig().MaxIdleConns
	}
	if cfg.BusyTimeout == 0 {
		cfg.BusyTimeout = DefaultConfig().BusyTimeout
	}

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}

	// Apply PRAGMAs through the DSN rather than db.Exec("PRAGMA ..."): DSN
	// pragmas run on EVERY connection the pool opens, whereas db.Exec only
	// affects the one connection it lands on. With ~40 agents sharing this
	// database and a multi-connection pool, every connection must inherit
	// busy_timeout (and WAL) for contention to resolve as a clean blocking
	// wait instead of an immediate SQLITE_BUSY.
	db, err := sql.Open("sqlite", sqliteDSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Collector{db: db}, nil
}

// sqliteDSN builds a modernc.org/sqlite DSN that applies per-connection
// PRAGMAs (busy_timeout, and journal_mode=WAL when enabled) to every pooled
// connection.
func sqliteDSN(cfg Config) string {
	dsn := cfg.DBPath + "?_pragma=busy_timeout(" + strconv.Itoa(int(cfg.BusyTimeout.Milliseconds())) + ")"
	if cfg.WAL {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	// Take the write lock at BEGIN (never upgrade read->write): this is what makes
	// the book-admission transaction deadlock-free across the ~40 worker processes
	// that share this database. Every writeRetry transaction is a write, so
	// IMMEDIATE is correct for all of them.
	dsn += "&_txlock=immediate"
	return dsn
}

// Close closes the database connection.
func (c *Collector) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// maxRetryAttempts is the number of retries for SQLITE_BUSY.
//
// The fleet runs ~153 workers, every one of which holds market.db open
// read-write for its whole lifetime. Five attempts gave up while the lock was
// still merely contended rather than deadlocked: marketbot captures failed at a
// steady 4-6/min, and market-prune could not drain a backlog at all. A plain
// write with a longer wait succeeds, so the cure is patience, not a bigger
// database.
const maxRetryAttempts = 24

// baseRetryDelay is the initial retry delay.
const baseRetryDelay = 50 * time.Millisecond

// maxRetryDelay caps the exponential backoff. Without it, the later attempts
// grow past an hour and a "retry" becomes a hang.
const maxRetryDelay = 2 * time.Second

// retryDelay returns the backoff before the given attempt (1-based), doubling
// from baseRetryDelay and flattening at maxRetryDelay.
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	d := baseRetryDelay
	for range attempt - 1 {
		d *= 2
		if d >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	return d
}

// writeRetry executes a write operation with exponential backoff retry on
// SQLITE_BUSY / database-locked errors.
func (c *Collector) writeRetry(ctx context.Context, fn func(tx *sql.Tx) error) error {
	var lastErr error
	for attempt := range maxRetryAttempts {
		if attempt > 0 {
			delay := retryDelay(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		tx, err := c.db.Begin()
		if err != nil {
			if isBusyError(err) {
				lastErr = err
				continue
			}
			return err
		}

		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			if isBusyError(err) {
				lastErr = err
				continue
			}
			return err
		}

		if err := tx.Commit(); err != nil {
			if isBusyError(err) {
				lastErr = err
				continue
			}
			return err
		}

		return nil
	}
	return fmt.Errorf("market: write failed after %d attempts: %w", maxRetryAttempts, lastErr)
}

// isBusyError reports whether err is a SQLite BUSY/locked condition worth retrying.
func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database is busy") ||
		strings.Contains(msg, "SQLITE_BUSY")
}

// upsertStation adds or updates a station record.
func (c *Collector) upsertStation(tx *sql.Tx, s Station) error {
	_, err := tx.Exec(`
		INSERT INTO stations (station_id, station_name, system_id, system_name, first_seen_utc, last_updated_utc)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(station_id) DO UPDATE SET
			station_name = excluded.station_name,
			system_id = excluded.system_id,
			system_name = excluded.system_name,
			last_updated_utc = excluded.last_updated_utc
	`, s.StationID, s.StationName, s.SystemID, s.SystemName, s.FirstSeenUTC, s.LastUpdatedUTC)
	return err
}

// UpsertStation writes one stations row outside a capture transaction —
// NearestFuel's station→system resolution reads this table, and tests (or
// backfills) need a way to seed it directly.
func (c *Collector) UpsertStation(ctx context.Context, s Station) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		return c.upsertStation(tx, s)
	})
}

// UpsertStationFuel writes the latest fuel price for a station, replacing any
// existing row (one row per station — no time growth).
func (c *Collector) UpsertStationFuel(ctx context.Context, s StationFuel) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		// Reserve columns update only when the incoming capture actually
		// observed them, which parseGetBaseFuel signals by stamping
		// ReserveObservedAt. Guarding on the stamp (not on the values) keeps
		// two failure modes out of the table: an old-format payload erasing a
		// real reading, and a zero-valued StationFuel{} from a price-only
		// caller writing FuelReserve 0 — which reads as "measured dry".
		obs := s.ReserveObservedAt != ""
		fr, fc, ffr, ffc := s.FuelReserve, s.FuelCapacity, s.FactionFuelReserve, s.FactionFuelCapacity
		if !obs {
			fr, fc, ffr, ffc = -1, -1, -1, -1
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO station_fuel_prices
			  (station_id, fuel_price, fuel_tax_per_unit, fuel_price_all_in, captured_at, captured_by,
			   fuel_reserve, fuel_capacity, faction_fuel_reserve, faction_fuel_capacity, faction_id, reserve_observed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(station_id) DO UPDATE SET
				fuel_price = excluded.fuel_price,
				fuel_tax_per_unit = excluded.fuel_tax_per_unit,
				fuel_price_all_in = excluded.fuel_price_all_in,
				captured_at = excluded.captured_at,
				captured_by = excluded.captured_by,
				fuel_reserve = CASE WHEN excluded.reserve_observed_at != '' AND excluded.fuel_reserve >= 0
					THEN excluded.fuel_reserve ELSE station_fuel_prices.fuel_reserve END,
				fuel_capacity = CASE WHEN excluded.reserve_observed_at != '' AND excluded.fuel_capacity >= 0
					THEN excluded.fuel_capacity ELSE station_fuel_prices.fuel_capacity END,
				faction_fuel_reserve = CASE WHEN excluded.reserve_observed_at != '' AND excluded.faction_fuel_capacity >= 0
					THEN excluded.faction_fuel_reserve ELSE station_fuel_prices.faction_fuel_reserve END,
				faction_fuel_capacity = CASE WHEN excluded.reserve_observed_at != '' AND excluded.faction_fuel_capacity >= 0
					THEN excluded.faction_fuel_capacity ELSE station_fuel_prices.faction_fuel_capacity END,
				faction_id = CASE WHEN excluded.faction_id != ''
					THEN excluded.faction_id ELSE station_fuel_prices.faction_id END,
				reserve_observed_at = CASE WHEN excluded.reserve_observed_at != ''
					THEN excluded.reserve_observed_at ELSE station_fuel_prices.reserve_observed_at END
		`, s.StationID, s.FuelPrice, s.FuelTaxPerUnit, s.FuelPriceAllIn, s.CapturedAt, s.CapturedBy,
			fr, fc, ffr, ffc, s.FactionID, s.ReserveObservedAt)
		return err
	})
}

// MarkDeskDry records a live station_fuel_empty refusal: a measured-dry (0)
// reserve reading stamped now, newer than whatever the hourly capture said.
// Inserts a placeholder row (prices 0) for stations never price-captured, so
// the observation is not lost.
func (c *Collector) MarkDeskDry(ctx context.Context, stationID, observedBy string, now time.Time) error {
	if c == nil || stationID == "" {
		return nil
	}
	at := now.UTC().Format(time.RFC3339)
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO station_fuel_prices
			  (station_id, fuel_price, fuel_tax_per_unit, fuel_price_all_in, captured_at, captured_by,
			   fuel_reserve, reserve_observed_at)
			VALUES (?, 0, 0, 0, ?, ?, 0, ?)
			ON CONFLICT(station_id) DO UPDATE SET
				fuel_reserve = 0,
				reserve_observed_at = excluded.reserve_observed_at
		`, stationID, at, observedBy, at)
		return err
	})
}

// GetStationFuelPrice returns the latest captured all-in fuel price for a
// station. ok is false (no error) when no price has been captured yet.
func (c *Collector) GetStationFuelPrice(ctx context.Context, stationID string) (allIn int, capturedAt time.Time, ok bool, err error) {
	var at string
	scanErr := c.db.QueryRowContext(ctx,
		`SELECT fuel_price_all_in, captured_at FROM station_fuel_prices WHERE station_id = ?`,
		stationID).Scan(&allIn, &at)
	if errors.Is(scanErr, sql.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if scanErr != nil {
		return 0, time.Time{}, false, scanErr
	}
	t, perr := time.Parse(time.RFC3339, at)
	if perr != nil {
		return allIn, time.Time{}, true, nil //nolint:nilerr // price is valid even if the stamp won't parse
	}
	return allIn, t, true, nil
}

// MedianStationFuelAllIn returns the median captured all-in fuel price across all
// stations. ok is false (no error) when no prices have been captured yet.
func (c *Collector) MedianStationFuelAllIn(ctx context.Context) (median int, ok bool, err error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT fuel_price_all_in FROM station_fuel_prices ORDER BY fuel_price_all_in`)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close() //nolint:errcheck
	var vals []int
	for rows.Next() {
		var v int
		if scanErr := rows.Scan(&v); scanErr != nil {
			return 0, false, scanErr
		}
		vals = append(vals, v)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return 0, false, rowsErr
	}
	if len(vals) == 0 {
		return 0, false, nil
	}
	n := len(vals)
	if n%2 == 1 {
		return vals[n/2], true, nil
	}
	return (vals[n/2-1] + vals[n/2]) / 2, true, nil
}

// upsertItem adds or updates an item record.
func (c *Collector) upsertItem(tx *sql.Tx, item Item) error {
	_, err := tx.Exec(`
		INSERT INTO items (item_id, item_name, category, first_seen_utc, last_updated_utc)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			item_name = excluded.item_name,
			category = excluded.category,
			last_updated_utc = excluded.last_updated_utc
	`, item.ItemID, item.ItemName, item.Category, item.FirstSeenUTC, item.LastUpdatedUTC)
	return err
}

// insertOrders adds order rows within a transaction.
func (c *Collector) insertOrders(tx *sql.Tx, orders []Order) error {
	if len(orders) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`
		INSERT INTO market_orders (station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at, bucket_utc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, o := range orders {
		if _, err := stmt.Exec(o.StationID, o.ItemID, o.Side, o.PriceEach, o.Quantity, o.MyQuantity, o.Source, o.CapturedAt.UTC().Format(time.RFC3339), o.BucketUTC); err != nil {
			return err
		}
	}
	return nil
}

// ohlcvAccumulator tallies OHLCV stats per (station, item, side) group.
type ohlcvAccumulator struct {
	stationID, itemID, side string
	open, high, low, close  float64
	volume                  float64
	sumPriceTimesQty        float64 // For VWAP calculation
	tradeCount              int
	firstPriceSet           bool
}

// computeOHLCV calculates OHLCV + VWAP from orders grouped by (station, item, side).
// Orders with non-positive price or quantity are skipped. Group iteration order is
// preserved (first-seen) for deterministic output.
func computeOHLCV(orders []Order, bucketUTC string) []OHLCV {
	key := func(stationID, itemID, side string) string {
		return stationID + "\x00" + itemID + "\x00" + side
	}

	accs := make(map[string]*ohlcvAccumulator)
	order := []string{} // preserve first-seen order for deterministic output

	for _, o := range orders {
		// Skip non-positive quantities and any rung at/above the not-for-sale
		// sentinel: those never trade and must not pollute vwap/volume/high.
		if o.Quantity <= 0 || !tradeablePrice(o.PriceEach) {
			continue
		}
		k := key(o.StationID, o.ItemID, o.Side)
		acc, ok := accs[k]
		if !ok {
			acc = &ohlcvAccumulator{
				stationID: o.StationID,
				itemID:    o.ItemID,
				side:      o.Side,
			}
			accs[k] = acc
			order = append(order, k)
		}

		acc.volume += o.Quantity
		acc.sumPriceTimesQty += o.PriceEach * o.Quantity
		acc.tradeCount++

		if !acc.firstPriceSet {
			acc.open = o.PriceEach
			acc.high = o.PriceEach
			acc.low = o.PriceEach
			acc.close = o.PriceEach
			acc.firstPriceSet = true
		} else {
			acc.close = o.PriceEach
			if o.PriceEach > acc.high {
				acc.high = o.PriceEach
			}
			if o.PriceEach < acc.low {
				acc.low = o.PriceEach
			}
		}
	}

	result := make([]OHLCV, 0, len(order))
	for _, k := range order {
		acc := accs[k]
		vwap := 0.0
		if acc.volume > 0 {
			// VWAP = sum(price * quantity) / sum(quantity)
			vwap = acc.sumPriceTimesQty / acc.volume
		}
		result = append(result, OHLCV{
			StationID:  acc.stationID,
			ItemID:     acc.itemID,
			Side:       acc.side,
			BucketUTC:  bucketUTC,
			OpenPrice:  acc.open,
			HighPrice:  acc.high,
			LowPrice:   acc.low,
			ClosePrice: acc.close,
			Volume:     acc.volume,
			TradeCount: acc.tradeCount,
			VWAP:       vwap,
		})
	}
	return result
}

// upsertOHLCV inserts or updates an OHLCV record. The conflict key is
// (station_id, item_id, side, bucket_utc), so a second capture within the same
// UTC hour OVERWRITES the existing bucket row rather than merging into it:
// open/high/low/close/volume/trade_count/vwap are replaced wholesale with values
// computed from the latest snapshot. To grow the time series by one point, a
// capture must land in a new UTC hour (see WriteSnapshot).
func (c *Collector) upsertOHLCV(tx *sql.Tx, ohlcv OHLCV) error {
	_, err := tx.Exec(`
		INSERT INTO market_ohlcv (station_id, item_id, side, bucket_utc, open_price, high_price, low_price, close_price, volume, trade_count, vwap)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(station_id, item_id, side, bucket_utc) DO UPDATE SET
			open_price = excluded.open_price,
			high_price = excluded.high_price,
			low_price = excluded.low_price,
			close_price = excluded.close_price,
			volume = excluded.volume,
			trade_count = excluded.trade_count,
			vwap = excluded.vwap
	`, ohlcv.StationID, ohlcv.ItemID, ohlcv.Side, ohlcv.BucketUTC,
		ohlcv.OpenPrice, ohlcv.HighPrice, ohlcv.LowPrice, ohlcv.ClosePrice,
		ohlcv.Volume, ohlcv.TradeCount, ohlcv.VWAP)
	return err
}

// WriteSnapshot persists a market snapshot atomically: it upserts the station and
// items, stores every order in market_orders (replacing any book already recorded
// for this station at this same captured_at — see the DELETE below), and upserts
// the hourly OHLCV bucket for each (station, item, side).
//
// OHLCV is computed only from this snapshot's orders and keyed by the truncated
// UTC hour. A capture that lands in the SAME hour as a previous one therefore
// overwrites that hour's OHLCV row instead of extending it — it does not merge
// volumes or widen the high/low range. Distinct OHLCV data points come from
// captures in distinct UTC hours, so the collection cadence that builds trend
// depth is "at most one per hour" (the hourly schedule is the intended driver).
func (c *Collector) WriteSnapshot(ctx context.Context, snapshot MarketSnapshot) error {
	now := time.Now().UTC().Format(time.RFC3339)
	// last_updated_utc tracks the capture time (snapshot.CapturedAt), not the
	// write moment: it must equal the station's MAX(market_orders.captured_at)
	// so the demand liveness gate can read it as the station's latest-capture
	// instant without a full-table scan (see LoadCurrentBuyOrders). In live
	// capture these coincide, but backfills/replays would diverge under now().
	captured := snapshot.CapturedAt.UTC().Format(time.RFC3339)
	bucketUTC := snapshot.CapturedAt.UTC().Truncate(time.Hour).Format(time.RFC3339)

	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		// Upsert station
		if err := c.upsertStation(tx, Station{
			StationID:      snapshot.StationID,
			StationName:    snapshot.StationName,
			SystemID:       snapshot.SystemID,
			SystemName:     snapshot.SystemName,
			FirstSeenUTC:   now,
			LastUpdatedUTC: captured,
		}); err != nil {
			return fmt.Errorf("upsert station: %w", err)
		}

		// Group orders by item for upsert (first non-empty ItemName/Category wins)
		itemMap := make(map[string]Item)
		for _, o := range snapshot.Orders {
			existing, ok := itemMap[o.ItemID]
			if !ok {
				itemMap[o.ItemID] = Item{
					ItemID:         o.ItemID,
					ItemName:       o.ItemName,
					Category:       o.Category,
					FirstSeenUTC:   now,
					LastUpdatedUTC: now,
				}
				continue
			}
			if existing.ItemName == "" && o.ItemName != "" {
				existing.ItemName = o.ItemName
			}
			if existing.Category == "" && o.Category != "" {
				existing.Category = o.Category
			}
			itemMap[o.ItemID] = existing
		}

		// Upsert items
		for _, item := range itemMap {
			if err := c.upsertItem(tx, item); err != nil {
				return fmt.Errorf("upsert item %s: %w", item.ItemID, err)
			}
		}

		// Set bucket UTC on all orders
		ordersWithBucket := make([]Order, len(snapshot.Orders))
		for i, o := range snapshot.Orders {
			ordersWithBucket[i] = o
			ordersWithBucket[i].BucketUTC = bucketUTC
		}

		// Replace, do not append, this station's book for this capture instant.
		// Several marketbots dock at the same station and capture it
		// independently, and captured_at is RFC3339 (second resolution), so two
		// captures in the same second previously landed as two full copies of the
		// book under one timestamp. Live on 2026-08-12 Frontier Station carried
		// eight copies and Central Nexus two.
		//
		// That is not a cosmetic duplicate. GetItemStationPrices sums
		// `AskQty += qty` across every row at the station's latest capture, so the
		// copies became phantom depth: source_units, and through bookCap the
		// number of haulers a book is thought to supply, scaled with the number of
		// bots that happened to look at once. Deleting first makes a snapshot
		// write idempotent per (station, captured_at) — a re-capture in the same
		// second replaces the book rather than doubling it.
		//
		// Scoped to this station AND this instant on purpose: a later capture of
		// the same station is a new observation the price history needs, and
		// another station captured in the same second is an unrelated book.
		// An order-less snapshot never replaces a book: it carries no observation
		// to put in its place, and a capture that arrived empty (a filtered
		// view_market, a dropped payload) must not erase a good book.
		if len(ordersWithBucket) > 0 {
			if _, err := tx.Exec(
				`DELETE FROM market_orders WHERE station_id = ? AND captured_at = ?`,
				snapshot.StationID, captured); err != nil {
				return fmt.Errorf("clear prior rows for this capture: %w", err)
			}
		}

		// Insert orders. Rows that tie on price and quantity within ONE book are
		// left alone: those are distinct real orders, and collapsing them would
		// under-report depth.
		if err := c.insertOrders(tx, ordersWithBucket); err != nil {
			return fmt.Errorf("insert orders: %w", err)
		}

		// Compute and upsert OHLCV (hourly aggregates from this snapshot's orders)
		for _, ohlcv := range computeOHLCV(ordersWithBucket, bucketUTC) {
			if err := c.upsertOHLCV(tx, ohlcv); err != nil {
				return fmt.Errorf("upsert OHLCV: %w", err)
			}
		}

		return nil
	})
}
