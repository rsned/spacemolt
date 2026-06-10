package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func main() {
	dataDir := flag.String("data-dir", "data", "path to the data directory")
	apply := flag.Bool("apply", false, "apply changes (default: dry-run)")
	flag.Parse()

	if err := validateRenames(explorerRenames); err != nil {
		log.Fatalf("invalid rename map: %v", err)
	}

	agentsDir := filepath.Join(*dataDir, "agents")
	reportsDir := filepath.Join(*dataDir, "reports")
	dbs := []string{
		filepath.Join(*dataDir, "spacemolt-knowledge.db"),
		filepath.Join(*dataDir, "daily-summary.db"),
	}
	m := renameMap(explorerRenames)

	mode := "DRY-RUN"
	if *apply {
		mode = "APPLY"
	}
	fmt.Printf("=== explorer renumber (%s) ===\n", mode)
	for _, r := range explorerRenames {
		fmt.Printf("  rename dir  %-12s -> %s\n", r.From, r.To)
	}
	for _, id := range placeholderSlots {
		fmt.Printf("  placeholder %s (outerrim)\n", id)
	}

	// 1. Directory renames (staged).
	if err := stageRenameDirs(agentsDir, explorerRenames, *apply); err != nil {
		log.Fatalf("dir rename: %v", err)
	}
	// 2. personality.json id rewrites at the new locations.
	if *apply {
		for _, r := range explorerRenames {
			path := filepath.Join(agentsDir, r.To, "personality.json")
			if err := rewritePersonalityID(path, r.To); err != nil {
				log.Fatalf("id rewrite %s: %v", r.To, err)
			}
		}
	}
	// 3. Placeholder stubs.
	for _, id := range placeholderSlots {
		if err := createPlaceholder(agentsDir, id, *apply); err != nil {
			log.Fatalf("placeholder %s: %v", id, err)
		}
	}
	// 4. Databases.
	for _, dbPath := range dbs {
		if err := processDB(dbPath, m, *apply); err != nil {
			log.Fatalf("db %s: %v", dbPath, err)
		}
	}
	// 5. Reports.
	n, err := rewriteReports(reportsDir, m, *apply)
	if err != nil {
		log.Fatalf("reports: %v", err)
	}
	fmt.Printf("  reports: %d file(s) %s\n", n, map[bool]string{true: "rewritten", false: "would change"}[*apply])

	// 6. Verification (only meaningful after apply).
	if *apply {
		var finalIDs []string
		for _, r := range explorerRenames {
			finalIDs = append(finalIDs, r.To)
		}
		finalIDs = append(finalIDs, placeholderSlots...)
		if probs := verifyAgents(agentsDir, finalIDs); len(probs) > 0 {
			log.Fatalf("verification failed:\n%v", probs)
		}
		for _, dbPath := range dbs {
			if probs := verifyDBProblemsAt(dbPath); len(probs) > 0 {
				log.Fatalf("db verification %s failed:\n%v", dbPath, probs)
			}
		}
		fmt.Println("  verification: OK")
	}
	fmt.Println("done.")
}

// processDB backs up, discovers agent columns, and staged-updates one database.
func processDB(dbPath string, m map[string]string, apply bool) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	cols, err := discoverAgentColumns(db)
	if err != nil {
		return err
	}
	fmt.Printf("  db %s: %d agent column(s)\n", filepath.Base(dbPath), len(cols))
	if !apply {
		return nil
	}
	bak, err := backupDB(dbPath)
	if err != nil {
		return err
	}
	fmt.Printf("    backup -> %s\n", filepath.Base(bak))
	var rs []Rename
	for from, to := range m {
		rs = append(rs, Rename{From: from, To: to})
	}
	return stagedUpdateDB(db, cols, rs, true)
}

func verifyDBProblemsAt(dbPath string) []string {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return []string{err.Error()}
	}
	defer func() { _ = db.Close() }()
	cols, err := discoverAgentColumns(db)
	if err != nil {
		return []string{err.Error()}
	}
	return verifyDBProblems(db, cols)
}
