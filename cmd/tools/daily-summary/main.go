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
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	_ "modernc.org/sqlite"
)

var (
	debug     = flag.Bool("debug", false, "Enable game client debug logging")
	transport = flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
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
	AgentID         string               `json:"agent_id"`
	Username        string               `json:"username"`
	Empire          string               `json:"empire"`
	Credits         float64              `json:"credits"`
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
	HomeBase        string               `json:"home_base,omitempty"`
	Fuel            float64              `json:"fuel"`
	MaxFuel         float64              `json:"max_fuel"`
	StatusRaw       json.RawMessage      `json:"status_raw,omitempty"`
	Error           string               `json:"error,omitempty"`
	CapturedAt      time.Time            `json:"captured_at"`
}

// AgentDiff holds the computed differences between two snapshots.
type AgentDiff struct {
	AgentID            string
	Username           string
	Empire             string
	CreditsDelta       float64
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
// FactionFacility represents a faction-owned facility.
type FactionFacility struct {
	FacilityID   string         `json:"facility_id"`
	FacilityType string         `json:"facility_type"`
	Category     string         `json:"category"`
	BaseID       string         `json:"base_id"`
	Level        int            `json:"level"`
	Status       string         `json:"status,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

// FactionStorage represents faction storage at a station.
type FactionStorage struct {
	BaseID    string         `json:"base_id"`
	Credits   float64        `json:"credits"`
	Items     []StorageEntry `json:"items"`
	ItemCount int            `json:"item_count"`
}

// FactionSnapshot holds faction data for a date.
type FactionSnapshot struct {
	FactionID       string            `json:"faction_id"`
	FactionName     string            `json:"faction_name"`
	FactionTag      string            `json:"faction_tag"`
	Treasury        float64           `json:"treasury"`
	MemberCount     int               `json:"member_count"`
	OwnedBases      int               `json:"owned_bases"`
	Facilities      []FactionFacility `json:"facilities"`
	StorageStations []FactionStorage  `json:"storage_stations"`
	FounderAgentID  string            `json:"founder_agent_id"`
	CapturedAt      time.Time         `json:"captured_at"`
}

// FactionDiff holds faction-level differences.
type FactionDiff struct {
	FactionID          string
	FactionName        string
	FactionTag         string
	TreasuryDelta      float64
	MemberCountDelta   int
	OwnedBasesDelta    int
	FacilityChanges    []string
	StorageDeltas      map[string]float64
	StorageItemChanges []string
	HasChanges         bool
	IsNew              bool
	Current            *FactionSnapshot
}

// FactionCollector tracks agents by faction and identifies the founder agent.
type FactionCollector struct {
	AgentsByFaction   map[string][]string
	FounderCandidates map[string]string
	ExistingFactions  map[string]bool
}

// NewFactionCollector creates a new faction collector.
func NewFactionCollector(existingFactions []string) *FactionCollector {
	fc := &FactionCollector{
		AgentsByFaction:   make(map[string][]string),
		FounderCandidates: make(map[string]string),
		ExistingFactions:  make(map[string]bool),
	}
	for _, fid := range existingFactions {
		fc.ExistingFactions[fid] = true
	}
	return fc
}

// AddAgent adds an agent to the faction map and updates founder candidate.
func (fc *FactionCollector) AddAgent(agentID, factionID string) {
	if factionID == "" {
		return
	}
	fc.AgentsByFaction[factionID] = append(fc.AgentsByFaction[factionID], agentID)
	fc.sortAndSetFounder(factionID)
}

// extractAgentNumber extracts the numeric suffix from an agent ID (e.g., "craftsman-7" -> 7).
func extractAgentNumber(agentID string) int {
	parts := strings.Split(agentID, "-")
	if len(parts) > 1 {
		if num, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return num
		}
	}
	return 999999
}

// sortAndSetFounder sorts agents by number suffix and sets lowest as founder.
func (fc *FactionCollector) sortAndSetFounder(factionID string) {
	agents := fc.AgentsByFaction[factionID]
	slices.SortFunc(agents, func(a, b string) int {
		na := extractAgentNumber(a)
		nb := extractAgentNumber(b)
		if na < nb {
			return -1
		} else if na > nb {
			return 1
		}
		return strings.Compare(a, b)
	})
	if len(agents) > 0 {
		fc.FounderCandidates[factionID] = agents[0]
	}
}

// IsFounder returns true if the agent is the founder candidate for their faction.
func (fc *FactionCollector) IsFounder(agentID, factionID string) bool {
	if founder, ok := fc.FounderCandidates[factionID]; ok {
		return founder == agentID
	}
	return false
}

// IsNewFaction returns true if the faction hasn't been seen before in existing data.
func (fc *FactionCollector) IsNewFaction(factionID string) bool {
	return !fc.ExistingFactions[factionID]
}

// GetExistingFactions returns all faction IDs from the database.
func GetExistingFactions(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT faction_id FROM faction_snapshots`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var factions []string
	for rows.Next() {
		var fid string
		if err := rows.Scan(&fid); err != nil {
			return nil, err
		}
		factions = append(factions, fid)
	}
	return factions, rows.Err()
}

// saveFactionSnapshot persists a faction snapshot to the database.
func saveFactionSnapshot(db *sql.DB, fs *FactionSnapshot, today string) error {
	jsonData, err := json.Marshal(fs)
	if err != nil {
		return fmt.Errorf("marshaling faction snapshot: %w", err)
	}
	_, err = db.Exec(
		`INSERT OR REPLACE INTO faction_snapshots (faction_id, captured_date, founder_agent_id, snapshot_json)
		 VALUES (?, ?, ?, ?)`,
		fs.FactionID, today, fs.FounderAgentID, string(jsonData),
	)
	return err
}

// loadFactionSnapshots loads all faction snapshots for a given date.
func loadFactionSnapshots(db *sql.DB, date string) (map[string]*FactionSnapshot, error) {
	rows, err := db.Query(
		`SELECT faction_id, snapshot_json FROM faction_snapshots WHERE captured_date = ?`, date,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	snaps := make(map[string]*FactionSnapshot)
	for rows.Next() {
		var factionID, jsonStr string
		if err := rows.Scan(&factionID, &jsonStr); err != nil {
			return nil, err
		}
		var snap FactionSnapshot
		if err := json.Unmarshal([]byte(jsonStr), &snap); err != nil {
			return nil, fmt.Errorf("unmarshaling faction snapshot for %s: %w", factionID, err)
		}
		snaps[factionID] = &snap
	}
	return snaps, rows.Err()
}

// captureAgentFactionData connects to an agent and captures their faction data.
// This should only be called for the founder agent of a faction.
func captureAgentFactionData(agentID, factionID string, logger *log.Logger) *FactionSnapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fs := &FactionSnapshot{
		FactionID:      factionID,
		FounderAgentID: agentID,
		CapturedAt:     time.Now(),
	}

	var client game.GameClient
	var initErr error
	switch *transport {
	case "mcp":
		client, _, initErr = game.InitializeMCPAgent(agentID, logger, ctx, *debug, false)
	case "ws":
		client, _, initErr = game.InitializeAgent(agentID, logger, ctx, *debug)
	default:
		logger.Printf("  Unknown transport: %s", *transport)
		return nil
	}
	if initErr != nil {
		logger.Printf("  Warning: Failed to connect to %s for faction data: %v", agentID, initErr)
		return nil
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			logger.Printf("  Warning: close error for %s: %v", agentID, cerr)
		}
	}()

	// 1. Get basic faction info (treasury, member count, etc.)
	if err := client.FactionInfo(ctx); err != nil {
		logger.Printf("  Warning: Failed to get faction info: %v", err)
		return nil
	}
	time.Sleep(game.SleepQuick)

	rawInfo := client.GetRawJSON("faction_info")
	if rawInfo != nil {
		var info serverapi.FactionInfoResponse
		if err := json.Unmarshal(rawInfo, &info); err == nil {
			fs.FactionName = info.Name
			fs.FactionTag = info.Tag
			fs.Treasury = float64(info.Treasury)
			fs.MemberCount = info.MemberCount
			fs.OwnedBases = info.OwnedBases
		}
	}

	// 2. Get faction facilities at current station
	if err := client.Facility(ctx, map[string]any{"action": "faction_list"}); err != nil {
		logger.Printf("  Warning: Failed to get faction facilities: %v", err)
	} else {
		time.Sleep(game.SleepQuick)
		rawFacilities := client.GetRawJSON("facility")
		if rawFacilities != nil {
			var facilityResp serverapi.FacilityListResponse
			if err := json.Unmarshal(rawFacilities, &facilityResp); err == nil {
				for _, f := range facilityResp.FactionFacilities {
					ff := parseFacility(f)
					ff.BaseID = facilityResp.BaseID
					fs.Facilities = append(fs.Facilities, ff)
				}
			}
		}
	}

	// 3. For each faction facility that is storage, query storage contents
	storageStations := make(map[string]bool) // base_id -> already_collected
	for _, fac := range fs.Facilities {
		if isStorageFacility(fac.FacilityType) {
			if !storageStations[fac.BaseID] {
				storageStations[fac.BaseID] = true
				if storage := captureFactionStorage(ctx, client, fac.BaseID, logger); storage != nil {
					fs.StorageStations = append(fs.StorageStations, *storage)
				}
			}
		}
	}

	return fs
}

