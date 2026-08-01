// Package assets is the local ledger of what each agent is and owns: identity,
// skills, standings, carrier tier, and hulls, plus a derived eligibility layer.
//
// It lives in its own database (data/assets.db) for blast radius, not size.
// spacemolt-knowledge.db is 1.4GB and shared with the sibling spacemolt-kb
// repo, and market.db has already cost a full day of recovery from write
// contention. A separate file means an asset capture can never stall the fleet.
package assets

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// Config holds configuration for the assets database.
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
		DBPath:       filepath.Join("data", "assets.db"),
		WAL:          true,
		MaxOpenConns: 10,
		MaxIdleConns: 2,
		BusyTimeout:  5 * time.Second,
	}
}

// Store owns the assets database handle.
type Store struct {
	db *sql.DB
}

// sqliteDSN builds the connection string. Pragmas go through the DSN, NOT
// db.Exec: an Exec pragma lands on whichever pooled connection it happens to
// get, whereas DSN pragmas run on every connection the pool opens. With ~110
// worker processes sharing this database, every connection must inherit
// busy_timeout and WAL or contention surfaces as an immediate SQLITE_BUSY
// instead of a clean blocking wait. Same reasoning as pkg/market/collector.go.
func sqliteDSN(cfg Config) string {
	dsn := cfg.DBPath + "?_pragma=busy_timeout(" + strconv.Itoa(int(cfg.BusyTimeout.Milliseconds())) + ")"
	if cfg.WAL {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	// Take the write lock at BEGIN rather than upgrading read->write mid
	// transaction. Every write here is a whole-set replacement inside one
	// transaction, so IMMEDIATE is correct for all of them.
	dsn += "&_txlock=immediate"

	return dsn
}

// Open creates a store against the assets database, running migrations.
func Open(cfg Config) (*Store, error) {
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
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, fmt.Errorf("assets: create dir: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("assets: open database: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	if err := runMigrations(db); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("assets: run migrations: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database handle. A nil store closes cleanly so callers can
// treat "assets disabled" and "assets configured" the same way.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

// DB exposes the handle for read-only queries and tests.
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}

	return s.db
}

// rfc3339 renders a timestamp in the format every captured_at column uses.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
