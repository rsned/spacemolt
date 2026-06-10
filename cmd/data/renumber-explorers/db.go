package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"
)

// tableCol identifies a text column that holds explorer agent ids.
type tableCol struct {
	Table string
	Col   string
}

// backupDB copies a sqlite file to a timestamped sibling before mutation.
func backupDB(path string) (string, error) {
	dst := fmt.Sprintf("%s.bak-renumber-%s", path, time.Now().Format("20060102-150405"))
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	return dst, out.Sync()
}

// discoverAgentColumns scans every table's columns for any value matching
// 'explorer-%', returning the (table,col) pairs that hold agent ids.
func discoverAgentColumns(db *sql.DB) ([]tableCol, error) {
	tables, err := queryStrings(db, `SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return nil, err
	}
	var cols []tableCol
	for _, t := range tables {
		rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%q)", t))
		if err != nil {
			return nil, err
		}
		var names []string
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt any
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				_ = rows.Close()
				return nil, err
			}
			names = append(names, name)
		}
		_ = rows.Close()
		for _, c := range names {
			var hit int
			q := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %q WHERE %q LIKE 'explorer-%%')", t, c)
			if err := db.QueryRow(q).Scan(&hit); err != nil {
				continue // non-text column; LIKE is harmless but ignore errors
			}
			if hit == 1 {
				cols = append(cols, tableCol{Table: t, Col: c})
			}
		}
	}
	return cols, nil
}

// stagedUpdateDB rewrites agent ids in every discovered column using a
// two-phase update so the permutation never collides on a UNIQUE/PK column.
func stagedUpdateDB(db *sql.DB, cols []tableCol, rs []Rename, apply bool) error {
	if !apply {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // rolled back unless Commit succeeds
	for _, tc := range cols {
		// Phase 1: from -> from||'__staging'
		for _, r := range rs {
			q := fmt.Sprintf("UPDATE %q SET %q=? WHERE %q=?", tc.Table, tc.Col, tc.Col)
			if _, err := tx.Exec(q, r.From+"__staging", r.From); err != nil {
				return fmt.Errorf("%s.%s phase1 %s: %w", tc.Table, tc.Col, r.From, err)
			}
		}
		// Phase 2: from||'__staging' -> to
		for _, r := range rs {
			q := fmt.Sprintf("UPDATE %q SET %q=? WHERE %q=?", tc.Table, tc.Col, tc.Col)
			if _, err := tx.Exec(q, r.To, r.From+"__staging"); err != nil {
				return fmt.Errorf("%s.%s phase2 %s: %w", tc.Table, tc.Col, r.From, err)
			}
		}
	}
	return tx.Commit()
}

func queryStrings(db *sql.DB, q string) ([]string, error) {
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
