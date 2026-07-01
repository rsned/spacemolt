// Command purge-preepoch removes dead-epoch market captures from the knowledge
// database.
//
// The game server periodically does a server-wide market reboot that wipes and
// regenerates every station's order book. The knowledge store keeps only the
// latest snapshot per station (DELETE+INSERT on capture), so a station that has
// not been re-scanned since the last reboot still holds pre-reboot rows. Those
// prices are not merely stale — they describe a market that no longer exists,
// and they leak into the `demand` tool and arbitrage as phantom opportunities.
//
// This tool deletes market_buy_orders / market_sell_orders rows whose
// captured_utc predates a cutoff (the reboot boundary). Stations re-scanned
// after the reboot are untouched; a purged station simply reads as "no data"
// until it is captured again, which is correct.
//
// Dry-run by default. Pass --apply to actually delete.
//
// Usage:
//
//	purge-preepoch --db data/spacemolt-knowledge.db --cutoff 2026-06-24T00:00:00Z
//	purge-preepoch --db data/spacemolt-knowledge.db --cutoff 2026-06-24T00:00:00Z --apply
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// preEpochTables are the knowledge-store tables carrying dated market captures.
var preEpochTables = []string{"market_buy_orders", "market_sell_orders"}

func main() {
	dbPath := flag.String("db", "data/spacemolt-knowledge.db", "path to the knowledge SQLite database")
	cutoff := flag.String("cutoff", "", "RFC3339 UTC cutoff; rows with captured_utc before this are dead-epoch (required)")
	apply := flag.Bool("apply", false, "actually delete; without this the tool only reports what it would remove")
	flag.Parse()

	if *cutoff == "" {
		log.Fatal("--cutoff is required (e.g. --cutoff 2026-06-24T00:00:00Z, the market-reboot boundary)")
	}
	if _, err := time.Parse(time.RFC3339, *cutoff); err != nil {
		log.Fatalf("invalid --cutoff %q: %v", *cutoff, err)
	}

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatalf("open %s: %v", *dbPath, err)
	}
	defer func() { _ = db.Close() }()

	mode := "DRY-RUN"
	if *apply {
		mode = "APPLY"
	}
	fmt.Printf("purge-preepoch [%s] db=%s cutoff=%s\n", mode, *dbPath, *cutoff)

	var total int64
	for _, table := range preEpochTables {
		// Report the affected stations before touching anything, so the run is
		// never a silent bulk delete.
		rows, err := db.Query(
			fmt.Sprintf("SELECT station_id, COUNT(*), MAX(captured_utc) FROM %s WHERE captured_utc < ? GROUP BY station_id ORDER BY MAX(captured_utc)", table),
			*cutoff,
		)
		if err != nil {
			log.Fatalf("survey %s: %v", table, err)
		}
		var tableRows int64
		for rows.Next() {
			var station, last string
			var n int64
			if err := rows.Scan(&station, &n, &last); err != nil {
				_ = rows.Close()
				log.Fatalf("scan %s: %v", table, err)
			}
			fmt.Printf("  %-20s %-18s %d rows (last %s)\n", station, table, n, last)
			tableRows += n
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			log.Fatalf("iterate %s: %v", table, err)
		}
		_ = rows.Close()

		if *apply && tableRows > 0 {
			res, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE captured_utc < ?", table), *cutoff)
			if err != nil {
				log.Fatalf("delete %s: %v", table, err)
			}
			deleted, _ := res.RowsAffected()
			fmt.Printf("  deleted %d rows from %s\n", deleted, table)
		}
		total += tableRows
	}

	if total == 0 {
		fmt.Println("no dead-epoch rows found; nothing to purge")
		return
	}
	if *apply {
		fmt.Printf("done — purged %d dead-epoch rows\n", total)
	} else {
		fmt.Printf("would purge %d dead-epoch rows; re-run with --apply to delete\n", total)
	}
}