// isStorageFacility returns true if the facility type is a storage facility.
func isStorageFacility(facilityType string) bool {
	storageTypes := []string{"lockbox", "vault", "warehouse", "depot"}
	lowerType := strings.ToLower(facilityType)
	for _, st := range storageTypes {
		if strings.Contains(lowerType, st) {
			return true
		}
	}
	return false
}

// captureFactionStorage captures faction storage at a specific station.
func captureFactionStorage(ctx context.Context, client game.GameClient, stationID string, logger *log.Logger) *FactionStorage {
	// Use ViewFactionStorageAt to query remotely
	if err := client.ViewFactionStorageAt(ctx, stationID); err != nil {
		logger.Printf("  Warning: Failed to view faction storage at %s: %v", stationID, err)
		return nil
	}
	time.Sleep(game.SleepQuick)

	rawStorage := client.GetRawJSON("faction_storage")
	if rawStorage == nil {
		return nil
	}

	var storageResp serverapi.ViewFactionStorageResponse
	if err := json.Unmarshal(rawStorage, &storageResp); err != nil {
		logger.Printf("  Warning: Failed to parse faction storage: %v", err)
		return nil
	}

	fs := &FactionStorage{
		BaseID:  storageResp.BaseID,
		Credits: float64(storageResp.Credits),
	}

	for _, item := range storageResp.Items {
		fs.Items = append(fs.Items, StorageEntry{
			ItemID:   item.ItemID,
			Quantity: item.Quantity,
		})
		fs.ItemCount++
	}

	return fs
}

