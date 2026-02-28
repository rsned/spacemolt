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

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	_ "modernc.org/sqlite"
)

var debug = flag.Bool("debug", false, "Enable game client debug logging")

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
	AgentID         string               `json:"agent_id"`
	Username        string               `json:"username"`
	Empire          string               `json:"empire"`
	Credits         float64              `json:"credits"`
	StorageCredits  float64              `json:"storage_credits"`
	Location        string               `json:"location"`
	Docked          bool                 `json:"docked"`
	POIType         string               `json:"poi_type"` // Type of POI agent is at (station, base, belt, etc.)
	Skills          map[string]SkillSnap `json:"skills"`
	Stats           game.PlayerStats     `json:"stats"`
	ShipClass       string               `json:"ship_class"`
	ShipName        string               `json:"ship_name"`
	CargoUsed       float64              `json:"cargo_used"`
	CargoCapacity   float64              `json:"cargo_capacity"`
	ModuleCount     int                  `json:"module_count"`
	TotalShips      int                  `json:"total_ships"`
	StorageItems    []StorageEntry       `json:"storage_items"`
	StorageTotal    float64              `json:"storage_total"`
	FactionID       string               `json:"faction_id"`
	FactionRank     string               `json:"faction_rank"`
	Experience      int64                `json:"experience"`
	Error           string               `json:"error,omitempty"`
	CapturedAt      time.Time            `json:"captured_at"`
}

// AgentDiff holds the computed differences between two snapshots.
type AgentDiff struct {
	AgentID            string
	Username           string
	Empire             string
	CreditsDelta       float64
	StorageCreditsDelta float64
	TotalCreditsDelta  float64
	SkillChanges       []string // e.g. "Mining: 4 -> 5"
	StatChanges        []string // e.g. "OreMined: +150.0"
	ShipChanged        string   // e.g. "Prospector -> Drillship"
	StorageDelta       float64
	StorageItemsDelta  int      // Change in number of storage items
	StorageItemChanges []string // e.g. "iron_ore: +100.0" or "copper_ore: -50.0"
	ShipsDelta         int
	LocationFrom       string
	LocationTo         string
	HasChanges         bool
	IsNew              bool // Agent appearing for first time
	Current            *AgentSnapshot
}

