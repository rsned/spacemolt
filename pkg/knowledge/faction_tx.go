package knowledge

import (
	"context"
	"database/sql"
	"time"
)

// txer is the subset of *sql.Tx used by faction store helpers.
type txer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// inTx runs fn inside a transaction, rolling back on error.
func (kb *SQLiteKB) inTx(ctx context.Context, fn func(tx txer) error) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// utc formats t as an RFC3339 UTC string, using now if t is zero.
func utc(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(time.RFC3339)
}

// parseUTC parses an RFC3339 timestamp, returning the zero time on failure.
func parseUTC(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
