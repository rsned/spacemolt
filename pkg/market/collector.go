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
	for attempt := 0; attempt < maxRetryAttempts; attempt++ {
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