func main() {
	dbPath := flag.String("db", "data/daily-summary.db", "SQLite database path")
	outputPath := flag.String("output", "", "Report output base path (default: data/reports/daily-summary-YYYY-MM-DD)")
	agents := flag.String("agents", "", "Comma-separated agent filter (default: all from data/agents/)")
	delay := flag.Int("delay", 3, "Delay in seconds between agent connections")
	reportOnly := flag.Bool("report-only", false, "Skip data collection, regenerate report from latest DB data")
	regenerateAll := flag.Bool("regenerate-all", false, "Regenerate all HTML and Markdown reports from database data")
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

	// Regenerate all reports if requested
	if *regenerateAll {
		if err := regenerateAllReports(db, *outputPath, logger); err != nil {
			logger.Fatalf("Failed to regenerate all reports: %v", err)
		}
		return
	}

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

	nextDate, err := findNextDate(db, today)
	if err != nil {
		logger.Fatalf("Failed to find next date: %v", err)
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

	if err := writeHTMLReport(*outputPath+".html", today, prevDate, nextDate, diffs); err != nil {
		logger.Fatalf("Failed to write HTML report: %v", err)
	}
	logger.Printf("HTML report: %s.html", *outputPath)

	// Generate/update index page with all daily summary links
	allDates, err := getAllDates(db)
	if err != nil {
		logger.Printf("Warning: failed to get all dates for index: %v", err)
	} else {
		outputDir := filepath.Dir(*outputPath)
		if err := writeIndexPage(outputDir, allDates); err != nil {
			logger.Printf("Warning: failed to write index page: %v", err)
		} else {
			logger.Printf("Index page: %s/index.html (%d reports)", outputDir, len(allDates))
		}
	}

	// Update previous day's HTML to include Next link to today
	if prevDate != "" {
		logger.Printf("Updating previous day's report with Next link...")
		prevPrevDate, err := findPreviousDate(db, prevDate)
		if err != nil {
			logger.Printf("Warning: failed to find previous-previous date: %v", err)
			prevPrevDate = ""
		}
		prevSnaps, err := loadSnapshots(db, prevDate)
		if err != nil {
			logger.Printf("Warning: failed to load previous snapshots for update: %v", err)
		} else {
			// Load prevPrevDate's snapshots for comparison
			var prevPrevSnaps map[string]*AgentSnapshot
			if prevPrevDate != "" {
				prevPrevSnaps, err = loadSnapshots(db, prevPrevDate)
				if err != nil {
					logger.Printf("Warning: failed to load prev-prev snapshots: %v", err)
					prevPrevSnaps = map[string]*AgentSnapshot{}
				}
			} else {
				prevPrevSnaps = map[string]*AgentSnapshot{}
			}
			// Compute diffs for the previous day (prevDate vs prevPrevDate)
			prevDiffs := computeDiffs(prevSnaps, prevPrevSnaps)
			// Build the output path for the previous day's report
			prevOutputPath := filepath.Join(filepath.Dir(*outputPath), "daily-summary-"+prevDate)
			if err := writeHTMLReport(prevOutputPath+".html", prevDate, prevPrevDate, today, prevDiffs); err != nil {
				logger.Printf("Warning: failed to update previous day's HTML: %v", err)
			} else {
				logger.Printf("Updated previous day's HTML report with Next link to %s", today)
			}
		}
	}
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
	var notAtStation []string

	for i, agentID := range agentList {
		if i > 0 {
			time.Sleep(time.Duration(delaySec) * time.Second)
		}
		logger.Printf("[%d/%d] Collecting %s...", i+1, len(agentList), agentID)
		snap := captureAgent(agentID, logger)
		if err := saveSnapshot(db, snap, today); err != nil {
			logger.Printf("  Failed to save snapshot: %v", err)
		}

		// Track agents not docked at station or base (incomplete storage data)
		if snap.Error == "" && snap.POIType != "station" && snap.POIType != "base" {
			notAtStation = append(notAtStation, agentID)
		}
	}

	// Print summary of agents not at stations
	if len(notAtStation) > 0 {
		logger.Printf("")
		logger.Printf("⚠️  Agents NOT at station/base (may have incomplete storage data):")
		for _, agentID := range notAtStation {
			logger.Printf("   - %s", agentID)
		}
		logger.Printf("")
		logger.Printf("💡 Tip: Re-run daily-summary to recapture data for these agents when they dock")
		logger.Printf("   Example: go run ./cmd/tools/daily-summary -agents %s", strings.Join(notAtStation, ","))
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

	client, creds, err := game.InitializeAgent(agentID, logger, ctx, *debug)
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

	// Determine POI type
	for _, poi := range state.System.POIs {
		if poi.ID == state.CurrentPOI {
			snap.POIType = poi.Type
			break
		}
	}

	logger.Printf("  Credits: %.0f (wallet)", snap.Credits)
	logger.Printf("  Docked: %v, POI: '%s' (type: %s)", state.Doc, state.CurrentPOI, snap.POIType)

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

	// Best-effort: storage (when docked OR with station_id)
	if state.Doc || state.CurrentPOI != "" {
		// Always try with station_id if we have a POI
		if state.CurrentPOI != "" {
			logger.Printf("  Viewing storage at: %s (docked: %v)", state.CurrentPOI, state.Doc)
			// Send view_storage with station_id parameter (works whether docked or not)
			resp, err := client.SendQueued(ctx, protocol.Message{
				Type:      "view_storage",
				Timestamp: time.Now().UnixMilli(),
				Payload: map[string]any{
					"station_id": state.CurrentPOI,
				},
			}, 10*time.Second)
			if err != nil {
				logger.Printf("  Warning: Failed to view storage: %v", err)
			} else if resp.Type == protocol.TypeError {
				// Server returned an error response
				if msg, ok := resp.Payload["message"].(string); ok {
					logger.Printf("  Warning: Server error: %s", msg)
				} else {
					logger.Printf("  Warning: Server returned error response")
				}
			}
			// Response is automatically stored by storeRawJSON handler
		}

		if rawJSON := client.GetRawJSON("storage"); rawJSON != nil {
			var storageResp struct {
				Credits float64 `json:"credits"`
				Items   []struct {
					ItemID   string  `json:"item_id"`
					Quantity float64 `json:"quantity"`
				} `json:"items"`
			}
			if json.Unmarshal(rawJSON, &storageResp) == nil {
				snap.StorageCredits = storageResp.Credits
				for _, item := range storageResp.Items {
					snap.StorageItems = append(snap.StorageItems, StorageEntry{
						ItemID:   item.ItemID,
						Quantity: item.Quantity,
					})
					snap.StorageTotal += item.Quantity
				}
				totalCreds := snap.Credits + snap.StorageCredits
				logger.Printf("  Storage: %.0f credits (Total: %.0f)", snap.StorageCredits, totalCreds)
			} else {
				logger.Printf("  Warning: Failed to parse storage response")
			}
		} else {
			logger.Printf("  Warning: No storage data in response")
		}
	} else {
		logger.Printf("  Not docked and no POI, skipping storage check")
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

// findNextDate finds the next snapshot date after the given date.
func findNextDate(db *sql.DB, currentDate string) (string, error) {
	var nextDate string
	err := db.QueryRow(
		`SELECT DISTINCT captured_date FROM snapshots
		 WHERE captured_date > ? ORDER BY captured_date ASC LIMIT 1`, currentDate,
	).Scan(&nextDate)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return nextDate, err
}

// getAllDates retrieves all snapshot dates, ordered newest to oldest.
func getAllDates(db *sql.DB) ([]string, error) {
	rows, err := db.Query(
		`SELECT DISTINCT captured_date FROM snapshots ORDER BY captured_date DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var dates []string
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return nil, err
		}
		dates = append(dates, date)
	}
	return dates, rows.Err()
}

// writeIndexPage generates an index page with links to all daily summaries.
func writeIndexPage(outputDir string, dates []string) error {
	path := filepath.Join(outputDir, "index.html")
	var b strings.Builder

	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Daily Summary Index</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600&display=swap');

  :root {
    --smui-surface-0: hsl(222, 47%, 11%);
    --smui-surface-1: hsl(222, 47%, 14%);
    --smui-surface-2: hsl(222, 45%, 18%);
    --smui-surface-3: hsl(222, 43%, 22%);
    --smui-text-primary: hsl(220, 15%, 95%);
    --smui-text-secondary: hsl(220, 12%, 70%);
    --smui-text-muted: hsl(220, 10%, 50%);
    --smui-aurora-green: hsl(150, 70%, 55%);
    --smui-aurora-blue: hsl(200, 70%, 55%);
  }

  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: 'JetBrains Mono', monospace;
    background: var(--smui-surface-0);
    color: var(--smui-text-secondary);
    line-height: 1.6;
    padding: 2rem;
  }
  .container { max-width: 800px; margin: 0 auto; }
  h1 {
    color: var(--smui-text-primary);
    font-size: 1.8rem;
    margin-bottom: 0.5rem;
    font-weight: 600;
  }
  .subtitle {
    color: var(--smui-text-muted);
    font-size: 0.9rem;
    margin-bottom: 2rem;
  }
  .date-list {
    list-style: none;
    padding: 0;
  }
  .date-list li {
    margin-bottom: 0.5rem;
  }
  .date-link {
    display: block;
    padding: 0.75rem 1rem;
    background: var(--smui-surface-1);
    color: var(--smui-text-secondary);
    text-decoration: none;
    border-radius: 4px;
    transition: background 0.2s, color 0.2s;
  }
  .date-link:hover {
    background: var(--smui-surface-2);
    color: var(--smui-text-primary);
  }
  .date-link .date {
    font-weight: 500;
    color: var(--smui-text-primary);
  }
  .date-link .label {
    color: var(--smui-text-muted);
    font-size: 0.85rem;
  }
  .footer {
    margin-top: 3rem;
    padding-top: 1rem;
    border-top: 1px solid var(--smui-surface-2);
    color: var(--smui-text-muted);
    font-size: 0.85rem;
  }
</style>
</head>
<body>
<div class="container">
<h1>Daily Summary Index</h1>
<p class="subtitle">` + fmt.Sprintf("%d reports available", len(dates)) + `</p>

<ul class="date-list">
`)
	for i, date := range dates {
		var label string
		if i == 0 {
			label = `<span class="label">(latest)</span>`
		}
		b.WriteString(fmt.Sprintf(`<li><a href="daily-summary-%s.html" class="date-link"><span class="date">%s</span> %s</a></li>`+"\n",
			html.EscapeString(date), html.EscapeString(date), label))
	}
	b.WriteString(`</ul>

<div class="footer">
  Generated by SpaceMolt daily-summary tool
</div>

</div>
</body>
</html>
`)

	return os.WriteFile(path, []byte(b.String()), 0644)
}

// regenerateAllReports regenerates HTML and Markdown reports for all dates in the database.
func regenerateAllReports(db *sql.DB, outputPath string, logger *log.Logger) error {
	logger.Printf("Regenerating all reports...")

	// Get all dates from the database
	dates, err := getAllDates(db)
	if err != nil {
		return fmt.Errorf("failed to get all dates: %w", err)
	}

	if len(dates) == 0 {
		logger.Printf("No dates found in database")
		return nil
	}

	logger.Printf("Found %d dates to regenerate", len(dates))

	// Determine output directory
	outputDir := "data/reports"
	if outputPath != "" {
		outputDir = filepath.Dir(outputPath)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create report directory: %w", err)
	}

	// Regenerate each date's reports
	for i, date := range dates {
		logger.Printf("[%d/%d] Regenerating %s...", i+1, len(dates), date)

		// Find previous and next dates
		prevDate, err := findPreviousDate(db, date)
		if err != nil {
			logger.Printf("Warning: failed to find previous date for %s: %v", date, err)
			prevDate = ""
		}

		nextDate, err := findNextDate(db, date)
		if err != nil {
			logger.Printf("Warning: failed to find next date for %s: %v", date, err)
			nextDate = ""
		}

		// Load snapshots for this date
		snaps, err := loadSnapshots(db, date)
		if err != nil {
			logger.Printf("Warning: failed to load snapshots for %s: %v", date, err)
			continue
		}

		// Load previous snapshots for comparison
		var prevSnaps map[string]*AgentSnapshot
		if prevDate != "" {
			prevSnaps, err = loadSnapshots(db, prevDate)
			if err != nil {
				logger.Printf("Warning: failed to load previous snapshots for %s: %v", date, err)
				prevSnaps = make(map[string]*AgentSnapshot)
			}
		} else {
			prevSnaps = make(map[string]*AgentSnapshot)
		}

		// Compute diffs
		diffs := computeDiffs(snaps, prevSnaps)

		// Generate reports
		dateOutputPath := filepath.Join(outputDir, "daily-summary-"+date)

		if err := writeMarkdownReport(dateOutputPath+".md", date, prevDate, diffs); err != nil {
			logger.Printf("Warning: failed to write markdown report for %s: %v", date, err)
		} else {
			logger.Printf("  Markdown: %s.md", dateOutputPath)
		}

		if err := writeHTMLReport(dateOutputPath+".html", date, prevDate, nextDate, diffs); err != nil {
			logger.Printf("Warning: failed to write HTML report for %s: %v", date, err)
		} else {
			logger.Printf("  HTML: %s.html", dateOutputPath)
		}
	}

	// Update index page
	if err := writeIndexPage(outputDir, dates); err != nil {
		logger.Printf("Warning: failed to write index page: %v", err)
	} else {
		logger.Printf("Index page: %s/index.html (%d reports)", outputDir, len(dates))
	}

	logger.Printf("Successfully regenerated %d reports", len(dates))
	return nil
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
			diff.IsNew = !hasPrev
			diff.HasChanges = snap.Error == ""
			diffs = append(diffs, diff)
			continue
		}

		// Credits
		diff.CreditsDelta = snap.Credits - old.Credits
		if math.Abs(diff.CreditsDelta) >= 1 {
			diff.HasChanges = true
		}

		// Storage credits
		diff.StorageCreditsDelta = snap.StorageCredits - old.StorageCredits
		if math.Abs(diff.StorageCreditsDelta) >= 1 {
			diff.HasChanges = true
		}

		// Total credits (on hand + storage)
		diff.TotalCreditsDelta = (snap.Credits + snap.StorageCredits) - (old.Credits + old.StorageCredits)
		if math.Abs(diff.TotalCreditsDelta) >= 1 {
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

		// Storage items count
		diff.StorageItemsDelta = len(snap.StorageItems) - len(old.StorageItems)
		if diff.StorageItemsDelta != 0 {
			diff.HasChanges = true
		}

		// Storage item changes (individual items added/removed)
		diff.StorageItemChanges = diffStorageItems(old.StorageItems, snap.StorageItems)
		if len(diff.StorageItemChanges) > 0 {
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
	return sign + formatNumber(v)
}

func formatNumber(v float64) string {
	// Format with thousands separator
	abs := math.Abs(v)
	intPart := int64(math.Floor(abs))
	decPart := abs - float64(intPart)

	// Add thousands separator
	intStr := fmt.Sprintf("%d", intPart)
	var result strings.Builder
	for i := 0; i < len(intStr); i++ {
		if i > 0 && (len(intStr)-i)%3 == 0 {
			result.WriteString(",")
		}
		result.WriteByte(intStr[i])
	}

	// Add decimal part if significant
	if decPart >= 0.01 {
		result.WriteString(fmt.Sprintf(".%.2f", decPart)[1:])
	}

	if v < 0 {
		return "-" + result.String()
	}
	return result.String()
}

// diffStorageItems compares two storage item lists and returns human-readable delta strings.
func diffStorageItems(old, cur []StorageEntry) []string {
	// Build maps for easy lookup
	oldItems := make(map[string]float64)
	for _, item := range old {
		oldItems[item.ItemID] = item.Quantity
	}

	curItems := make(map[string]float64)
	for _, item := range cur {
		curItems[item.ItemID] = item.Quantity
	}

	// Collect all item IDs
	allItemIDs := make(map[string]bool)
	for id := range oldItems {
		allItemIDs[id] = true
	}
	for id := range curItems {
		allItemIDs[id] = true
	}

	var changes []string
	for id := range allItemIDs {
		oldQty := oldItems[id]
		curQty := curItems[id]
		delta := curQty - oldQty

		// Only report changes (items added, removed, or quantity changed)
		// Show if item is new (oldQty == 0), removed (curQty == 0), or changed
		if math.Abs(delta) >= 0.01 || (oldQty == 0 && curQty > 0) || (curQty == 0 && oldQty > 0) {
			if delta > 0 {
				changes = append(changes, fmt.Sprintf("%s: +%.1f", id, delta))
			} else if delta < 0 {
				changes = append(changes, fmt.Sprintf("%s: %.1f", id, delta))
			}
			// If delta is 0 but item was added then removed or vice versa, don't show
		}
	}

	// Sort by item ID
	slices.Sort(changes)

	// Limit to top 20 changes to avoid overwhelming output
	if len(changes) > 20 {
		changes = changes[:20]
		changes = append(changes, fmt.Sprintf("... and %d more", len(changes)-20))
	}

	return changes
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
	var totalOreMined float64
	var totalItemsCrafted int
	var changedCount, errorCount int
	for _, d := range diffs {
		if d.Current.Error != "" {
			errorCount++
			continue
		}
		totalCredits += d.TotalCreditsDelta
		totalSkills += len(d.SkillChanges)
		if d.HasChanges {
			changedCount++
		}
		// Parse stat changes for ore mined and items crafted
		for _, stat := range d.StatChanges {
			if strings.HasPrefix(stat, "OreMined:") {
				var val float64
				if _, err := fmt.Sscanf(stat, "OreMined: %f", &val); err == nil {
					totalOreMined += val
				}
			}
			if strings.HasPrefix(stat, "ItemsCrafted:") {
				var val int
				if _, err := fmt.Sscanf(stat, "ItemsCrafted: %d", &val); err == nil {
					totalItemsCrafted += val
				}
			}
		}
	}

	b.WriteString("## Fleet Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	fmt.Fprintf(&b, "| Agents | %d |\n", len(diffs))
	fmt.Fprintf(&b, "| Changed | %d |\n", changedCount)
	fmt.Fprintf(&b, "| Errors | %d |\n", errorCount)
	fmt.Fprintf(&b, "| Fleet Credits Delta | %s |\n", formatCredits(totalCredits))
	fmt.Fprintf(&b, "| Total Skills Leveled | %d |\n", totalSkills)
	if totalOreMined > 0 {
		fmt.Fprintf(&b, "| Ore Mined | %s |\n", formatNumber(totalOreMined))
	}
	if totalItemsCrafted > 0 {
		fmt.Fprintf(&b, "| Items Crafted | %s |\n", formatNumber(float64(totalItemsCrafted)))
	}
	b.WriteString("\n")

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
				totalCreds := d.Current.Credits + d.Current.StorageCredits
				credText = fmt.Sprintf("%.0f", totalCreds)
				if d.Current.StorageCredits > 0 {
					credText += fmt.Sprintf(" (%.0f in storage)", d.Current.StorageCredits)
				}
			} else {
				credText = formatCredits(d.TotalCreditsDelta)
				if d.StorageCreditsDelta != 0 {
					credText += fmt.Sprintf(" [%s: %s]", formatCredits(d.CreditsDelta), formatCredits(d.StorageCreditsDelta))
				}
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
				// Add individual storage item changes
				details = append(details, d.StorageItemChanges...)
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
				totalCreds := d.Current.Credits + d.Current.StorageCredits
				creditsText := fmt.Sprintf("%.0f credits", totalCreds)
				if d.Current.StorageCredits > 0 {
					creditsText += fmt.Sprintf(" (%.0f in storage)", d.Current.StorageCredits)
				}
				b.WriteString(fmt.Sprintf("- %s (%s) - %s at %s\n",
					d.AgentID, d.Username, creditsText, d.Current.Location))
			}
		}
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// writeHTMLReport generates a self-contained HTML report.
func writeHTMLReport(path, today, prevDate, nextDate string, diffs []AgentDiff) error {
	var totalCreditsDelta float64
	var totalAllCredits float64
	var totalCreditsSpent, totalCreditsEarned float64
	var totalSkills int
	var totalOreMined float64
	var totalItemsCrafted int
	var totalStorageItems int
	var totalStorageItemsDelta int
	var changedCount, errorCount int

	// Calculate totals
	for _, d := range diffs {
		if d.Current.Error != "" {
			errorCount++
			continue
		}
		totalCreditsDelta += d.TotalCreditsDelta
		totalAllCredits += d.Current.Credits + d.Current.StorageCredits
		totalSkills += len(d.SkillChanges)
		totalStorageItems += len(d.Current.StorageItems)
		totalStorageItemsDelta += d.StorageItemsDelta
		if d.HasChanges {
			changedCount++
		}

		// Extract stats from StatChanges
		for _, sc := range d.StatChanges {
			if strings.HasPrefix(sc, "CreditsSpent:") {
				parts := strings.Fields(sc)
				if len(parts) >= 2 {
					var val float64
					_, _ = fmt.Sscanf(parts[1], "%f", &val)
					totalCreditsSpent += val
				}
			} else if strings.HasPrefix(sc, "CreditsEarned:") {
				parts := strings.Fields(sc)
				if len(parts) >= 2 {
					var val float64
					_, _ = fmt.Sscanf(parts[1], "%f", &val)
					totalCreditsEarned += val
				}
			} else if strings.HasPrefix(sc, "OreMined:") {
				parts := strings.Fields(sc)
				if len(parts) >= 2 {
					var val float64
					_, _ = fmt.Sscanf(parts[1], "%f", &val)
					totalOreMined += val
				}
			} else if strings.HasPrefix(sc, "ItemsCrafted:") {
				parts := strings.Fields(sc)
				if len(parts) >= 2 {
					var val int
					_, _ = fmt.Sscanf(parts[1], "%d", &val)
					totalItemsCrafted += val
				}
			}
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
  @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600&display=swap');

  :root {
    --smui-frost-1: hsl(220, 25%, 95%);
    --smui-frost-2: hsl(220, 20%, 85%);
    --smui-frost-3: hsl(220, 15%, 65%);
    --smui-frost-4: hsl(220, 15%, 40%);
    --smui-aurora-red: hsl(350, 80%, 65%);
    --smui-aurora-orange: hsl(25, 85%, 60%);
    --smui-aurora-yellow: hsl(45, 90%, 60%);
    --smui-aurora-green: hsl(150, 70%, 55%);
    --smui-aurora-purple: hsl(270, 70%, 65%);
    --smui-surface-0: hsl(222, 47%, 11%);
    --smui-surface-1: hsl(222, 47%, 14%);
    --smui-surface-2: hsl(222, 45%, 18%);
    --smui-surface-3: hsl(222, 43%, 22%);
    --smui-text-primary: hsl(220, 15%, 95%);
    --smui-text-secondary: hsl(220, 12%, 70%);
    --smui-text-muted: hsl(220, 10%, 50%);
  }

  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: 'JetBrains Mono', monospace;
    background: var(--smui-surface-0);
    color: var(--smui-text-secondary);
    line-height: 1.6;
    padding: 2rem;
  }
  .container { max-width: 1400px; margin: 0 auto; }
  h1 {
    color: var(--smui-text-primary);
    font-size: 1.8rem;
    margin-bottom: 0.5rem;
    font-weight: 600;
    letter-spacing: -0.02em;
  }
  h2 {
    color: var(--smui-frost-3);
    font-size: 1.1rem;
    margin: 2rem 0 0.8rem;
    border-bottom: 1px solid var(--smui-surface-2);
    padding-bottom: 0.4rem;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }
  .subtitle { color: var(--smui-text-muted); margin-bottom: 1.5rem; font-size: 0.9rem; }

  .nav-links {
    display: flex;
    gap: 1rem;
    margin-bottom: 1.5rem;
  }
  .nav-link {
    color: var(--smui-frost-3);
    text-decoration: none;
    padding: 0.5rem 1rem;
    background: var(--smui-surface-1);
    border: 1px solid var(--smui-surface-2);
    font-size: 0.85rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    transition: all 0.15s ease;
  }
  .nav-link:hover:not(.disabled) {
    background: var(--smui-surface-2);
    color: var(--smui-text-primary);
  }
  .nav-link.disabled {
    opacity: 0.3;
    cursor: not-allowed;
    text-decoration: none;
  }

  .summary-bar { display: flex; gap: 1rem; flex-wrap: wrap; margin-bottom: 2rem; }
  .stat-card {
    background: var(--smui-surface-1);
    border: 1px solid var(--smui-surface-2);
    padding: 0.75rem 1.25rem;
    min-width: 140px;
  }
  .stat-card .label {
    color: var(--smui-text-muted);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.15em;
    margin-bottom: 0.25rem;
  }
  .stat-card .value {
    color: var(--smui-text-primary);
    font-size: 1.4rem;
    font-weight: 600;
  }
  .positive { color: var(--smui-aurora-green) !important; }
  .negative { color: var(--smui-aurora-red) !important; }
  .neutral { color: var(--smui-text-secondary) !important; }

  table { width: 100%; border-collapse: collapse; margin-bottom: 1rem; }
  th {
    background: var(--smui-surface-1);
    color: var(--smui-frost-3);
    text-align: left;
    padding: 0.75rem 1rem;
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.15em;
    font-weight: 500;
    border-bottom: 2px solid var(--smui-surface-2);
  }
  td {
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--smui-surface-2);
    vertical-align: top;
  }
  tr:hover { background: var(--smui-surface-1); }
  .agent-name {
    color: var(--smui-frost-2);
    font-weight: 500;
  }
  .agent-name small {
    color: var(--smui-frost-2);
    font-size: 0.8em;
    display: block;
    margin-top: 0.2rem;
  }

  .change-list { list-style: none; padding: 0; }
  .change-list li {
    padding: 0.2rem 0;
    font-size: 0.85rem;
    color: var(--smui-text-secondary);
  }
  .change-list li::before { content: ""; margin-right: 0.4rem; }

  .error-row td { color: var(--smui-aurora-red); }

  details { margin-top: 1rem; }
  summary {
    cursor: pointer;
    color: var(--smui-text-muted);
    padding: 0.5rem 0;
    font-size: 0.9rem;
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }
  summary:hover { color: var(--smui-frost-3); }

  .unchanged-list {
    columns: 3;
    column-gap: 2rem;
    list-style: none;
    padding: 0.5rem 0;
  }
  .unchanged-list li {
    padding: 0.2rem 0;
    font-size: 0.85rem;
    color: var(--smui-text-muted);
  }
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

	// Navigation links
	b.WriteString(`<div class="nav-links">` + "\n")
	if prevDate != "" {
		b.WriteString(fmt.Sprintf(`<a href="daily-summary-%s.html" class="nav-link">← Previous (%s)</a>`+"\n",
			html.EscapeString(prevDate), html.EscapeString(prevDate)))
	} else {
		b.WriteString(`<span class="nav-link disabled">← Previous</span>` + "\n")
	}
	// Back to Index link
	b.WriteString(`<a href="index.html" class="nav-link">Back To Index</a>` + "\n")
	if nextDate != "" {
		b.WriteString(fmt.Sprintf(`<a href="daily-summary-%s.html" class="nav-link">Next (%s) →</a>`+"\n",
			html.EscapeString(nextDate), html.EscapeString(nextDate)))
	} else {
		b.WriteString(`<span class="nav-link disabled">Next →</span>` + "\n")
	}
	b.WriteString("</div>\n")

	// Summary bar
	b.WriteString(`<div class="summary-bar">` + "\n")
	writeStatCard(&b, "Agents", fmt.Sprintf("%d", len(diffs)), "")
	writeStatCard(&b, "Changed", fmt.Sprintf("%d", changedCount), "")

	// Fleet Credits with trend arrow, spent and earned breakdown (write directly to avoid HTML escaping)
	trendArrow := "→"
	trendClass := "neutral"
	if totalCreditsDelta > 0 {
		trendArrow = "↗"
		trendClass = "positive"
	} else if totalCreditsDelta < 0 {
		trendArrow = "↘"
		trendClass = "negative"
	}
	b.WriteString(fmt.Sprintf(`<div class="stat-card"><div class="label">%s</div><div class="value">`, "Fleet Credits"))
	// Total credits on first line
	b.WriteString(fmt.Sprintf(`<span class="positive">%s</span>`, formatNumber(totalAllCredits)))
	// Delta on second line with arrow
	b.WriteString(fmt.Sprintf(`<br><small class="%s">%s %s</small>`, trendClass, trendArrow, formatNumber(totalCreditsDelta)))
	if totalCreditsSpent > 0 || totalCreditsEarned > 0 {
		b.WriteString(fmt.Sprintf(`<br><small class="negative">%s</small>`, formatNumber(totalCreditsSpent)))
		if totalCreditsEarned > 0 {
			if totalCreditsSpent > 0 {
				b.WriteString(` `)
			}
			b.WriteString(fmt.Sprintf(`<small class="positive">%s</small>`, formatNumber(totalCreditsEarned)))
		}
	}
	b.WriteString(`</div></div>` + "\n")

	// Stored Items card with trend arrow and delta
	if totalStorageItems > 0 || totalStorageItemsDelta != 0 {
		itemsTrendArrow := "→"
		itemsTrendClass := "neutral"
		if totalStorageItemsDelta > 0 {
			itemsTrendArrow = "↗"
			itemsTrendClass = "positive"
		} else if totalStorageItemsDelta < 0 {
			itemsTrendArrow = "↘"
			itemsTrendClass = "negative"
		}
		b.WriteString(fmt.Sprintf(`<div class="stat-card"><div class="label">%s</div><div class="value">`, "Stored Items"))
		b.WriteString(fmt.Sprintf(`<span class="neutral">%d</span>`, totalStorageItems))
		if prevDate != "" {
			b.WriteString(fmt.Sprintf(`<br><small class="%s">%s %+d</small>`, itemsTrendClass, itemsTrendArrow, totalStorageItemsDelta))
		}
		b.WriteString(`</div></div>` + "\n")
	}

	writeStatCard(&b, "Skills Leveled", fmt.Sprintf("%d", totalSkills), ternary(totalSkills > 0, "positive", ""))
	if totalOreMined > 0 {
		writeStatCard(&b, "Ore Mined", formatNumber(totalOreMined), "positive")
	}
	if totalItemsCrafted > 0 {
		writeStatCard(&b, "Items Crafted", formatNumber(float64(totalItemsCrafted)), "positive")
	}
	b.WriteString("</div>\n")

	// Changes table
	if changedCount > 0 {
		b.WriteString("<h2>Changes</h2>\n<table>\n")
		b.WriteString("<tr><th>Agent</th><th>Credits</th><th>Skills</th><th>Location</th><th>Storage</th></tr>\n")
		for _, d := range diffs {
			if !d.HasChanges || d.Current.Error != "" {
				continue
			}

			// Extract CreditsEarned and CreditsSpent values from StatChanges for Credits column
			var creditsEarnedVal, creditsSpentVal float64
			var otherStatChanges []string
			for _, sc := range d.StatChanges {
				if strings.HasPrefix(sc, "CreditsSpent:") {
					// Parse value from "CreditsSpent: +376.0"
					parts := strings.Fields(sc)
					if len(parts) >= 2 {
						_, _ = fmt.Sscanf(parts[1], "%f", &creditsSpentVal)
					}
				} else if strings.HasPrefix(sc, "CreditsEarned:") {
					// Parse value from "CreditsEarned: +2550.0"
					parts := strings.Fields(sc)
					if len(parts) >= 2 {
						_, _ = fmt.Sscanf(parts[1], "%f", &creditsEarnedVal)
					}
				} else {
					otherStatChanges = append(otherStatChanges, sc)
				}
			}

			// Credits cell: show total with trend arrow, then spent (red) and earned (green) below
			var credText string
			if prevDate == "" {
				// Baseline report: show current total with no trend arrow
				totalCreds := d.Current.Credits + d.Current.StorageCredits
				credText = fmt.Sprintf(`<span class="positive">%s</span>`, formatNumber(totalCreds))
				if d.Current.StorageCredits > 0 {
					credText += fmt.Sprintf(`<br><small class="neutral">%s in storage</small>`, formatNumber(d.Current.StorageCredits))
				}
			} else {
				// Comparison report: show total with trend arrow
				totalCreds := d.Current.Credits + d.Current.StorageCredits
				trendArrow := "→"
				trendClass := "neutral"
				if d.TotalCreditsDelta > 0 {
					trendArrow = "↗"
					trendClass = "positive"
				} else if d.TotalCreditsDelta < 0 {
					trendArrow = "↘"
					trendClass = "negative"
				}
				credText = fmt.Sprintf(`<span class="positive">%s</span> <small class="%s">%s</small>`,
					formatNumber(totalCreds), trendClass, trendArrow)
			}

			// Add spent and earned values below (without labels)
			if creditsSpentVal > 0 {
				credText += fmt.Sprintf(`<br><small class="negative">%s</small>`, formatNumber(creditsSpentVal))
			}
			if creditsEarnedVal > 0 {
				if creditsSpentVal > 0 {
					credText += ` `
				} else {
					credText += `<br>`
				}
				credText += fmt.Sprintf(`<small class="positive">%s</small>`, formatNumber(creditsEarnedVal))
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
					skills = append(skills, fmt.Sprintf("%s<br><small>level %d (%d XP)</small>",
						html.EscapeString(skillID), skill.Level, int(skill.XP)))
				}
			} else {
				for _, sc := range d.SkillChanges {
					// Parse skill change to format with skill name on one line, details below
					if idx := strings.Index(sc, ":"); idx != -1 {
						skillName := strings.TrimSpace(sc[:idx])
						details := strings.TrimSpace(sc[idx+1:])
						var formatted string
						if strings.Contains(details, "new at level") {
							// New skill - sparkling star emoji after name
							formatted = fmt.Sprintf("%s ✨<br><small>%s</small>",
								html.EscapeString(skillName), html.EscapeString(details))
						} else if strings.Contains(details, "level ") && strings.Contains(details, " -> ") {
							// Level up - green up arrow after name (same as credits positive trend)
							formatted = fmt.Sprintf("%s <small class=\"positive\">↗</small><br><small>%s</small>",
								html.EscapeString(skillName), html.EscapeString(details))
						} else {
							// XP gain - no emoji
							formatted = fmt.Sprintf("%s<br><small>%s</small>",
								html.EscapeString(skillName), html.EscapeString(details))
						}
						skills = append(skills, formatted)
					} else {
						// Fallback for unexpected format
						skills = append(skills, html.EscapeString(sc))
					}
				}
			}
			skillsHTML := buildListHTML(skills)

			// Location cell - show only system changes
			var locationHTML string
			if prevDate == "" {
				// Baseline: extract system from "System / POI" format
				if idx := strings.Index(d.Current.Location, " / "); idx != -1 {
					locationHTML = html.EscapeString(d.Current.Location[:idx])
				} else {
					locationHTML = html.EscapeString(d.Current.Location)
				}
			} else if d.LocationTo != "" {
				// Extract system names from "System / POI" format
				var systemFrom, systemTo string
				if idx := strings.Index(d.LocationFrom, " / "); idx != -1 {
					systemFrom = d.LocationFrom[:idx]
				} else {
					systemFrom = d.LocationFrom
				}
				if idx := strings.Index(d.LocationTo, " / "); idx != -1 {
					systemTo = d.LocationTo[:idx]
				} else {
					systemTo = d.LocationTo
				}

				// Only show arrow if system changed
				if systemFrom != systemTo {
					locationHTML = fmt.Sprintf("%s → %s",
						html.EscapeString(systemFrom), html.EscapeString(systemTo))
				} else {
					// System didn't change, just show current system
					locationHTML = html.EscapeString(systemTo)
				}
			} else {
				// No location change, show current system
				if idx := strings.Index(d.Current.Location, " / "); idx != -1 {
					locationHTML = html.EscapeString(d.Current.Location[:idx])
				} else {
					locationHTML = html.EscapeString(d.Current.Location)
				}
			}

			// Storage cell (formerly Details, but without location and creditsSpent)
			var storage []string
			if prevDate == "" {
				storage = append(storage, fmt.Sprintf("Ship: %s (%s)", html.EscapeString(d.Current.ShipName), html.EscapeString(d.Current.ShipClass)))
				storage = append(storage, fmt.Sprintf("Ships: %d", d.Current.TotalShips))
			} else {
				for _, sc := range otherStatChanges {
					storage = append(storage, html.EscapeString(sc))
				}
				if d.ShipChanged != "" {
					storage = append(storage, "Ship: "+html.EscapeString(d.ShipChanged))
				}
				if d.StorageDelta != 0 {
					storage = append(storage, fmt.Sprintf("Storage: %+.0f units", d.StorageDelta))
				}
				// Add individual storage item changes
				for _, itemChange := range d.StorageItemChanges {
					storage = append(storage, html.EscapeString(itemChange))
				}
				// Note: Can't use spread here due to html.EscapeString requirement
				if d.ShipsDelta != 0 {
					storage = append(storage, fmt.Sprintf("Ships: %+d (now %d)", d.ShipsDelta, d.Current.TotalShips))
				}
			}
			storageHTML := buildListHTML(storage)

			// Add sparkle emoji for new agents
			agentIDDisplay := html.EscapeString(d.AgentID)
			if d.IsNew {
				agentIDDisplay = agentIDDisplay + " ✨"
			}

			fmt.Fprintf(&b, `<tr><td class="agent-name">%s<br><small>%s</small></td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`+"\n",
				agentIDDisplay, html.EscapeString(d.Username),
				credText, skillsHTML, locationHTML, storageHTML)
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
				totalCreds := d.Current.Credits + d.Current.StorageCredits
				b.WriteString(fmt.Sprintf("<li>%s (%.0f cr)</li>\n",
					html.EscapeString(d.AgentID), totalCreds))
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
