// Command daily-summary connects to game agents, captures state snapshots,
// stores them in SQLite, and generates diff reports showing day-over-day changes.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	_ "modernc.org/sqlite"
)

// SkillSnap captures a skill's level for diffing.
type SkillSnap struct {
	Level int     `json:"level"`
	XP    float64 `json:"xp"`
}

// StorageEntry captures a single storage item.
type StorageEntry struct {
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
}

// AgentSnapshot holds the captured state for one agent at a point in time.
type AgentSnapshot struct {
	AgentID       string               `json:"agent_id"`
	Username      string               `json:"username"`
	Empire        string               `json:"empire"`
	Credits       float64              `json:"credits"`
	Location      string               `json:"location"`
	Docked        bool                 `json:"docked"`
	Skills        map[string]SkillSnap `json:"skills"`
	Stats         game.PlayerStats     `json:"stats"`
	ShipClass     string               `json:"ship_class"`
	ShipName      string               `json:"ship_name"`
	CargoUsed     float64              `json:"cargo_used"`
	CargoCapacity float64              `json:"cargo_capacity"`
	ModuleCount   int                  `json:"module_count"`
	TotalShips    int                  `json:"total_ships"`
	StorageItems  []StorageEntry       `json:"storage_items"`
	StorageTotal  float64              `json:"storage_total"`
	FactionID     string               `json:"faction_id"`
	FactionRank   string               `json:"faction_rank"`
	Experience    int64                `json:"experience"`
	Error         string               `json:"error,omitempty"`
	CapturedAt    time.Time            `json:"captured_at"`
}

// AgentDiff holds the computed differences between two snapshots.
type AgentDiff struct {
	AgentID      string
	Username     string
	Empire       string
	CreditsDelta float64
	SkillChanges []string // e.g. "Mining: 4 -> 5"
	StatChanges  []string // e.g. "OreMined: +150.0"
	ShipChanged  string   // e.g. "Prospector -> Drillship"
	StorageDelta float64
	ShipsDelta   int
	LocationFrom string
	LocationTo   string
	HasChanges   bool
	Current      *AgentSnapshot
}

func main() {
	dbPath := flag.String("db", "data/daily-summary.db", "SQLite database path")
	outputPath := flag.String("output", "", "Report output base path (default: data/reports/daily-summary-YYYY-MM-DD)")
	agents := flag.String("agents", "", "Comma-separated agent filter (default: all from data/agents/)")
	delay := flag.Int("delay", 3, "Delay in seconds between agent connections")
	reportOnly := flag.Bool("report-only", false, "Skip data collection, regenerate report from latest DB data")
	flag.Parse()

	logger := log.New(os.Stdout, "[daily-summary] ", log.LstdFlags)
	today := time.Now().Format("2006-01-02")

	if *outputPath == "" {
		*outputPath = filepath.Join("data", "reports", "daily-summary-"+today)
	}

	// Resolve agent list
	agentList, err := resolveAgents(*agents)
	if err != nil {
		logger.Fatalf("Failed to resolve agents: %v", err)
	}
	logger.Printf("Agents: %d total", len(agentList))

	// Open database
	db, err := openDB(*dbPath)
	if err != nil {
		logger.Fatalf("Failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Collect data unless report-only mode
	if !*reportOnly {
		collectSnapshots(db, agentList, *delay, today, logger)
	}

	// Load today's snapshots and previous snapshots for diff
	todaySnaps, err := loadSnapshots(db, today)
	if err != nil {
		logger.Fatalf("Failed to load today's snapshots: %v", err)
	}
	if len(todaySnaps) == 0 {
		logger.Fatalf("No snapshots found for %s", today)
	}

	prevDate, err := findPreviousDate(db, today)
	if err != nil {
		logger.Fatalf("Failed to find previous date: %v", err)
	}

	var prevSnaps map[string]*AgentSnapshot
	if prevDate != "" {
		prevSnaps, err = loadSnapshots(db, prevDate)
		if err != nil {
			logger.Fatalf("Failed to load previous snapshots: %v", err)
		}
		logger.Printf("Comparing %s vs %s (%d previous snapshots)", today, prevDate, len(prevSnaps))
	} else {
		logger.Printf("No previous data found, generating baseline report for %s", today)
		prevSnaps = make(map[string]*AgentSnapshot)
	}

	// Compute diffs
	diffs := computeDiffs(todaySnaps, prevSnaps)

	// Generate reports
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		logger.Fatalf("Failed to create report directory: %v", err)
	}

	if err := writeMarkdownReport(*outputPath+".md", today, prevDate, diffs); err != nil {
		logger.Fatalf("Failed to write markdown report: %v", err)
	}
	logger.Printf("Markdown report: %s.md", *outputPath)

	if err := writeHTMLReport(*outputPath+".html", today, prevDate, diffs); err != nil {
		logger.Fatalf("Failed to write HTML report: %v", err)
	}
	logger.Printf("HTML report: %s.html", *outputPath)
}

// resolveAgents returns the list of agent IDs to process.
func resolveAgents(filter string) ([]string, error) {
	if filter != "" {
		return strings.Split(filter, ","), nil
	}
	entries, err := os.ReadDir("data/agents")
	if err != nil {
		return nil, fmt.Errorf("reading data/agents: %w", err)
	}
	var agents []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Only include directories that have credentials.json
		if _, err := os.Stat(filepath.Join("data", "agents", e.Name(), "credentials.json")); err == nil {
			agents = append(agents, e.Name())
		}
	}
	slices.Sort(agents)
	return agents, nil
}

