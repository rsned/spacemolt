package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDiscoverAndStagedUpdate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "k.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mustExec(t, db, `CREATE TABLE experiences (agent_id TEXT, note TEXT)`)
	mustExec(t, db, `CREATE TABLE pois (detected_by TEXT, n INTEGER)`)
	mustExec(t, db, `INSERT INTO experiences VALUES ('explorer-1','a'),('explorer-3','b'),('miner-2','c')`)
	mustExec(t, db, `INSERT INTO pois VALUES ('explorer-3',1)`)

	cols, err := discoverAgentColumns(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("want 2 agent columns, got %d: %v", len(cols), cols)
	}

	// 2-cycle permutation: 1->3, 3->1.
	rs := []Rename{{"explorer-1", "explorer-3"}, {"explorer-3", "explorer-1"}}
	if err := stagedUpdateDB(db, cols, rs, true); err != nil {
		t.Fatal(err)
	}
	// experiences row formerly explorer-1 is now explorer-3 and vice versa; no loss.
	if got := countWhere(t, db, "experiences", "agent_id", "explorer-3"); got != 1 {
		t.Fatalf("explorer-3 count = %d, want 1", got)
	}
	if got := countWhere(t, db, "experiences", "agent_id", "explorer-1"); got != 1 {
		t.Fatalf("explorer-1 count = %d, want 1", got)
	}
	if got := countLike(t, db, "experiences", "agent_id", "%__staging"); got != 0 {
		t.Fatalf("staging values left: %d", got)
	}
	// pois explorer-3 -> explorer-1
	if got := countWhere(t, db, "pois", "detected_by", "explorer-1"); got != 1 {
		t.Fatalf("pois explorer-1 count = %d, want 1", got)
	}
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func countWhere(t *testing.T, db *sql.DB, table, col, val string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM "+table+" WHERE "+col+"=?", val).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countLike(t *testing.T, db *sql.DB, table, col, pat string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM "+table+" WHERE "+col+" LIKE ?", pat).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
