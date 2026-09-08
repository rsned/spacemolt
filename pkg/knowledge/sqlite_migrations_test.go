package knowledge

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A fresh database records the collapsed baseline and every migration above
// it, so its ledger reads the same as the live database's tail.
func TestRunMigrations_FreshLedgerIsBaselinePlusChain(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	rows, err := kb.db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	ms := migrations()
	if len(got) != len(ms) {
		t.Fatalf("ledger = %v, want one row per migration (%d)", got, len(ms))
	}
	for i, m := range ms {
		if got[i] != m.version {
			t.Fatalf("ledger = %v, want versions %d.. in order", got, collapseFloor)
		}
	}
	if got[0] != collapseFloor {
		t.Fatalf("first ledger row = %d, want the collapse floor %d", got[0], collapseFloor)
	}
}

// A database that came through the old chain but stopped short of the floor
// would silently miss the collapsed migrations; it must be refused, naming
// the version and the build that can still upgrade it.
func TestRunMigrations_RefusesPreFloorDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations VALUES (2, 'x'), (44, 'x');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = raw.Close()

	_, err = NewSQLiteKB(Config{DBPath: path, WAL: false, MaxOpenConns: 1, MaxIdleConns: 1})
	if err == nil {
		t.Fatal("NewSQLiteKB opened a version-44 database; want a refusal")
	}
	for _, want := range []string{"version 44", "floor 59", collapseUpgradeCommit} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