// openDB opens the SQLite database and creates the schema.
func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	schema := `
		CREATE TABLE IF NOT EXISTS snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id TEXT NOT NULL,
			captured_at TEXT NOT NULL,
			captured_date TEXT NOT NULL,
			state_json TEXT NOT NULL,
			UNIQUE(agent_id, captured_date)
		);
		CREATE INDEX IF NOT EXISTS idx_snapshots_agent_date ON snapshots(agent_id, captured_date DESC);
		CREATE INDEX IF NOT EXISTS idx_snapshots_date ON snapshots(captured_date DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	return db, nil
}

// collectSnapshots connects to each agent, captures state, and saves to DB.
func collectSnapshots(db *sql.DB, agentList []string, delaySec int, today string, logger *log.Logger) {
	for i, agentID := range agentList {
		if i > 0 {
			time.Sleep(time.Duration(delaySec) * time.Second)
		}
		logger.Printf("[%d/%d] Collecting %s...", i+1, len(agentList), agentID)
		snap := captureAgent(agentID, logger)
		if err := saveSnapshot(db, snap, today); err != nil {
			logger.Printf("  Failed to save snapshot: %v", err)
		}
	}
}

// captureAgent connects to a single agent and captures its state.
func captureAgent(agentID string, logger *log.Logger) *AgentSnapshot {
	snap := &AgentSnapshot{
		AgentID:    agentID,
		CapturedAt: time.Now(),
		Skills:     make(map[string]SkillSnap),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		snap.Error = fmt.Sprintf("init: %v", err)
		return snap
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			logger.Printf("  Warning: close error for %s: %v", agentID, cerr)
		}
	}()

	// Extract state from login response
	state := client.GetState()
	snap.Username = creds.Username
	snap.Empire = creds.Empire
	snap.Credits = state.Credits
	snap.Location = state.System.Name + " / " + state.CurrentPOI
	snap.Docked = state.Doc
	snap.ShipClass = state.Ship.ClassID
	snap.ShipName = state.Ship.Name
	snap.CargoUsed = state.Ship.CargoUsed
	snap.CargoCapacity = state.Ship.CargoCapacity
	snap.ModuleCount = len(state.Ship.Modules)
	snap.Stats = state.Player.Stats
	snap.FactionID = state.Player.FactionID
	snap.FactionRank = state.Player.FactionRank
	snap.Experience = state.Player.Experience

	for skillID, skill := range state.Player.Skills {
		xp := skill.XP
		// Prefer State.SkillXP which tracks current XP toward next level
		if stateXP, ok := state.SkillXP[skillID]; ok {
			xp = stateXP
		}
		snap.Skills[skillID] = SkillSnap{Level: skill.Level, XP: xp}
	}

	// Best-effort: list ships
	if err := client.ListShips(ctx); err == nil {
		time.Sleep(1 * time.Second)
		rawJSON := client.GetRawJSON("owned_ships")
		if rawJSON == nil {
			rawJSON = client.GetRawJSON("ships")
		}
		if rawJSON != nil {
			var shipsResp struct {
				Ships []json.RawMessage `json:"ships"`
			}
			if json.Unmarshal(rawJSON, &shipsResp) == nil {
				snap.TotalShips = len(shipsResp.Ships)
			}
		}
	}

	// Best-effort: storage (only when docked)
	if state.Doc {
		if err := client.ViewStorage(ctx); err == nil {
			time.Sleep(1 * time.Second)
			if rawJSON := client.GetRawJSON("storage"); rawJSON != nil {
				var storageResp struct {
					Items []struct {
						ItemID   string  `json:"item_id"`
						Quantity float64 `json:"quantity"`
					} `json:"items"`
				}
				if json.Unmarshal(rawJSON, &storageResp) == nil {
					for _, item := range storageResp.Items {
						snap.StorageItems = append(snap.StorageItems, StorageEntry{
							ItemID:   item.ItemID,
							Quantity: item.Quantity,
						})
						snap.StorageTotal += item.Quantity
					}
				}
			}
		}
	}

	return snap
}

// saveSnapshot persists a snapshot to the database.
func saveSnapshot(db *sql.DB, snap *AgentSnapshot, today string) error {
	jsonData, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshaling snapshot: %w", err)
	}
	_, err = db.Exec(
		`INSERT OR REPLACE INTO snapshots (agent_id, captured_at, captured_date, state_json)
		 VALUES (?, ?, ?, ?)`,
		snap.AgentID, snap.CapturedAt.Format(time.RFC3339), today, string(jsonData),
	)
	return err
}

// loadSnapshots loads all snapshots for a given date.
func loadSnapshots(db *sql.DB, date string) (map[string]*AgentSnapshot, error) {
	rows, err := db.Query(
		`SELECT agent_id, state_json FROM snapshots WHERE captured_date = ?`, date,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	snaps := make(map[string]*AgentSnapshot)
	for rows.Next() {
		var agentID, jsonStr string
		if err := rows.Scan(&agentID, &jsonStr); err != nil {
			return nil, err
		}
		var snap AgentSnapshot
		if err := json.Unmarshal([]byte(jsonStr), &snap); err != nil {
			return nil, fmt.Errorf("unmarshaling snapshot for %s: %w", agentID, err)
		}
		snaps[agentID] = &snap
	}
	return snaps, rows.Err()
}

// findPreviousDate finds the most recent snapshot date before the given date.
func findPreviousDate(db *sql.DB, currentDate string) (string, error) {
	var prevDate string
	err := db.QueryRow(
		`SELECT DISTINCT captured_date FROM snapshots
		 WHERE captured_date < ? ORDER BY captured_date DESC LIMIT 1`, currentDate,
	).Scan(&prevDate)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return prevDate, err
}

// computeDiffs computes the differences between today's and previous snapshots.
func computeDiffs(today, prev map[string]*AgentSnapshot) []AgentDiff {
	var diffs []AgentDiff
	for _, snap := range today {
		diff := AgentDiff{
			AgentID:  snap.AgentID,
			Username: snap.Username,
			Empire:   snap.Empire,
			Current:  snap,
		}

		old, hasPrev := prev[snap.AgentID]
		if !hasPrev || snap.Error != "" {
			diff.HasChanges = snap.Error == ""
			diffs = append(diffs, diff)
			continue
		}

		// Credits
		diff.CreditsDelta = snap.Credits - old.Credits
		if math.Abs(diff.CreditsDelta) >= 1 {
			diff.HasChanges = true
		}

		// Skills
		for skillID, cur := range snap.Skills {
			oldSkill, ok := old.Skills[skillID]
			if !ok {
				// New skill
				diff.SkillChanges = append(diff.SkillChanges, fmt.Sprintf("%s: new at level %d", skillID, cur.Level))
				diff.HasChanges = true
				continue
			}
			if cur.Level != oldSkill.Level {
				// Level changed - XP reset, don't show XP delta
				diff.SkillChanges = append(diff.SkillChanges, fmt.Sprintf("%s: level %d -> %d", skillID, oldSkill.Level, cur.Level))
				diff.HasChanges = true
			} else if xpDelta := int(cur.XP) - int(oldSkill.XP); xpDelta != 0 {
				// Same level, XP changed
				diff.SkillChanges = append(diff.SkillChanges, fmt.Sprintf("%s: +%d XP (level %d)", skillID, xpDelta, cur.Level))
				diff.HasChanges = true
			}
		}
		slices.Sort(diff.SkillChanges)

		// Stats
		diff.StatChanges = diffStats(old.Stats, snap.Stats)
		if len(diff.StatChanges) > 0 {
			diff.HasChanges = true
		}

		// Ship
		if snap.ShipClass != old.ShipClass {
			diff.ShipChanged = fmt.Sprintf("%s -> %s", old.ShipName, snap.ShipName)
			diff.HasChanges = true
		}

		// Storage
		diff.StorageDelta = snap.StorageTotal - old.StorageTotal
		if math.Abs(diff.StorageDelta) >= 1 {
			diff.HasChanges = true
		}

		// Ships count
		diff.ShipsDelta = snap.TotalShips - old.TotalShips
		if diff.ShipsDelta != 0 {
			diff.HasChanges = true
		}

		// Location
		if snap.Location != old.Location {
			diff.LocationFrom = old.Location
			diff.LocationTo = snap.Location
			diff.HasChanges = true
		}

		diffs = append(diffs, diff)
	}
	slices.SortFunc(diffs, func(a, b AgentDiff) int {
		return strings.Compare(a.AgentID, b.AgentID)
	})
	return diffs
}

// diffStats compares two PlayerStats and returns human-readable delta strings.
func diffStats(old, cur game.PlayerStats) []string {
	var changes []string
	check := func(name string, oldV, curV int) {
		if d := curV - oldV; d != 0 {
			changes = append(changes, fmt.Sprintf("%s: %+d", name, d))
		}
	}
	checkF := func(name string, oldV, curV float64) {
		if d := curV - oldV; math.Abs(d) >= 0.01 {
			changes = append(changes, fmt.Sprintf("%s: %+.1f", name, d))
		}
	}
	checkI64 := func(name string, oldV, curV int64) {
		if d := curV - oldV; d != 0 {
			changes = append(changes, fmt.Sprintf("%s: %+d", name, d))
		}
	}

	check("ShipsDestroyed", old.ShipsDestroyed, cur.ShipsDestroyed)
	check("TimesDestroyed", old.TimesDestroyed, cur.TimesDestroyed)
	checkF("OreMined", old.OreMined, cur.OreMined)
	checkF("CreditsEarned", old.CreditsEarned, cur.CreditsEarned)
	checkF("CreditsSpent", old.CreditsSpent, cur.CreditsSpent)
	check("TradesCompleted", old.TradesCompleted, cur.TradesCompleted)
	check("SystemsDiscovered", old.SystemsDiscovered, cur.SystemsDiscovered)
	check("ItemsCrafted", old.ItemsCrafted, cur.ItemsCrafted)
	check("MissionsCompleted", old.MissionsCompleted, cur.MissionsCompleted)
	check("BasesDestroyed", old.BasesDestroyed, cur.BasesDestroyed)
	checkI64("DistanceTraveled", old.DistanceTraveled, cur.DistanceTraveled)
	check("PiratesDestroyed", old.PiratesDestroyed, cur.PiratesDestroyed)
	check("ShipsLost", old.ShipsLost, cur.ShipsLost)
	checkI64("TimePlayed", old.TimePlayed, cur.TimePlayed)

	return changes
}

// formatCredits formats a credit value with commas and +/- sign for deltas.
func formatCredits(v float64) string {
	sign := ""
	if v > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.0f", sign, v)
}

// writeMarkdownReport generates a markdown report file.
func writeMarkdownReport(path, today, prevDate string, diffs []AgentDiff) error {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Daily Summary: %s\n\n", today))
	if prevDate != "" {
		b.WriteString(fmt.Sprintf("Compared to: %s\n\n", prevDate))
	} else {
		b.WriteString("Baseline report (no previous data)\n\n")
	}

	// Summary stats
	var totalCredits float64
	var totalSkills int
	var changedCount, errorCount int
	for _, d := range diffs {
		if d.Current.Error != "" {
			errorCount++
			continue
		}
		totalCredits += d.CreditsDelta
		totalSkills += len(d.SkillChanges)
		if d.HasChanges {
			changedCount++
		}
	}

	b.WriteString("## Fleet Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	fmt.Fprintf(&b, "| Agents | %d |\n", len(diffs))
	fmt.Fprintf(&b, "| Changed | %d |\n", changedCount)
	fmt.Fprintf(&b, "| Errors | %d |\n", errorCount)
	fmt.Fprintf(&b, "| Fleet Credits Delta | %s |\n", formatCredits(totalCredits))
	fmt.Fprintf(&b, "| Total Skills Leveled | %d |\n\n", totalSkills)

	// Errors
	if errorCount > 0 {
		b.WriteString("## Errors\n\n")
		b.WriteString("| Agent | Error |\n")
		b.WriteString("|-------|-------|\n")
		for _, d := range diffs {
			if d.Current.Error != "" {
				b.WriteString(fmt.Sprintf("| %s | %s |\n", d.AgentID, d.Current.Error))
			}
		}
		b.WriteString("\n")
	}

	// Changed agents
	if changedCount > 0 {
		b.WriteString("## Changes\n\n")
		b.WriteString("| Agent | Credits | Skills | Details |\n")
		b.WriteString("|-------|---------|--------|---------|\n")
		for _, d := range diffs {
			if !d.HasChanges || d.Current.Error != "" {
				continue
			}

			// Credits cell
			var credText string
			if prevDate == "" {
				credText = fmt.Sprintf("%.0f", d.Current.Credits)
			} else {
				credText = formatCredits(d.CreditsDelta)
			}

			// Skills cell
			var skills []string
			if prevDate == "" {
				skillIDs := make([]string, 0, len(d.Current.Skills))
				for id := range d.Current.Skills {
					skillIDs = append(skillIDs, id)
				}
				slices.Sort(skillIDs)
				for _, skillID := range skillIDs {
					skill := d.Current.Skills[skillID]
					skills = append(skills, fmt.Sprintf("%s: lvl %d (%d XP)", skillID, skill.Level, int(skill.XP)))
				}
			} else {
				skills = append(skills, d.SkillChanges...)
			}

			// Details cell
			var details []string
			if prevDate == "" {
				details = append(details, fmt.Sprintf("Ship: %s (%s)", d.Current.ShipName, d.Current.ShipClass))
				details = append(details, "Location: "+d.Current.Location)
				details = append(details, fmt.Sprintf("Ships: %d", d.Current.TotalShips))
			} else {
				details = append(details, d.StatChanges...)
				if d.ShipChanged != "" {
					details = append(details, "Ship: "+d.ShipChanged)
				}
				if d.StorageDelta != 0 {
					details = append(details, fmt.Sprintf("Storage: %+.0f units", d.StorageDelta))
				}
				if d.ShipsDelta != 0 {
					details = append(details, fmt.Sprintf("Ships: %+d (now %d)", d.ShipsDelta, d.Current.TotalShips))
				}
				if d.LocationTo != "" {
					details = append(details, fmt.Sprintf("Location: %s -> %s", d.LocationFrom, d.LocationTo))
				}
			}

			fmt.Fprintf(&b, "| **%s** (%s) | %s | %s | %s |\n",
				d.AgentID, d.Username, credText,
				strings.Join(skills, ", "), strings.Join(details, ", "))
		}
		b.WriteString("\n")
	}

	// Unchanged agents
	unchanged := 0
	for _, d := range diffs {
		if !d.HasChanges && d.Current.Error == "" {
			unchanged++
		}
	}
	if unchanged > 0 {
		b.WriteString("## Unchanged\n\n")
		for _, d := range diffs {
			if !d.HasChanges && d.Current.Error == "" {
				b.WriteString(fmt.Sprintf("- %s (%s) - %.0f credits at %s\n",
					d.AgentID, d.Username, d.Current.Credits, d.Current.Location))
			}
		}
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// writeHTMLReport generates a self-contained HTML report.
func writeHTMLReport(path, today, prevDate string, diffs []AgentDiff) error {
	var totalCredits float64
	var totalSkills int
	var changedCount, errorCount int
	for _, d := range diffs {
		if d.Current.Error != "" {
			errorCount++
			continue
		}
		totalCredits += d.CreditsDelta
		totalSkills += len(d.SkillChanges)
		if d.HasChanges {
			changedCount++
		}
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Daily Summary - ` + html.EscapeString(today) + `</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: 'Segoe UI', system-ui, sans-serif; background: #0a0e1a; color: #c8ccd4; line-height: 1.6; padding: 2rem; }
  .container { max-width: 1200px; margin: 0 auto; }
  h1 { color: #e0e4ec; font-size: 1.8rem; margin-bottom: 0.5rem; }
  h2 { color: #8b9dc3; font-size: 1.3rem; margin: 1.5rem 0 0.8rem; border-bottom: 1px solid #1e2740; padding-bottom: 0.3rem; }
  .subtitle { color: #6b7b9e; margin-bottom: 1.5rem; }
  .summary-bar { display: flex; gap: 1.5rem; flex-wrap: wrap; margin-bottom: 1.5rem; }
  .stat-card { background: #131829; border: 1px solid #1e2740; border-radius: 8px; padding: 1rem 1.5rem; min-width: 150px; }
  .stat-card .label { color: #6b7b9e; font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .stat-card .value { color: #e0e4ec; font-size: 1.5rem; font-weight: 600; }
  .positive { color: #4ade80; }
  .negative { color: #f87171; }
  .neutral { color: #94a3b8; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 1rem; }
  th { background: #131829; color: #8b9dc3; text-align: left; padding: 0.6rem 1rem; font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.05em; }
  td { padding: 0.6rem 1rem; border-bottom: 1px solid #1a2035; }
  tr:hover { background: #131829; }
  .agent-name { color: #60a5fa; font-weight: 500; }
  .change-list { list-style: none; padding: 0; }
  .change-list li { padding: 0.15rem 0; font-size: 0.9rem; }
  .change-list li::before { content: ""; margin-right: 0.4rem; }
  .error-row td { color: #f87171; }
  details { margin-top: 1rem; }
  summary { cursor: pointer; color: #6b7b9e; padding: 0.5rem 0; }
  summary:hover { color: #8b9dc3; }
  .unchanged-list { columns: 3; column-gap: 2rem; list-style: none; padding: 0.5rem 0; }
  .unchanged-list li { padding: 0.2rem 0; font-size: 0.9rem; color: #6b7b9e; }
</style>
</head>
<body>
<div class="container">
`)

	b.WriteString("<h1>Daily Summary</h1>\n")
	if prevDate != "" {
		b.WriteString(fmt.Sprintf(`<p class="subtitle">%s vs %s</p>`+"\n", html.EscapeString(today), html.EscapeString(prevDate)))
	} else {
		b.WriteString(fmt.Sprintf(`<p class="subtitle">%s (baseline)</p>`+"\n", html.EscapeString(today)))
	}

	// Summary bar
	creditsClass := "neutral"
	if totalCredits > 0 {
		creditsClass = "positive"
	} else if totalCredits < 0 {
		creditsClass = "negative"
	}
	b.WriteString(`<div class="summary-bar">` + "\n")
	writeStatCard(&b, "Agents", fmt.Sprintf("%d", len(diffs)), "")
	writeStatCard(&b, "Changed", fmt.Sprintf("%d", changedCount), "")
	writeStatCard(&b, "Errors", fmt.Sprintf("%d", errorCount), ternary(errorCount > 0, "negative", ""))
	writeStatCard(&b, "Fleet Credits", formatCredits(totalCredits), creditsClass)
	writeStatCard(&b, "Skills Leveled", fmt.Sprintf("%d", totalSkills), ternary(totalSkills > 0, "positive", ""))
	b.WriteString("</div>\n")

	// Errors section
	if errorCount > 0 {
		b.WriteString("<h2>Errors</h2>\n<table>\n<tr><th>Agent</th><th>Error</th></tr>\n")
		for _, d := range diffs {
			if d.Current.Error != "" {
				b.WriteString(fmt.Sprintf(`<tr class="error-row"><td>%s</td><td>%s</td></tr>`+"\n",
					html.EscapeString(d.AgentID), html.EscapeString(d.Current.Error)))
			}
		}
		b.WriteString("</table>\n")
	}

	// Changes table
	if changedCount > 0 {
		b.WriteString("<h2>Changes</h2>\n<table>\n")
		b.WriteString("<tr><th>Agent</th><th>Credits</th><th>Skills</th><th>Details</th></tr>\n")
		for _, d := range diffs {
			if !d.HasChanges || d.Current.Error != "" {
				continue
			}
			// Credits cell
			var credClass, credText string
			if prevDate == "" {
				credClass = "neutral"
				credText = fmt.Sprintf("%.0f", d.Current.Credits)
			} else {
				credClass = "neutral"
				if d.CreditsDelta > 0 {
					credClass = "positive"
				} else if d.CreditsDelta < 0 {
					credClass = "negative"
				}
				credText = formatCredits(d.CreditsDelta)
			}

			// Skills cell
			var skills []string
			if prevDate == "" {
				skillIDs := make([]string, 0, len(d.Current.Skills))
				for id := range d.Current.Skills {
					skillIDs = append(skillIDs, id)
				}
				slices.Sort(skillIDs)
				for _, skillID := range skillIDs {
					skill := d.Current.Skills[skillID]
					skills = append(skills, fmt.Sprintf("%s: level %d (%d XP)", html.EscapeString(skillID), skill.Level, int(skill.XP)))
				}
			} else {
				for _, sc := range d.SkillChanges {
					skills = append(skills, html.EscapeString(sc))
				}
			}
			skillsHTML := buildListHTML(skills)

			// Details cell (non-skill changes)
			var details []string
			if prevDate == "" {
				details = append(details, fmt.Sprintf("Ship: %s (%s)", html.EscapeString(d.Current.ShipName), html.EscapeString(d.Current.ShipClass)))
				details = append(details, "Location: "+html.EscapeString(d.Current.Location))
				details = append(details, fmt.Sprintf("Ships: %d", d.Current.TotalShips))
			} else {
				for _, sc := range d.StatChanges {
					details = append(details, "Stat "+html.EscapeString(sc))
				}
				if d.ShipChanged != "" {
					details = append(details, "Ship: "+html.EscapeString(d.ShipChanged))
				}
				if d.StorageDelta != 0 {
					details = append(details, fmt.Sprintf("Storage: %+.0f units", d.StorageDelta))
				}
				if d.ShipsDelta != 0 {
					details = append(details, fmt.Sprintf("Ships: %+d (now %d)", d.ShipsDelta, d.Current.TotalShips))
				}
				if d.LocationTo != "" {
					details = append(details, fmt.Sprintf("Location: %s -> %s",
						html.EscapeString(d.LocationFrom), html.EscapeString(d.LocationTo)))
				}
			}
			detailHTML := buildListHTML(details)

			fmt.Fprintf(&b, `<tr><td class="agent-name">%s<br><small>%s</small></td><td class="%s">%s</td><td>%s</td><td>%s</td></tr>`+"\n",
				html.EscapeString(d.AgentID), html.EscapeString(d.Username),
				credClass, credText, skillsHTML, detailHTML)
		}
		b.WriteString("</table>\n")
	}

	// Unchanged section (collapsed)
	unchangedCount := 0
	for _, d := range diffs {
		if !d.HasChanges && d.Current.Error == "" {
			unchangedCount++
		}
	}
	if unchangedCount > 0 {
		b.WriteString(fmt.Sprintf("<details>\n<summary>Unchanged agents (%d)</summary>\n", unchangedCount))
		b.WriteString(`<ul class="unchanged-list">` + "\n")
		for _, d := range diffs {
			if !d.HasChanges && d.Current.Error == "" {
				b.WriteString(fmt.Sprintf("<li>%s (%.0f cr)</li>\n",
					html.EscapeString(d.AgentID), d.Current.Credits))
			}
		}
		b.WriteString("</ul>\n</details>\n")
	}

	b.WriteString("</div>\n</body>\n</html>\n")

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func buildListHTML(items []string) string {
	if len(items) == 0 {
		return ""
	}
	h := `<ul class="change-list">`
	for _, item := range items {
		h += "<li>" + item + "</li>"
	}
	h += "</ul>"
	return h
}

func writeStatCard(b *strings.Builder, label, value, class string) {
	valueClass := "value"
	if class != "" {
		valueClass = "value " + class
	}
	fmt.Fprintf(b, `<div class="stat-card"><div class="label">%s</div><div class="%s">%s</div></div>`+"\n",
		html.EscapeString(label), valueClass, html.EscapeString(value))
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