// parseFacility parses a facility map into a FactionFacility struct.
func parseFacility(f map[string]any) FactionFacility {
	ff := FactionFacility{Details: f}

	if id, ok := f["facility_id"].(string); ok {
		ff.FacilityID = id
	}
	if ft, ok := f["facility_type"].(string); ok {
		ff.FacilityType = ft
	}
	if cat, ok := f["category"].(string); ok {
		ff.Category = cat
	}
	if level, ok := f["level"].(float64); ok {
		ff.Level = int(level)
	} else if level, ok := f["level"].(int); ok {
		ff.Level = level
	}

	return ff
}

// computeFactionDiffs computes the differences between today's and previous faction snapshots.
func computeFactionDiffs(today, prev map[string]*FactionSnapshot) []FactionDiff {
	var diffs []FactionDiff

	// Get all unique faction IDs
	factionIDs := make(map[string]bool)
	for id := range today {
		factionIDs[id] = true
	}
	for id := range prev {
		factionIDs[id] = true
	}

	for id := range factionIDs {
		diff := FactionDiff{FactionID: id}

		cur, hasCur := today[id]
		old, hasPrev := prev[id]

		if !hasCur && hasPrev {
			diff.FactionName = old.FactionName
			diff.FactionTag = old.FactionTag
			diff.HasChanges = true
			diffs = append(diffs, diff)
			continue
		}

		if !hasCur {
			continue
		}

		diff.Current = cur
		diff.FactionName = cur.FactionName
		diff.FactionTag = cur.FactionTag

		if !hasPrev {
			diff.IsNew = true
			diff.HasChanges = true
			diffs = append(diffs, diff)
			continue
		}

		// Compute deltas
		diff.TreasuryDelta = cur.Treasury - old.Treasury
		if math.Abs(diff.TreasuryDelta) >= 1 {
			diff.HasChanges = true
		}

		diff.MemberCountDelta = cur.MemberCount - old.MemberCount
		if diff.MemberCountDelta != 0 {
			diff.HasChanges = true
		}

		diff.OwnedBasesDelta = cur.OwnedBases - old.OwnedBases
		if diff.OwnedBasesDelta != 0 {
			diff.HasChanges = true
		}

		// Facility changes
		oldFacilities := facilitiesMap(old.Facilities)
		curFacilities := facilitiesMap(cur.Facilities)
		diff.FacilityChanges = diffFacilities(oldFacilities, curFacilities)
		if len(diff.FacilityChanges) > 0 {
			diff.HasChanges = true
		}

		// Storage changes per station
		diff.StorageDeltas, diff.StorageItemChanges = diffFactionStorage(old.StorageStations, cur.StorageStations)
		if len(diff.StorageDeltas) > 0 || len(diff.StorageItemChanges) > 0 {
			diff.HasChanges = true
		}

		diffs = append(diffs, diff)
	}

	slices.SortFunc(diffs, func(a, b FactionDiff) int {
		return strings.Compare(a.FactionTag, b.FactionTag)
	})

	return diffs
}

