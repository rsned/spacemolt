#!/usr/bin/env bash
# Regenerates scripts/sql/initialize_database.sql from the current Go migration
# runner state. Run this any time you add, modify, or remove a migration in
# pkg/knowledge/sqlite_migrations.go so the reference SQL file stays in sync.
#
# Usage: ./scripts/sql/regenerate_initialize_database.sh
#
# Requires: sqlite3, go toolchain, and a working module at the repo root.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

DB="$TMPDIR/fresh.db"
RUNNER="$TMPDIR/runner.go"
OUT="scripts/sql/initialize_database.sql"

cat > "$RUNNER" <<'EOF'
package main

import (
	"fmt"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: runner <db-path>")
		os.Exit(1)
	}
	cfg := knowledge.DefaultConfig()
	cfg.DBPath = os.Args[1]
	cfg.WAL = false
	kb, err := knowledge.NewSQLiteKB(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	_ = kb.Close()
}
EOF

go run "$RUNNER" "$DB"

MAXVER=$(sqlite3 "$DB" "SELECT MAX(version) FROM schema_migrations")
TODAY=$(date -u +%Y-%m-%d)

# Header
cat > "$OUT" <<HEADER
-- SpaceMolt Knowledge Base Database Schema
-- SQLite 3 Compatible
--
-- This file is AUTO-GENERATED from the Go migration runner in
-- pkg/knowledge/sqlite_migrations.go. Do not edit by hand — instead add or
-- modify a migration in sqlite_migrations.go and re-run:
--
--   ./scripts/sql/regenerate_initialize_database.sh
--
-- Use this to initialize a fresh database outside the application:
--
--   sqlite3 spacemolt-knowledge.db < scripts/sql/initialize_database.sql
--
-- Schema Version: ${MAXVER}
-- Last Regenerated: ${TODAY}

HEADER

# Tables, sorted by name
echo "-- ============================================================================" >> "$OUT"
echo "-- TABLES" >> "$OUT"
echo "-- ============================================================================" >> "$OUT"
echo "" >> "$OUT"
sqlite3 "$DB" <<SQL >> "$OUT"
SELECT sql || ';' || char(10) || char(10)
FROM sqlite_master
WHERE type = 'table'
  AND name NOT LIKE 'sqlite_%'
ORDER BY name;
SQL

# Indexes, sorted by name (exclude auto-indexes)
echo "-- ============================================================================" >> "$OUT"
echo "-- INDEXES" >> "$OUT"
echo "-- ============================================================================" >> "$OUT"
echo "" >> "$OUT"
sqlite3 "$DB" <<SQL >> "$OUT"
SELECT sql || ';' || char(10)
FROM sqlite_master
WHERE type = 'index'
  AND sql IS NOT NULL
ORDER BY name;
SQL

# Migration seed rows
echo "" >> "$OUT"
echo "-- ============================================================================" >> "$OUT"
echo "-- MIGRATION VERSION RECORDS" >> "$OUT"
echo "-- ============================================================================" >> "$OUT"
echo "" >> "$OUT"
sqlite3 "$DB" <<SQL >> "$OUT"
SELECT 'INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (' || version || ', datetime(''now''));'
FROM schema_migrations
ORDER BY version;
SQL

echo "" >> "$OUT"
echo "Wrote $OUT (schema version $MAXVER, $(wc -l < "$OUT") lines)"
