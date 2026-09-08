// kb-migrate opens a knowledge base through the real migration runner and
// reports the ledger before and after. It exists so a schema migration can
// be applied to the live database by ONE process, deliberately, before any
// fleet worker meets it cold at startup (a failed startup migration burns a
// worker's restart budget and parks it).
//
//	bin/kb-migrate -db data/spacemolt-knowledge.db
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func main() {
	dbPath := flag.String("db", "data/spacemolt-knowledge.db", "path to the knowledge-base sqlite DB")
	flag.Parse()

	before, err := ledger(*dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read ledger:", err)
		os.Exit(1)
	}
	fmt.Printf("before: %s\n", describe(before))

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *dbPath, WAL: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
	_ = kb.Close()

	after, err := ledger(*dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read ledger:", err)
		os.Exit(1)
	}
	fmt.Printf("after:  %s\n", describe(after))
	applied := after[len(before):]
	if len(applied) == 0 {
		fmt.Println("nothing to apply: schema already current")
		return
	}
	fmt.Printf("applied: %s\n", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(applied)), ","), "[]"))
}

// ledger returns the applied migration versions in order. A database with
// no ledger table reports none (it is fresh).
func ledger(path string) ([]int, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func describe(versions []int) string {
	if len(versions) == 0 {
		return "no ledger (fresh database)"
	}
	return fmt.Sprintf("%d versions, latest %d", len(versions), versions[len(versions)-1])
}