// facilitiesMap converts a facility slice to a map keyed by BaseID:FacilityType.
func facilitiesMap(facilities []FactionFacility) map[string]FactionFacility {
	m := make(map[string]FactionFacility)
	for _, f := range facilities {
		key := f.BaseID + ":" + f.FacilityType
		m[key] = f
	}
	return m
}

// diffFacilities compares two facility maps and returns human-readable changes.
func diffFacilities(old, cur map[string]FactionFacility) []string {
	var changes []string

	// Check for new facilities
	for id, f := range cur {
		if _, exists := old[id]; !exists {
			changes = append(changes, fmt.Sprintf("NEW: %s at %s (Lvl %d)", f.FacilityType, f.BaseID, f.Level))
		}
	}

	// Check for removed facilities
	for id, f := range old {
		if _, exists := cur[id]; !exists {
			changes = append(changes, fmt.Sprintf("REMOVED: %s at %s", f.FacilityType, f.BaseID))
		}
	}

	// Check for level changes
	for id, f := range cur {
		if oldF, exists := old[id]; exists {
			if f.Level != oldF.Level {
				changes = append(changes, fmt.Sprintf("%s at %s: Lvl %d -> %d", f.FacilityType, f.BaseID, oldF.Level, f.Level))
			}
		}
	}

	slices.Sort(changes)
	return changes
}

// diffFactionStorage compares two faction storage lists and returns credit deltas and item changes.
func diffFactionStorage(old, cur []FactionStorage) (map[string]float64, []string) {
	creditDeltas := make(map[string]float64)
	var itemChanges []string

	oldMap := make(map[string]FactionStorage)
	for _, s := range old {
		oldMap[s.BaseID] = s
	}

	curMap := make(map[string]FactionStorage)
	for _, s := range cur {
		curMap[s.BaseID] = s
	}

	for _, s := range cur {
		oldS, exists := oldMap[s.BaseID]
		if !exists {
			creditDeltas[s.BaseID] = s.Credits
			itemChanges = append(itemChanges, fmt.Sprintf("NEW storage at %s: %.0f credits", s.BaseID, s.Credits))
		} else {
			delta := s.Credits - oldS.Credits
			if math.Abs(delta) >= 1 {
				creditDeltas[s.BaseID] = delta
			}
			// Item-level changes
			for _, item := range s.Items {
				oldQty := findItemQuantity(oldS.Items, item.ItemID)
				qtyDelta := item.Quantity - oldQty
				if math.Abs(qtyDelta) >= 0.01 {
					itemChanges = append(itemChanges, fmt.Sprintf("%s @ %s: %+.1f", item.ItemID, s.BaseID, qtyDelta))
				}
			}
		}
	}

	slices.Sort(itemChanges)

	// Limit storage item changes
	if len(itemChanges) > 15 {
		itemChanges = itemChanges[:15]
		itemChanges = append(itemChanges, fmt.Sprintf("... and %d more", len(itemChanges)-15))
	}

	return creditDeltas, itemChanges
}

// findItemQuantity finds the quantity of an item in a storage list.
func findItemQuantity(items []StorageEntry, itemID string) float64 {
	for _, i := range items {
		if i.ItemID == itemID {
			return i.Quantity
		}
	}
	return 0
}

