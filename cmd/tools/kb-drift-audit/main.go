// Command kb-drift-audit compares the knowledge-base sqlite DB against the
// authoritative catalog/map JSON files in data/game-api/ and reports:
//
//  1. Stale rows — IDs present in the DB but absent from the source file.
//     These accumulate because most importers use INSERT OR IGNORE without
//     a corresponding DELETE, so records removed from the game persist
//     indefinitely (e.g. the phantom antares->node_alpha connection bug).
//
//  2. Un-imported rows — IDs present in the source file but not the DB.
//     Usually benign; means the catalog was updated but not re-imported.
//
//  3. Referential orphans — rows whose foreign-keyish id columns point at
//     ids that no longer exist in the parent table. Catches inconsistencies
//     that CASCADE would miss (tables without the FK declared, or rows
//     inserted via paths that bypassed the constraint).
//
// Read-only: makes no DB mutations.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

type catalogFile struct {
	Items []map[string]any `json:"items"`
}

type mapFile struct {
	Systems []struct {
		SystemID    string   `json:"system_id"`
		Name        string   `json:"name"`
		Connections []string `json:"connections"`
	} `json:"systems"`
}

func main() {
	dbPath := flag.String("db", "data/spacemolt-knowledge.db", "path to knowledge-base sqlite DB")
	dataDir := flag.String("data", "data/game-api/latest", "directory containing catalog_*.json + get_map.json")
	showSamples := flag.Int("samples", 10, "max example IDs to print per finding (0 = counts only)")
	flag.Parse()

	db, err := sql.Open("sqlite", *dbPath+"?mode=ro")
	if err != nil {
		die("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	resolved, err := filepath.EvalSymlinks(*dataDir)
	if err != nil {
		die("resolve data dir: %v", err)
	}
	fmt.Printf("DB:   %s\nData: %s\n\n", *dbPath, resolved)

	anyDrift := false
	anyDrift = auditCatalog(db, *dataDir, "items", "catalog_items.json", "id", *showSamples) || anyDrift
	anyDrift = auditCatalog(db, *dataDir, "skills", "catalog_skills.json", "id", *showSamples) || anyDrift
	anyDrift = auditCatalog(db, *dataDir, "ships", "catalog_ships.json", "id", *showSamples) || anyDrift
	anyDrift = auditCatalog(db, *dataDir, "recipes", "catalog_recipes.json", "id", *showSamples) || anyDrift
	anyDrift = auditMap(db, *dataDir, *showSamples) || anyDrift
	anyDrift = auditReferentialOrphans(db, *showSamples) || anyDrift

	fmt.Println()
	if anyDrift {
		fmt.Println("Drift found. See findings above.")
		os.Exit(1)
	}
	fmt.Println("Clean — no drift detected.")
}

func auditCatalog(db *sql.DB, dataDir, table, fileName, pkCol string, samples int) bool {
	fmt.Printf("== %s (vs %s) ==\n", table, fileName)

	path := filepath.Join(dataDir, fileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("  skip: %v\n\n", err)
		return false
	}
	var cat catalogFile
	if err := json.Unmarshal(raw, &cat); err != nil {
		fmt.Printf("  skip: parse: %v\n\n", err)
		return false
	}

	fileIDs := make(map[string]bool, len(cat.Items))
	for _, item := range cat.Items {
		if id, ok := item["id"].(string); ok && id != "" {
			fileIDs[id] = true
		}
	}

	dbIDs, err := selectStrings(db, fmt.Sprintf("SELECT %s FROM %s", pkCol, table))
	if err != nil {
		fmt.Printf("  skip: query db: %v\n\n", err)
		return false
	}

	stale := diff(dbIDs, fileIDs)
	missing := diffSet(fileIDs, dbIDs)

	fmt.Printf("  file rows: %d | db rows: %d\n", len(fileIDs), len(dbIDs))
	report("stale (in DB, not in file)", stale, samples)
	report("un-imported (in file, not in DB)", missing, samples)
	fmt.Println()
	return len(stale) > 0
}

func auditMap(db *sql.DB, dataDir string, samples int) bool {
	fmt.Println("== systems + connections (vs get_map.json) ==")

	path := filepath.Join(dataDir, "get_map.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("  skip: %v\n\n", err)
		return false
	}
	var m mapFile
	if err := json.Unmarshal(raw, &m); err != nil {
		fmt.Printf("  skip: parse: %v\n\n", err)
		return false
	}

	fileSystems := make(map[string]bool, len(m.Systems))
	fileEdges := make(map[string]bool) // canonical "from|to"
	for _, s := range m.Systems {
		fileSystems[s.SystemID] = true
		for _, c := range s.Connections {
			fileEdges[s.SystemID+"|"+c] = true
		}
	}

	dbSystems, err := selectStrings(db, "SELECT id FROM systems")
	if err != nil {
		fmt.Printf("  skip: query systems: %v\n\n", err)
		return false
	}

	dbEdges, err := selectStringPairs(db, "SELECT from_system, to_system FROM connections")
	if err != nil {
		fmt.Printf("  skip: query connections: %v\n\n", err)
		return false
	}

	staleSystems := diff(dbSystems, fileSystems)
	missingSystems := diffSet(fileSystems, dbSystems)

	staleEdges := diff(dbEdges, fileEdges)
	missingEdges := diffSet(fileEdges, dbEdges)

	fmt.Printf("  systems:     file=%d db=%d\n", len(fileSystems), len(dbSystems))
	fmt.Printf("  connections: file=%d db=%d\n", len(fileEdges), len(dbEdges))
	report("stale systems (in DB, not in map)", staleSystems, samples)
	report("un-imported systems (in map, not in DB)", missingSystems, samples)
	report("stale connections (in DB, not in map)", staleEdges, samples)
	report("un-imported connections (in map, not in DB)", missingEdges, samples)
	fmt.Println()
	return len(staleSystems) > 0 || len(staleEdges) > 0
}

// auditReferentialOrphans checks for rows whose foreign-keyish columns point
// at non-existent parent rows. Some of these have ON DELETE CASCADE declared,
// but drift can still arise from bypass-paths or pre-existing bad data.
func auditReferentialOrphans(db *sql.DB, samples int) bool {
	fmt.Println("== referential orphans ==")

	type check struct {
		label string
		sql   string
	}
	checks := []check{
		{"recipe_inputs.item_id -> items.id",
			`SELECT DISTINCT item_id FROM recipe_inputs
			 WHERE item_id NOT IN (SELECT id FROM items)`},
		{"recipe_outputs.item_id -> items.id",
			`SELECT DISTINCT item_id FROM recipe_outputs
			 WHERE item_id NOT IN (SELECT id FROM items)`},
		{"recipe_inputs.recipe_id -> recipes.id",
			`SELECT DISTINCT recipe_id FROM recipe_inputs
			 WHERE recipe_id NOT IN (SELECT id FROM recipes)`},
		{"recipe_outputs.recipe_id -> recipes.id",
			`SELECT DISTINCT recipe_id FROM recipe_outputs
			 WHERE recipe_id NOT IN (SELECT id FROM recipes)`},
		{"ship_build_materials.item_id -> items.id",
			`SELECT DISTINCT item_id FROM ship_build_materials
			 WHERE item_id NOT IN (SELECT id FROM items)`},
		{"ship_build_materials.ship_class_id -> ships.id",
			`SELECT DISTINCT ship_class_id FROM ship_build_materials
			 WHERE ship_class_id NOT IN (SELECT id FROM ships)`},
		{"pois.system_id -> systems.id",
			`SELECT DISTINCT system_id FROM pois
			 WHERE system_id NOT IN (SELECT id FROM systems)`},
		{"connections.from_system -> systems.id",
			`SELECT DISTINCT from_system FROM connections
			 WHERE from_system NOT IN (SELECT id FROM systems)`},
		{"connections.to_system -> systems.id",
			`SELECT DISTINCT to_system FROM connections
			 WHERE to_system NOT IN (SELECT id FROM systems)`},
		{"poi_resources.poi_id -> pois.id",
			`SELECT DISTINCT poi_id FROM poi_resources
			 WHERE poi_id NOT IN (SELECT id FROM pois)`},
		{"poi_resources.resource_id -> items.id",
			`SELECT DISTINCT resource_id FROM poi_resources
			 WHERE resource_id NOT IN (SELECT id FROM items)`},
		{"player_skills.skill_id -> skills.id",
			`SELECT DISTINCT skill_id FROM player_skills
			 WHERE skill_id NOT IN (SELECT id FROM skills)`},
	}

	any := false
	for _, c := range checks {
		orphans, err := selectStrings(db, c.sql)
		if err != nil {
			fmt.Printf("  %s: skip (%v)\n", c.label, err)
			continue
		}
		if len(orphans) == 0 {
			fmt.Printf("  %s: ok\n", c.label)
			continue
		}
		any = true
		fmt.Printf("  %s: %d orphan(s)\n", c.label, len(orphans))
		if samples > 0 {
			sort.Strings(orphans)
			limit := min(samples, len(orphans))
			for _, id := range orphans[:limit] {
				fmt.Printf("    - %s\n", id)
			}
			if len(orphans) > limit {
				fmt.Printf("    ... and %d more\n", len(orphans)-limit)
			}
		}
	}
	fmt.Println()
	return any
}

func selectStrings(db *sql.DB, q string) ([]string, error) {
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

func selectStringPairs(db *sql.DB, q string) ([]string, error) {
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		out = append(out, a+"|"+b)
	}
	return out, rows.Err()
}

// diff returns elements of haystack that aren't keys in set.
func diff(haystack []string, set map[string]bool) []string {
	var out []string
	for _, s := range haystack {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}

// diffSet returns keys of set that aren't in list (via constructed map).
func diffSet(set map[string]bool, list []string) []string {
	listSet := make(map[string]bool, len(list))
	for _, s := range list {
		listSet[s] = true
	}
	var out []string
	for k := range set {
		if !listSet[k] {
			out = append(out, k)
		}
	}
	return out
}

func report(label string, items []string, samples int) {
	if len(items) == 0 {
		fmt.Printf("  %s: 0\n", label)
		return
	}
	fmt.Printf("  %s: %d\n", label, len(items))
	if samples <= 0 {
		return
	}
	sort.Strings(items)
	limit := min(samples, len(items))
	for _, id := range items[:limit] {
		fmt.Printf("    - %s\n", id)
	}
	if len(items) > limit {
		fmt.Printf("    ... and %d more\n", len(items)-limit)
	}
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "error: "+f+"\n", a...)
	os.Exit(2)
}
