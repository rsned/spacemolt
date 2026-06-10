package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// verifyAgents confirms each listed agent dir has a personality.json whose id
// matches the dir and whose empire matches its slot's expected empire.
func verifyAgents(agentsDir string, ids []string) []string {
	var probs []string
	for _, id := range ids {
		path := filepath.Join(agentsDir, id, "personality.json")
		b, err := os.ReadFile(path)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		var doc struct {
			ID     string `json:"id"`
			Empire string `json:"empire"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			probs = append(probs, fmt.Sprintf("%s: bad json: %v", id, err))
			continue
		}
		if doc.ID != id {
			probs = append(probs, fmt.Sprintf("%s: id field is %q", id, doc.ID))
		}
		n, err := explorerNum(id)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		if want := expectedEmpire[n]; doc.Empire != want {
			probs = append(probs, fmt.Sprintf("%s: empire %q, want %q", id, doc.Empire, want))
		}
	}
	return probs
}

// verifyDBProblems checks discovered columns for leftover staging values or
// explorer ids whose number falls outside the valid 1..12 range.
func verifyDBProblems(db *sql.DB, cols []tableCol) []string {
	var probs []string
	for _, tc := range cols {
		var stg int
		q := fmt.Sprintf("SELECT count(*) FROM %q WHERE %q LIKE 'explorer-%%__staging'", tc.Table, tc.Col)
		if err := db.QueryRow(q).Scan(&stg); err == nil && stg > 0 {
			probs = append(probs, fmt.Sprintf("%s.%s: %d staging values left", tc.Table, tc.Col, stg))
		}
		ids, _ := queryStrings(db, fmt.Sprintf("SELECT DISTINCT %q FROM %q WHERE %q LIKE 'explorer-%%'", tc.Col, tc.Table, tc.Col))
		for _, id := range ids {
			if n, err := explorerNum(id); err != nil || n < 1 || n > 12 {
				probs = append(probs, fmt.Sprintf("%s.%s: bad id %q", tc.Table, tc.Col, id))
			}
		}
	}
	return probs
}