func main() {
	dbPath := flag.String("db", "data/daily-summary.db", "SQLite database path")
	kbPath := flag.String("kb", "data/spacemolt-knowledge.db", "Shared knowledge base SQLite path (for storage snapshots)")
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

	// Open shared knowledge base for storage snapshot capture
	var kb knowledge.Base
	if *kbPath != "" {
		sqliteKB, kbErr := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *kbPath, WAL: true})
		if kbErr != nil {
			logger.Printf("Warning: Failed to open knowledge base at %s: %v (storage snapshots will not be saved)", *kbPath, kbErr)
		} else {
			kb = sqliteKB
			defer func() { _ = kb.Close() }()
		}
	}

	// Collect data unless report-only mode
	if !*reportOnly {
		collectSnapshots(db, kb, agentList, *delay, today, logger)
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

	// Load and compute faction diffs
	todayFactions, err := loadFactionSnapshots(db, today)
	if err != nil {
		logger.Printf("Warning: failed to load today's faction snapshots: %v", err)
		todayFactions = make(map[string]*FactionSnapshot)
	}
	var prevFactions map[string]*FactionSnapshot
	if prevDate != "" {
		prevFactions, err = loadFactionSnapshots(db, prevDate)
		if err != nil {
			logger.Printf("Warning: failed to load previous faction snapshots: %v", err)
			prevFactions = make(map[string]*FactionSnapshot)
		}
	} else {
		prevFactions = make(map[string]*FactionSnapshot)
	}
	factionDiffs := computeFactionDiffs(todayFactions, prevFactions)

	// Generate reports
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		logger.Fatalf("Failed to create report directory: %v", err)
	}

	if err := writeMarkdownReport(*outputPath+".md", today, prevDate, diffs, factionDiffs); err != nil {
		logger.Fatalf("Failed to write markdown report: %v", err)
	}
	logger.Printf("Markdown report: %s.md", *outputPath)

	if err := writeHTMLReport(*outputPath+".html", today, prevDate, nextDate, diffs, factionDiffs); err != nil {
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
			if err := writeHTMLReport(prevOutputPath+".html", prevDate, prevPrevDate, today, prevDiffs, nil); err != nil {
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
		CREATE TABLE IF NOT EXISTS faction_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			faction_id TEXT NOT NULL,
			captured_date TEXT NOT NULL,
			founder_agent_id TEXT NOT NULL,
			snapshot_json TEXT NOT NULL,
			UNIQUE(faction_id, captured_date)
		);
		CREATE INDEX IF NOT EXISTS idx_faction_snapshots_date ON faction_snapshots(captured_date DESC);
		CREATE INDEX IF NOT EXISTS idx_faction_snapshots_faction ON faction_snapshots(faction_id, captured_date DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	return db, nil
}

// collectSnapshots connects to each agent, captures state, and saves to DB.
func collectSnapshots(db *sql.DB, kb knowledge.Base, agentList []string, delaySec int, today string, logger *log.Logger) {
	var notAtStation []string

	// Initialize faction collector
	existingFactions, _ := GetExistingFactions(db)
	fc := NewFactionCollector(existingFactions)

	for i, agentID := range agentList {
		if i > 0 {
			time.Sleep(time.Duration(delaySec) * time.Second)
		}
		logger.Printf("[%d/%d] Collecting %s...", i+1, len(agentList), agentID)
		snap := captureAgent(agentID, kb, logger)
		if err := saveSnapshot(db, snap, today); err != nil {
			logger.Printf("  Failed to save snapshot: %v", err)
		}

		// Track agents not docked at station or base (incomplete storage data)
		if snap.Error == "" && snap.POIType != "station" && snap.POIType != "base" {
			notAtStation = append(notAtStation, agentID)
		}

		// Collect faction data if agent is in a faction and is the founder or faction is new
		if snap.Error == "" && snap.FactionID != "" {
			fc.AddAgent(agentID, snap.FactionID)
			isFounder := fc.IsFounder(agentID, snap.FactionID)
			isNewFaction := fc.IsNewFaction(snap.FactionID)

			if isFounder || isNewFaction {
				logger.Printf("  Collecting faction data for %s...", snap.FactionID)
				factionSnap := captureAgentFactionData(agentID, snap.FactionID, logger)
				if factionSnap != nil {
					factionSnap.FounderAgentID = agentID
					if err := saveFactionSnapshot(db, factionSnap, today); err != nil {
						logger.Printf("  Warning: Failed to save faction snapshot: %v", err)
					} else {
						logger.Printf("  Faction [%s] %s: Treasury=%.0f, Members=%d, Facilities=%d, Storage Stations=%d",
							factionSnap.FactionTag, factionSnap.FactionName, factionSnap.Treasury, factionSnap.MemberCount,
							len(factionSnap.Facilities), len(factionSnap.StorageStations))
					}
				}
			}
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
func captureAgent(agentID string, kb knowledge.Base, logger *log.Logger) *AgentSnapshot {
	snap := &AgentSnapshot{
		AgentID:    agentID,
		CapturedAt: time.Now(),
		Skills:     make(map[string]SkillSnap),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var client game.GameClient
	var creds *game.Credentials
	var initErr error
	switch *transport {
	case "mcp":
		client, creds, initErr = game.InitializeMCPAgent(agentID, logger, ctx, *debug, false)
	case "ws":
		client, creds, initErr = game.InitializeAgent(agentID, logger, ctx, *debug)
	default:
		snap.Error = fmt.Sprintf("unknown transport: %s", *transport)
		return snap
	}
	if initErr != nil {
		snap.Error = fmt.Sprintf("init: %v", initErr)
		return snap
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			logger.Printf("  Warning: close error for %s: %v", agentID, cerr)
		}
	}()

	// Wire storage snapshot capture to shared knowledge base
	if kb != nil {
		if wsClient, ok := client.(*game.Client); ok {
			agent.WireStorageCapture(wsClient, kb, agentID, logger)
		}
	}

	// Refresh authoritative state via get_status before reading fields.
	// This ensures fuel, home_base, and lifetime stats are current even if
	// the login response was stale. Best-effort: fall back to login state on error.
	if err := client.GetStatus(ctx); err != nil {
		logger.Printf("  Warning: get_status failed: %v", err)
	} else {
		time.Sleep(game.SleepQuick)
	}

	// Extract state (refreshed by get_status above when successful)
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
	snap.Fuel = state.Ship.Fuel
	snap.MaxFuel = state.Ship.MaxFuel
	snap.Stats = state.Player.Stats
	snap.FactionID = state.Player.FactionID
	snap.FactionRank = state.Player.FactionRank
	snap.Experience = state.Player.Experience
	snap.HomeBase = state.Player.HomeBase

	// Capture the full raw get_status response for future-proofing.
	// Any field on Player/Ship/Modules/System/POI/Nearby is preserved verbatim.
	if rawStatus := client.GetRawJSON("status"); rawStatus != nil {
		snap.StatusRaw = json.RawMessage(rawStatus)
	}

	// Determine POI type
	for _, poi := range state.System.POIs {
		if poi.ID == state.CurrentPOI {
			snap.POIType = poi.Type
			break
		}
	}
	// MCP transport may not include POIs in initial state — fetch system data if needed
	if snap.POIType == "" && state.CurrentPOI != "" {
		if err := client.GetSystem(ctx); err == nil {
			time.Sleep(1 * time.Second)
			state = client.GetState()
			for _, poi := range state.System.POIs {
				if poi.ID == state.CurrentPOI {
					snap.POIType = poi.Type
					break
				}
			}
		}
	}

	// Save wallet credits to shared knowledge base
	if kb != nil {
		if err := kb.UpdateAgentWalletCredits(ctx, agentID, int(snap.Credits)); err != nil {
			logger.Printf("  Warning: failed to update wallet credits in KB: %v", err)
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

	// Best-effort: storage — resolve station ID from current POI (if at a station/base)
	// or fall back to home base for remote storage viewing.
	var storageStationID string
	if state.CurrentPOI != "" && (snap.POIType == "station" || snap.POIType == "base") {
		storageStationID = state.CurrentPOI
	}
	if storageStationID == "" {
		storageStationID = state.Player.HomeBase
	}
	if storageStationID != "" {
		logger.Printf("  Viewing storage at: %s (docked: %v)", storageStationID, state.Doc)
		if err := client.ViewStorageAt(ctx, storageStationID); err != nil {
			logger.Printf("  Warning: Failed to view storage: %v", err)
		} else {
			time.Sleep(game.SleepQuick)
		}

		// Parse storage data from raw JSON
		var rawJSON []byte
		if rj := client.GetRawJSON("storage"); rj != nil {
			rawJSON = rj
		}

		if rawJSON != nil {
			var storageResp struct {
				BaseID  string  `json:"base_id"`
				Credits float64 `json:"credits"`
				Items   []struct {
					ItemID   string  `json:"item_id"`
					Name     string  `json:"name"`
					Quantity float64 `json:"quantity"`
					Size     int     `json:"size"`
				} `json:"items"`
				Ships []struct {
					ShipID    string `json:"ship_id"`
					ClassID   string `json:"class_id"`
					ClassName string `json:"class_name"`
					CargoUsed int    `json:"cargo_used"`
				} `json:"ships"`
			}
			if json.Unmarshal(rawJSON, &storageResp) == nil {
				for _, item := range storageResp.Items {
					snap.StorageItems = append(snap.StorageItems, StorageEntry{
						ItemID:   item.ItemID,
						Quantity: item.Quantity,
					})
					snap.StorageTotal += item.Quantity
				}

				// Save storage snapshot to shared knowledge base
				if kb != nil && storageResp.BaseID != "" {
					kbItems := make([]knowledge.StorageSnapshotItem, len(storageResp.Items))
					for i, item := range storageResp.Items {
						kbItems[i] = knowledge.StorageSnapshotItem{
							ItemID: item.ItemID, Name: item.Name,
							Quantity: item.Quantity, Size: item.Size,
						}
					}
					kbShips := make([]knowledge.StorageSnapshotShip, len(storageResp.Ships))
					for i, ship := range storageResp.Ships {
						kbShips[i] = knowledge.StorageSnapshotShip{
							ShipID: ship.ShipID, ClassID: ship.ClassID,
							ClassName: ship.ClassName, CargoUsed: ship.CargoUsed,
						}
					}
					snapshot := knowledge.StorageSnapshot{
						AgentID: agentID, BaseID: storageResp.BaseID,
						Credits: int(storageResp.Credits),
						Items: kbItems, Ships: kbShips,
					}
					if err := kb.StoreStorageSnapshot(context.Background(), snapshot); err != nil {
						logger.Printf("  Warning: failed to save storage snapshot to KB: %v", err)
					}
				}
			} else {
				logger.Printf("  Warning: Failed to parse storage response")
			}
		} else {
			logger.Printf("  Warning: No storage data in response")
		}
	} else {
		logger.Printf("  No station POI or home base set, skipping storage check")
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

		// Load faction snapshots for this date and the prior date.
		todayFactions, err := loadFactionSnapshots(db, date)
		if err != nil {
			logger.Printf("Warning: failed to load faction snapshots for %s: %v", date, err)
			todayFactions = make(map[string]*FactionSnapshot)
		}
		var prevFactions map[string]*FactionSnapshot
		if prevDate != "" {
			prevFactions, err = loadFactionSnapshots(db, prevDate)
			if err != nil {
				logger.Printf("Warning: failed to load previous faction snapshots for %s: %v", date, err)
				prevFactions = make(map[string]*FactionSnapshot)
			}
		} else {
			prevFactions = make(map[string]*FactionSnapshot)
		}
		factionDiffs := computeFactionDiffs(todayFactions, prevFactions)

		// Generate reports
		dateOutputPath := filepath.Join(outputDir, "daily-summary-"+date)

		if err := writeMarkdownReport(dateOutputPath+".md", date, prevDate, diffs, factionDiffs); err != nil {
			logger.Printf("Warning: failed to write markdown report for %s: %v", date, err)
		} else {
			logger.Printf("  Markdown: %s.md", dateOutputPath)
		}

		if err := writeHTMLReport(dateOutputPath+".html", date, prevDate, nextDate, diffs, factionDiffs); err != nil {
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

		// Credits (server unified the wallet — no separate storage credits).
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
	checkI64 := func(name string, oldV, curV int64) {
		if d := curV - oldV; d != 0 {
			changes = append(changes, fmt.Sprintf("%s: %+d", name, d))
		}
	}

	check("ShipsDestroyed", old.ShipsDestroyed, cur.ShipsDestroyed)
	checkI64("OreMined", old.OreMined, cur.OreMined)
	checkI64("CreditsEarned", old.CreditsEarned, cur.CreditsEarned)
	checkI64("CreditsSpent", old.CreditsSpent, cur.CreditsSpent)
	check("TradesCompleted", old.TradesCompleted, cur.TradesCompleted)
	check("SystemsExplored", old.SystemsExplored, cur.SystemsExplored)
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
func writeMarkdownReport(path, today, prevDate string, diffs []AgentDiff, factionDiffs []FactionDiff) error {
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
		totalCredits += d.CreditsDelta
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

	// Faction summary
	if len(factionDiffs) > 0 {
		b.WriteString("## Faction Summary\n\n")
		b.WriteString("| Tag | Name | Treasury | Members | Bases | Changes |\n")
		b.WriteString("|-----|------|----------|---------|-------|---------|\n")
		for _, fd := range factionDiffs {
			treasury := "—"
			members := "—"
			bases := 0
			if fd.Current != nil {
				if prevDate == "" || fd.IsNew {
					treasury = formatCredits(fd.Current.Treasury)
				} else {
					treasury = formatCredits(fd.TreasuryDelta)
				}
				if fd.MemberCountDelta != 0 {
					members = fmt.Sprintf("%+d (now %d)", fd.MemberCountDelta, fd.Current.MemberCount)
				} else {
					members = fmt.Sprintf("%d", fd.Current.MemberCount)
				}
				bases = fd.Current.OwnedBases
			}
			var changes []string
			changes = append(changes, fd.FacilityChanges...)
			if fd.OwnedBasesDelta != 0 {
				changes = append(changes, fmt.Sprintf("Bases: %+d", fd.OwnedBasesDelta))
			}
			changes = append(changes, fd.StorageItemChanges...)
			changeStr := strings.Join(changes, "; ")
			if len(changeStr) > 100 {
				changeStr = changeStr[:97] + "..."
			}
			if fd.IsNew {
				changeStr = "NEW; " + changeStr
			}
			fmt.Fprintf(&b, "| **%s** | %s | %s | %s | %d | %s |\n",
				fd.FactionTag, fd.FactionName, treasury, members, bases, changeStr)
		}
		b.WriteString("\n")
	}

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
				creditsText := fmt.Sprintf("%.0f credits", d.Current.Credits)
				b.WriteString(fmt.Sprintf("- %s (%s) - %s at %s\n",
					d.AgentID, d.Username, creditsText, d.Current.Location))
			}
		}
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// writeHTMLReport generates a self-contained HTML report.
func writeHTMLReport(path, today, prevDate, nextDate string, diffs []AgentDiff, factionDiffs []FactionDiff) error {
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
		totalCreditsDelta += d.CreditsDelta
		totalAllCredits += d.Current.Credits
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

	// Faction Summary
	if len(factionDiffs) > 0 {
		b.WriteString("<h2>Faction Summary</h2>\n<table>\n")
		b.WriteString("<tr><th>Tag</th><th>Name</th><th>Treasury</th><th>Members</th><th>Bases</th><th>Changes</th></tr>\n")
		for _, fd := range factionDiffs {
			var treasury, members string
			bases := 0
			if fd.Current != nil {
				if prevDate == "" || fd.IsNew {
					treasury = fmt.Sprintf(`<span class="neutral">%s</span>`, formatNumber(fd.Current.Treasury))
				} else {
					trendCls := "neutral"
					if fd.TreasuryDelta > 0 {
						trendCls = "positive"
					} else if fd.TreasuryDelta < 0 {
						trendCls = "negative"
					}
					treasury = fmt.Sprintf(`<span class="%s">%s</span>`, trendCls, formatNumber(fd.TreasuryDelta))
				}
				if fd.MemberCountDelta != 0 {
					trendCls := "positive"
					if fd.MemberCountDelta < 0 {
						trendCls = "negative"
					}
					members = fmt.Sprintf(`<span class="%s">%+d</span> (now %d)`, trendCls, fd.MemberCountDelta, fd.Current.MemberCount)
				} else {
					members = fmt.Sprintf("%d", fd.Current.MemberCount)
				}
				bases = fd.Current.OwnedBases
			}
			var changes []string
			if fd.IsNew {
				changes = append(changes, `<span class="positive">NEW</span>`)
			}
			for _, fc := range fd.FacilityChanges {
				changes = append(changes, html.EscapeString(fc))
			}
			if fd.OwnedBasesDelta != 0 {
				trendCls := "positive"
				if fd.OwnedBasesDelta < 0 {
					trendCls = "negative"
				}
				changes = append(changes, fmt.Sprintf(`<span class="%s">Bases: %+d</span>`, trendCls, fd.OwnedBasesDelta))
			}
			for _, sc := range fd.StorageItemChanges {
				changes = append(changes, html.EscapeString(sc))
			}
			fmt.Fprintf(&b, `<tr><td><strong>%s</strong></td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%s</td></tr>`+"\n",
				html.EscapeString(fd.FactionTag), html.EscapeString(fd.FactionName),
				treasury, members, bases, strings.Join(changes, "; "))
		}
		b.WriteString("</table>\n")
	}

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

			// Credits cell: show wallet with trend arrow, then spent (red) and earned (green) below
			var credText string
			if prevDate == "" {
				// Baseline report: show current wallet with no trend arrow
				credText = fmt.Sprintf(`<span class="positive">%s</span>`, formatNumber(d.Current.Credits))
			} else {
				// Comparison report: show wallet with trend arrow
				trendArrow := "→"
				trendClass := "neutral"
				if d.CreditsDelta > 0 {
					trendArrow = "↗"
					trendClass = "positive"
				} else if d.CreditsDelta < 0 {
					trendArrow = "↘"
					trendClass = "negative"
				}
				credText = fmt.Sprintf(`<span class="positive">%s</span> <small class="%s">%s</small>`,
					formatNumber(d.Current.Credits), trendClass, trendArrow)
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
