// Command stations-refresh reconciles the knowledge base's bases table against
// the game's canonical station list.
//
//	go run ./cmd/tools/stations-refresh              # dry run, prints the plan
//	go run ./cmd/tools/stations-refresh --apply      # write it
//
// https://game.spacemolt.com/api/stations is public (no auth) and is the
// authority for three things the local table gets wrong: which stations exist,
// the base_id <-> poi_id alias pairing, and the services each one offers.
//
// It deliberately does NOT go through knowledge.RememberBase. That upsert
// assigns empire = excluded.empire unconditionally, and the canonical payload
// has no empire field at all (it carries faction_* instead), so routing a
// refresh through it would blank the empire on every row that has one. Only
// the fields the endpoint is actually authoritative for are written here;
// empire, story, public_access, defense_level, pirate_rep_required and the
// tick are left as the capture path recorded them.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

const defaultURL = "https://game.spacemolt.com/api/stations"

// station is the subset of the canonical payload this tool trusts.
type station struct {
	BaseID          string   `json:"base_id"`
	POIID           string   `json:"poi_id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Type            string   `json:"type"`
	SystemID        string   `json:"system_id"`
	SystemName      string   `json:"system_name"`
	Services        []string `json:"services"`
	Condition       string   `json:"condition"`
	ConditionText   string   `json:"condition_text"`
	SatisfactionPct int      `json:"satisfaction_pct"`
}

func main() {
	var (
		url    = flag.String("url", defaultURL, "canonical station endpoint")
		dbPath = flag.String("db", "data/spacemolt-knowledge.db", "knowledge database")
		apply  = flag.Bool("apply", false, "write changes (default: dry run)")
	)
	flag.Parse()

	if err := run(*url, *dbPath, *apply); err != nil {
		fmt.Fprintln(os.Stderr, "stations-refresh:", err)
		os.Exit(1)
	}
}

func run(url, dbPath string, apply bool) error {
	stations, err := fetch(url)
	if err != nil {
		return err
	}
	fmt.Printf("canonical: %d stations from %s\n", len(stations), url)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	known := make(map[string]bool)
	rows, err := db.Query(`SELECT id FROM bases`)
	if err != nil {
		return fmt.Errorf("read bases: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan base id: %w", err)
		}
		known[id] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate bases: %w", err)
	}
	_ = rows.Close()

	var inserts, updates []station
	for _, s := range stations {
		if s.BaseID == "" {
			continue
		}
		if known[s.BaseID] {
			updates = append(updates, s)
		} else {
			inserts = append(inserts, s)
		}
	}
	sort.Slice(inserts, func(i, j int) bool { return inserts[i].Name < inserts[j].Name })

	fmt.Printf("local bases: %d\n", len(known))
	fmt.Printf("to insert:   %d\n", len(inserts))
	for _, s := range inserts {
		fmt.Printf("   + %-26s %-8s %-18s services=%d\n", s.Name, s.Type, s.SystemName, len(s.Services))
	}
	fmt.Printf("to update:   %d (poi_id, name, description, condition, satisfaction, services)\n", len(updates))

	if !apply {
		fmt.Println("\ndry run; re-run with --apply to write")
		return nil
	}
	return write(db, inserts, updates)
}

func fetch(url string) ([]station, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	var payload struct {
		Stations []station `json:"stations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}
	if len(payload.Stations) == 0 {
		return nil, fmt.Errorf("decode %s: no stations in payload", url)
	}
	return payload.Stations, nil
}

func write(db *sql.DB, inserts, updates []station) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, s := range inserts {
		if _, err := tx.Exec(`
			INSERT INTO bases (id, poi_id, name, description, condition, condition_text, satisfaction_pct)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			s.BaseID, s.POIID, s.Name, s.Description, s.Condition, s.ConditionText, s.SatisfactionPct); err != nil {
			return fmt.Errorf("insert base %s: %w", s.Name, err)
		}
	}
	for _, s := range updates {
		if _, err := tx.Exec(`
			UPDATE bases SET poi_id = ?, name = ?, description = ?,
			                 condition = ?, condition_text = ?, satisfaction_pct = ?
			WHERE id = ?`,
			s.POIID, s.Name, s.Description, s.Condition, s.ConditionText, s.SatisfactionPct,
			s.BaseID); err != nil {
			return fmt.Errorf("update base %s: %w", s.Name, err)
		}
	}
	// Services are replaced wholesale: the endpoint states the full set, so a
	// service that vanished must disappear rather than linger.
	for _, s := range append(append([]station{}, inserts...), updates...) {
		if _, err := tx.Exec(`DELETE FROM base_services WHERE base_id = ?`, s.BaseID); err != nil {
			return fmt.Errorf("clear services %s: %w", s.Name, err)
		}
		for _, name := range s.Services {
			if _, err := tx.Exec(`
				INSERT INTO base_services (base_id, service_name, available) VALUES (?, ?, 1)`,
				s.BaseID, name); err != nil {
				return fmt.Errorf("insert service %s/%s: %w", s.Name, name, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	fmt.Printf("\napplied: %d inserted, %d updated\n", len(inserts), len(updates))
	return nil
}
