// Command market-widen-arbitrage rebuilds market.db's arbitrage_opportunities
// table so its action_type CHECK admits consigned (withdraw_then_sell) routes.
//
// SQLite cannot alter a CHECK constraint, so this follows SQLite's documented
// table-rebuild procedure: create a copy with the widened CHECK, copy every
// row, drop the original, rename the copy, recreate the indexes — all in one
// IMMEDIATE transaction, so a failure anywhere leaves the original untouched.
//
// Run it with every writer of market.db stopped (arbitrage-scanner, the haul
// fleet, marketbots): the transaction holds the write lock for its duration.
// Without --yes it only reports what it would do.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	table    = "arbitrage_opportunities"
	oldCheck = "CHECK (action_type IN ('buy_then_sell', 'sell_then_buy'))"
	newCheck = "CHECK (action_type IN ('buy_then_sell', 'sell_then_buy', 'withdraw_then_sell'))"
)

func main() {
	dbPath := flag.String("db", "data/market.db", "market database to migrate")
	yes := flag.Bool("yes", false, "perform the rebuild (default: dry run)")
	flag.Parse()

	db, err := sql.Open("sqlite", *dbPath+"?_pragma=busy_timeout(60000)")
	if err != nil {
		log.Fatalf("open %s: %v", *dbPath, err)
	}
	defer db.Close() //nolint:errcheck

	res, err := widen(context.Background(), db, !*yes)
	if err != nil {
		log.Fatalf("widen: %v", err)
	}
	fmt.Println(res)
	if !*yes && !strings.HasPrefix(res, "already") {
		fmt.Fprintln(os.Stderr, "dry run: re-run with --yes to rebuild")
	}
}

// widen performs (or, with dryRun, describes) the rebuild and returns a
// one-line summary. It is idempotent: a table already admitting
// withdraw_then_sell is left alone.
func widen(ctx context.Context, db *sql.DB, dryRun bool) (string, error) {
	var ddl string
	if err := db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl); err != nil {
		return "", fmt.Errorf("read %s schema: %w", table, err)
	}
	if strings.Contains(ddl, "'withdraw_then_sell'") {
		return "already migrated: " + table + " admits withdraw_then_sell", nil
	}
	if n := strings.Count(ddl, oldCheck); n != 1 {
		return "", fmt.Errorf("expected exactly one %q in the %s schema, found %d — refusing to guess", oldCheck, table, n)
	}
	newDDL := strings.Replace(strings.Replace(ddl, oldCheck, newCheck, 1),
		"CREATE TABLE "+table, "CREATE TABLE "+table+"_new", 1)
	if !strings.Contains(newDDL, table+"_new") {
		return "", fmt.Errorf("could not rename the table in its own DDL: %s", ddl)
	}

	var rows int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&rows); err != nil {
		return "", fmt.Errorf("count rows: %w", err)
	}
	if dryRun {
		return fmt.Sprintf("would rebuild %s (%d rows) with %s", table, rows, newCheck), nil
	}

	idx, err := indexDDL(ctx, db)
	if err != nil {
		return "", err
	}
	cols, err := columns(ctx, db)
	if err != nil {
		return "", err
	}

	// A single connection: foreign_keys is per-connection and must be off
	// around the DROP, and BEGIN IMMEDIATE must share it with every statement.
	conn, err := db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close() //nolint:errcheck
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return "", fmt.Errorf("foreign_keys off: %w", err)
	}

	start := time.Now()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	colList := strings.Join(cols, ", ")
	steps := []string{
		newDDL,
		`INSERT INTO ` + table + `_new (` + colList + `) SELECT ` + colList + ` FROM ` + table,
	}
	for _, s := range steps {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			return "", fmt.Errorf("rebuild step %.60q: %w", s, err)
		}
	}
	var copied int64
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+`_new`).Scan(&copied); err != nil {
		return "", fmt.Errorf("count copy: %w", err)
	}
	if copied != rows {
		return "", fmt.Errorf("copied %d rows, original has %d — rolled back", copied, rows)
	}
	// DROP first so the original's sqlite_sequence row goes with it; the
	// RENAME then carries the copy's sequence row (max id) across.
	steps = []string{`DROP TABLE ` + table, `ALTER TABLE ` + table + `_new RENAME TO ` + table}
	steps = append(steps, idx...)
	for _, s := range steps {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			return "", fmt.Errorf("rebuild step %.60q: %w", s, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	committed = true
	return fmt.Sprintf("rebuilt %s: %d rows, %d indexes, %s", table, copied, len(idx), time.Since(start).Round(time.Millisecond)), nil
}

// indexDDL captures the table's explicit index definitions so they can be
// recreated after the DROP removes them.
func indexDDL(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND sql IS NOT NULL ORDER BY name`, table)
	if err != nil {
		return nil, fmt.Errorf("read indexes: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// columns lists the table's columns in order, for an explicit-column copy.
func columns(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, `"`+s+`"`)
	}
	return out, rows.Err()
}
