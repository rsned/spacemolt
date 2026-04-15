package knowledge

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestInitializeDatabaseSQLInSync guards against drift between the Go
// migration runner's output and the committed
// scripts/sql/initialize_database.sql. If the initial_schema.sql embedded
// file is edited without re-running scripts/sql/regenerate_initialize_database.sh,
// this test catches it.
//
// The check rebuilds the TABLES and INDEXES sections of
// initialize_database.sql in the same format the shell script produces,
// then byte-compares against what's on disk.
func TestInitializeDatabaseSQLInSync(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	tables := dumpSchemaObjects(t, kb, "table")
	indexes := dumpSchemaObjects(t, kb, "index")

	// Build the DDL section string in the exact format the regenerate
	// script produces. See scripts/sql/regenerate_initialize_database.sh
	// lines 74–98 for the authoritative format.
	var sb strings.Builder
	sb.WriteString("-- ============================================================================\n")
	sb.WriteString("-- TABLES\n")
	sb.WriteString("-- ============================================================================\n")
	sb.WriteString("\n")
	// Each row is <sql>;\n\n from the SELECT, plus \n from the sqlite3 CLI
	// row separator → total \n\n\n per row. Matched here so the output is
	// byte-identical with what scripts/sql/regenerate_initialize_database.sh
	// produces.
	for _, s := range tables {
		sb.WriteString(s)
		sb.WriteString(";\n\n\n")
	}
	sb.WriteString("-- ============================================================================\n")
	sb.WriteString("-- INDEXES\n")
	sb.WriteString("-- ============================================================================\n")
	sb.WriteString("\n")
	// Indexes: <sql>;\n from SELECT plus \n row separator → \n\n per row.
	for _, s := range indexes {
		sb.WriteString(s)
		sb.WriteString(";\n\n")
	}
	expected := sb.String()

	raw, err := os.ReadFile("../../scripts/sql/initialize_database.sql")
	if err != nil {
		t.Fatalf("read initialize_database.sql: %v", err)
	}
	actual := extractDDLSection(string(raw))

	if expected != actual {
		t.Fatalf("scripts/sql/initialize_database.sql is out of sync with the migration runner.\n"+
			"Run ./scripts/sql/regenerate_initialize_database.sh and commit the result.\n\n"+
			"First diff (expected vs actual):\n%s",
			firstDiffContext(expected, actual))
	}
}

// dumpSchemaObjects returns the CREATE statements from sqlite_master for
// objects of the given type (table or index), ordered by name, excluding
// sqlite_-internal tables and auto-created indexes.
func dumpSchemaObjects(t *testing.T, kb *SQLiteKB, objectType string) []string {
	t.Helper()
	var query string
	switch objectType {
	case "table":
		query = `SELECT sql FROM sqlite_master
		         WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		         ORDER BY name`
	case "index":
		query = `SELECT sql FROM sqlite_master
		         WHERE type = 'index' AND sql IS NOT NULL
		         ORDER BY name`
	default:
		t.Fatalf("unknown object type %q", objectType)
	}
	rows, err := kb.db.Query(query)
	if err != nil {
		t.Fatalf("query %s: %v", objectType, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan %s: %v", objectType, err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s: %v", objectType, err)
	}
	return out
}

// extractDDLSection returns the TABLES and INDEXES sections of an
// initialize_database.sql file: everything from the `-- TABLES` section
// header through the last index statement, stopping before the
// `-- MIGRATION VERSION RECORDS` header.
func extractDDLSection(content string) string {
	const tablesMarker = "-- ============================================================================\n-- TABLES\n"
	const migMarker = "\n-- ============================================================================\n-- MIGRATION VERSION RECORDS\n"
	startIdx := strings.Index(content, tablesMarker)
	if startIdx == -1 {
		return ""
	}
	endIdx := strings.Index(content, migMarker)
	if endIdx == -1 {
		return content[startIdx:]
	}
	return content[startIdx:endIdx]
}

// firstDiffContext returns a human-readable snippet showing where two
// strings first differ, with ~200 characters of context on either side.
func firstDiffContext(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			lo := i - 200
			if lo < 0 {
				lo = 0
			}
			hiA := i + 200
			if hiA > len(a) {
				hiA = len(a)
			}
			hiB := i + 200
			if hiB > len(b) {
				hiB = len(b)
			}
			return "--- expected ---\n" + a[lo:hiA] + "\n--- actual ---\n" + b[lo:hiB]
		}
	}
	if len(a) != len(b) {
		return fmt.Sprintf("length mismatch: expected=%d actual=%d", len(a), len(b))
	}
	return "(no difference found)"
}
