package market

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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
		DBPath:       filepath.Join(os.Getenv("HOME"), "spacemolt", "spacemolt", "data", "market.db"),
		WAL:          true,
		MaxOpenConns: 25,
		MaxIdleConns: 5,
		BusyTimeout:  5 * time.Second,
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

	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", int(cfg.BusyTimeout.Milliseconds()))); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	if cfg.WAL {
		if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("enable WAL: %w", err)
		}
	}

	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Collector{db: db}, nil
}

// Close closes the database connection.
func (c *Collector) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// maxRetryAttempts is the number of retries for SQLITE_BUSY.
const maxRetryAttempts = 5

// baseRetryDelay is the initial retry delay.
const baseRetryDelay = 50 * time.Millisecond

// writeRetry executes a write operation with exponential backoff retry on
// SQLITE_BUSY / database-locked errors.
func (c *Collector) writeRetry(ctx context.Context, fn func(tx *sql.Tx) error) error {
	var lastErr error
	for attempt := range maxRetryAttempts {
		if attempt > 0 {
			delay := baseRetryDelay * time.Duration(1<<(attempt-1))
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

// WriteSnapshot persists a market snapshot atomically.
func (c *Collector) WriteSnapshot(ctx context.Context, snapshot MarketSnapshot) error {
	now := time.Now().UTC().Format(time.RFC3339)
	bucketUTC := snapshot.CapturedAt.UTC().Truncate(time.Hour).Format(time.RFC3339)

	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		// Upsert station
		if err := c.upsertStation(tx, Station{
			StationID:      snapshot.StationID,
			StationName:    snapshot.StationName,
			SystemID:       snapshot.SystemID,
			SystemName:     snapshot.SystemName,
			FirstSeenUTC:   now,
			LastUpdatedUTC: now,
		}); err != nil {
			return fmt.Errorf("upsert station: %w", err)
		}

		// Group orders by item for upsert (first non-empty ItemName wins)
		itemMap := make(map[string]Item)
		for _, o := range snapshot.Orders {
			existing, ok := itemMap[o.ItemID]
			if !ok {
				itemMap[o.ItemID] = Item{
					ItemID:         o.ItemID,
					ItemName:       o.ItemName,
					Category:       "",
					FirstSeenUTC:   now,
					LastUpdatedUTC: now,
				}
				continue
			}
			if existing.ItemName == "" && o.ItemName != "" {
				existing.ItemName = o.ItemName
				itemMap[o.ItemID] = existing
			}
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

		// Insert orders
		if err := c.insertOrders(tx, ordersWithBucket); err != nil {
			return fmt.Errorf("insert orders: %w", err)
		}

		return nil
	})
}
