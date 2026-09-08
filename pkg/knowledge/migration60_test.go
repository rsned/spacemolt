package knowledge

import (
	"database/sql"
	"strings"
	"testing"
)

// tableColumns returns one "name|type|notnull|default|pk" line per column of
// table, in declaration order: the column-level signature the 2026-09-08
// live-vs-fresh audit compared.
func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name||'|'||type||'|'||"notnull"||'|'||COALESCE(dflt_value,'')||'|'||pk FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, s)
	}
	return out
}

// TestMigration60_ReconcilesLiveShapes rebuilds the three tables exactly as
// the live database had them before migration 60 (old two-column faction
// keys, pre-collapse base_market defaults), seeds rows, re-runs the
// migration, and checks the result matches a fresh database column for
// column with the rows preserved.
func TestMigration60_ReconcilesLiveShapes(t *testing.T) {
	path := testDBPath(t)
	kb, err := NewSQLiteKB(Config{DBPath: path, WAL: false, MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	liveShapes := `
		DROP TABLE faction_orders;
		CREATE TABLE faction_orders (
			faction_id TEXT NOT NULL, base_id TEXT NOT NULL, order_id TEXT NOT NULL,
			side TEXT, item_id TEXT, item_name TEXT,
			price_each REAL NOT NULL DEFAULT 0, quantity REAL NOT NULL DEFAULT 0,
			captured_utc TEXT NOT NULL,
			PRIMARY KEY (faction_id, order_id)
		);
		CREATE INDEX faction_orders_faction ON faction_orders(faction_id);
		DROP TABLE faction_missions;
		CREATE TABLE faction_missions (
			faction_id TEXT NOT NULL, base_id TEXT NOT NULL, mission_id TEXT NOT NULL,
			title TEXT, type TEXT, description TEXT, giver_name TEXT,
			rewards_json TEXT, objectives_json TEXT, assigned_player_id TEXT,
			expiration_utc TEXT, captured_utc TEXT NOT NULL,
			PRIMARY KEY (faction_id, mission_id)
		);
		CREATE INDEX faction_missions_faction ON faction_missions(faction_id);
		DROP TABLE base_market;
		CREATE TABLE base_market (
			id TEXT PRIMARY KEY, base_id TEXT NOT NULL, item_id TEXT NOT NULL,
			price_each REAL NOT NULL, quantity INTEGER DEFAULT 0, is_npc BOOLEAN DEFAULT 0,
			last_updated_tick INTEGER DEFAULT 0,
			FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
		);
		CREATE INDEX idx_base_market_base_id ON base_market(base_id);
		CREATE INDEX idx_base_market_item_id ON base_market(item_id);
		INSERT INTO faction_orders (faction_id, base_id, order_id, side, captured_utc) VALUES ('f1','b1','o1','buy','t');
		INSERT INTO faction_missions (faction_id, base_id, mission_id, title, captured_utc) VALUES ('f1','b1','m1','Escort','t');
		INSERT INTO base_market (id, base_id, item_id, price_each) VALUES ('bm1','b1','iron_ore',12.5);
		DELETE FROM schema_migrations WHERE version >= 60;
	`
	if _, err := kb.db.Exec(liveShapes); err != nil {
		t.Fatalf("rebuild live shapes: %v", err)
	}
	if err := kb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen: migration 60 must run and reconcile.
	kb, err = NewSQLiteKB(Config{DBPath: path, WAL: false, MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("reopen (migration 60): %v", err)
	}
	defer func() { _ = kb.Close() }()
	fresh := newTestSQLiteKB(t)
	defer func() { _ = fresh.Close() }()

	for _, table := range []string{"faction_orders", "faction_missions", "base_market"} {
		got := strings.Join(tableColumns(t, kb.db, table), "\n")
		want := strings.Join(tableColumns(t, fresh.db, table), "\n")
		if got != want {
			t.Errorf("%s after migration 60 differs from fresh:\n got:\n%s\n want:\n%s", table, got, want)
		}
	}
	for table, want := range map[string]int{"faction_orders": 1, "faction_missions": 1, "base_market": 1} {
		var n int
		if err := kb.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != want {
			t.Errorf("%s rows after rebuild = %d, want %d (rows must survive the copy)", table, n, want)
		}
	}
	var qty, npc int
	if err := kb.db.QueryRow("SELECT quantity, is_npc FROM base_market WHERE id='bm1'").Scan(&qty, &npc); err != nil {
		t.Fatalf("read bm1: %v", err)
	}
	if qty != 0 || npc != 0 {
		t.Errorf("bm1 quantity/is_npc = %d/%d, want 0/0 (live defaults preserved for existing rows)", qty, npc)
	}
	var version int
	if err := kb.db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("version: %v", err)
	}
	if version != 60 {
		t.Errorf("ledger = %d, want 60", version)
	}
}
