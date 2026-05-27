# Faction Data Collection for Daily Summary

## Overview

Add faction-level data collection to the daily-summary tool. For each faction represented by agents, track faction treasury, facilities, and storage across stations.

## Requirements

- **Factions**: Multiple agents can be in same faction (e.g., CRFT, XPLR)
- **Founder priority**: Lowest-numbered agent in faction (e.g., craftsman-1) has most permissions
- **Station-specific storage**: Faction storage varies by station (lockbox, vault, warehouse, etc.)
- **Data re-use**: Faction storage shares data shape with personal storage

## Key Commands

1. `client.FactionInfo(ctx)` - Gets faction info (treasury, member count, etc.)
2. `client.Facility(ctx, map[string]any{"action": "faction_list"})` - Shows faction facilities at current station
3. Need to add: `ViewFactionStorageAt(ctx, stationID)` - Shows faction storage at specific station (remote query as of v0.299.0)

## Existing Data Structures

```go
// From pkg/game/serverapi/responses.go

type FactionInfoResponse struct {
    ID             string                `json:"id"`
    Name           string                `json:"name"`
    Tag            string                `json:"tag"`
    LeaderID       string                `json:"leader_id"`
    LeaderUsername string                `json:"leader_username"`
    MemberCount    int                   `json:"member_count"`
    OwnedBases     int                   `json:"owned_bases"`
    Treasury       int                   `json:"treasury,omitempty"`
    // ... more fields
}

type FacilityListResponse struct {
    BaseID              string           `json:"base_id"`
    FactionFacilities   []map[string]any `json:"faction_facilities"`
    // FactionFacilities contains maps with keys like:
    // - facility_id, facility_type, category, level, status
    // - For storage: type contains "lockbox", "vault", "warehouse", "depot"
}

type ViewFactionStorageResponse struct {
    FactionID      string      `json:"faction_id"`
    FactionName    string      `json:"faction_name"`
    FactionTag     string      `json:"faction_tag"`
    BaseID         string      `json:"base_id"`
    Credits        int         `json:"credits"`
    Items          []CargoItem `json:"items"`
    RecentActivity []map[string]any `json:"recent_activity,omitempty"`
}
```

## Plan

### 1. Add Missing Game Client Method

First, add a method to query faction storage at a specific station to `pkg/game/client_commands.go`:

```go
// ViewFactionStorageAt views your faction's shared storage at a specific station.
// As of v0.299.0, you can query remotely with station_id as long as you're a faction member.
func (c *Client) ViewFactionStorageAt(ctx context.Context, stationID string) error {
    msg := protocol.Message{
        Type:      "view_faction_storage",
        Timestamp: time.Now().UnixMilli(),
        Payload:   map[string]any{"station_id": stationID},
    }
    h, err := c.Submit(ctx, msg, WithTimeout(SleepTick))
    if err == nil {
        _, err = h.Result(ctx)
    }
    return err
}
```

Also add to `pkg/game/interface.go` and `pkg/game/mcp_game_client_commands.go` for MCP transport.

### 2. New Data Structures in daily-summary/main.go

```go
// FactionFacility represents a faction-owned facility
type FactionFacility struct {
    FacilityID   string         `json:"facility_id"`
    FacilityType string         `json:"facility_type"`
    Category     string         `json:"category"`
    BaseID       string         `json:"base_id"`
    Level        int            `json:"level"`
    Status       string         `json:"status,omitempty"`
    Details      map[string]any `json:"details,omitempty"`
}

// FactionStorage represents faction storage at a station
type FactionStorage struct {
    BaseID    string         `json:"base_id"`
    Credits   float64        `json:"credits"`
    Items     []StorageEntry `json:"items"`
    ItemCount int            `json:"item_count"`
}

// FactionSnapshot holds faction data for a date
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

// Update AgentSnapshot to include faction data pointer
type AgentSnapshot struct {
    // ... existing fields ...
    FactionID       string           `json:"faction_id"`
    FactionRank     string           `json:"faction_rank"`
    // Include faction data if this is the founder/primary agent
    FactionData     *FactionSnapshot `json:"faction_data,omitempty"`
}
```

### 3. Database Schema Extensions

Add new tables to daily-summary DB:

```sql
-- Store faction snapshots separately from agent snapshots
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
```

### 4. Collection Strategy

#### Step 1: Build Faction Map During Collection

```go
// Track which faction agents are in and identify lowest-numbered agent per faction
type FactionCollector struct {
    AgentsByFaction   map[string][]string // faction_id -> agent_ids
    FounderCandidates map[string]string    // faction_id -> lowest-numbered agent_id
    ExistingFactions  map[string]bool      // factions we already have data for
}

func (fc *FactionCollector) AddAgent(agentID, factionID string) {
    if factionID == "" {
        return
    }
    fc.AgentsByFaction[factionID] = append(fc.AgentsByFaction[factionID], agentID)
    fc.sortAndSetFounder(factionID)
}

func (fc *FactionCollector) sortAndSetFounder(factionID string) {
    agents := fc.AgentsByFaction[factionID]
    // Sort by numeric suffix (extract number from agent-name-1)
    sort.Slice(agents, func(i, j int) bool {
        return extractAgentNumber(agents[i]) < extractAgentNumber(agents[j])
    })
    fc.FounderCandidates[factionID] = agents[0]
}

func extractAgentNumber(agentID string) int {
    parts := strings.Split(agentID, "-")
    if len(parts) > 1 {
        if num, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
            return num
        }
    }
    return 999999
}
```

#### Step 2: Collect Full Faction Data

For each agent during capture:

```go
func captureAgent(agentID string, kb knowledge.Base, fc *FactionCollector, logger *log.Logger) *AgentSnapshot {
    // ... existing capture code ...

    // Check if this agent is the faction founder/primary
    isFactionFounder := false
    if snap.FactionID != "" {
        if founderID, ok := fc.FounderCandidates[snap.FactionID]; ok && founderID == agentID {
            isFactionFounder = true
        }

        // Check if faction is newly seen (not in ExistingFactions)
        isNewFaction := !fc.ExistingFactions[snap.FactionID]

        // Collect faction data if founder OR newly seen faction
        if isFactionFounder || isNewFaction {
            if factionData := captureFactionData(ctx, client, snap.FactionID, agentID, logger); factionData != nil {
                snap.FactionData = factionData
                logger.Printf("  Faction %s: Treasury=%.0f, Members=%d, Facilities=%d, Stations=%d",
                    factionData.FactionTag, factionData.Treasury, factionData.MemberCount,
                    len(factionData.Facilities), len(factionData.StorageStations))
                fc.ExistingFactions[snap.FactionID] = true
            }
        }
    }

    return snap
}

func captureFactionData(ctx context.Context, client game.GameClient, factionID, agentID string, logger *log.Logger) *FactionSnapshot {
    fs := &FactionSnapshot{
        FactionID:      factionID,
        FounderAgentID: agentID,
        CapturedAt:     time.Now(),
    }

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
    // Note: This only shows facilities at the agent's current station
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

func isStorageFacility(facilityType string) bool {
    storageTypes := []string{"lockbox", "vault", "warehouse", "depot"}
    for _, st := range storageTypes {
        if strings.Contains(strings.ToLower(facilityType), st) {
            return true
        }
    }
    return false
}

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
```

### 5. Storage Functions

```go
// Save faction snapshot to database
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

// Load faction snapshots for a date
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

// Check if faction has previous data
func factionHasPreviousData(db *sql.DB, factionID string) (bool, error) {
    var count int
    err := db.QueryRow(
        `SELECT COUNT(*) FROM faction_snapshots WHERE faction_id = ?`, factionID,
    ).Scan(&count)
    return count > 0, err
}

// Get all factions that have data in the database
func getExistingFactions(db *sql.DB) ([]string, error) {
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
```

### 6. Diff Calculations

```go
// FactionDiff holds faction-level differences
type FactionDiff struct {
    FactionID          string
    FactionName        string
    FactionTag         string
    TreasuryDelta      float64
    MemberCountDelta   int
    OwnedBasesDelta    int
    FacilityChanges    []string
    StorageCreditDeltas map[string]float64  // base_id -> storage credit delta
    StorageItemChanges []string
    HasChanges         bool
    IsNew              bool
    Current            *FactionSnapshot
}

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
            // Faction dissolved or no longer has members
            diff.FactionName = old.FactionName
            diff.FactionTag = old.FactionTag
            diff.HasChanges = true
            diffs = append(diffs, diff)
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
        diff.FacilityChanges = diffFacilities(old.Facilities, cur.Facilities)
        if len(diff.FacilityChanges) > 0 {
            diff.HasChanges = true
        }

        // Storage changes
        diff.StorageCreditDeltas, diff.StorageItemChanges = diffFactionStorage(old.StorageStations, cur.StorageStations)
        if len(diff.StorageCreditDeltas) > 0 || len(diff.StorageItemChanges) > 0 {
            diff.HasChanges = true
        }

        diffs = append(diffs, diffs)
    }

    sort.Slice(diffs, func(i, j int) bool {
        return diffs[i].FactionTag < diffs[j].FactionTag
    })

    return diffs
}

func diffFacilities(old, cur []FactionFacility) []string {
    var changes []string
    oldMap := make(map[string]FactionFacility)
    for _, f := range old {
        key := f.FacilityID
        if key == "" {
            key = f.BaseID + ":" + f.FacilityType
        }
        oldMap[key] = f
    }

    curMap := make(map[string]FactionFacility)
    for _, f := range cur {
        key := f.FacilityID
        if key == "" {
            key = f.BaseID + ":" + f.FacilityType
        }
        curMap[key] = f
    }

    // Check for new facilities
    for key, f := range curMap {
        if _, exists := oldMap[key]; !exists {
            changes = append(changes, fmt.Sprintf("NEW: %s at %s (Lvl %d)", f.FacilityType, f.BaseID, f.Level))
        }
    }

    // Check for removed facilities
    for key, f := range oldMap {
        if _, exists := curMap[key]; !exists {
            changes = append(changes, fmt.Sprintf("REMOVED: %s at %s", f.FacilityType, f.BaseID))
        }
    }

    // Check for level changes
    for key, f := range curMap {
        if oldF, exists := oldMap[key]; exists && f.Level != oldF.Level {
            changes = append(changes, fmt.Sprintf("%s at %s: Lvl %d -> %d", f.FacilityType, f.BaseID, oldF.Level, f.Level))
        }
    }

    return changes
}

func diffFactionStorage(old, cur []FactionStorage) (map[string]float64, []string) {
    creditDeltas := make(map[string]float64)
    var itemChanges []string

    oldMap := make(map[string]FactionStorage)
    for _, s := range old {
        oldMap[s.BaseID] = s
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

    return creditDeltas, itemChanges
}

func findItemQuantity(items []StorageEntry, itemID string) float64 {
    for _, i := range items {
        if i.ItemID == itemID {
            return i.Quantity
        }
    }
    return 0
}
```

### 7. Report Generation

#### Markdown Report - Add Faction Section

```go
func writeMarkdownReport(path, today, prevDate string, diffs []AgentDiff, factionDiffs []FactionDiff) error {
    // ... existing code ...

    // Add Faction Summary section before agent details
    if len(factionDiffs) > 0 {
        b.WriteString("## Faction Summary\n\n")
        b.WriteString("| Tag | Name | Treasury | Members | Bases | Changes |\n")
        b.WriteString("|-----|------|----------|---------|-------|---------|\n")

        for _, fd := range factionDiffs {
            var treasury string
            if prevDate == "" {
                treasury = fmt.Sprintf("%.0f", fd.Current.Treasury)
            } else {
                treasury = formatCredits(fd.TreasuryDelta)
            }

            var memberStr string
            if prevDate == "" {
                memberStr = fmt.Sprintf("%d", fd.Current.MemberCount)
            } else if fd.MemberCountDelta != 0 {
                memberStr = fmt.Sprintf("%+d (now %d)", fd.MemberCountDelta, fd.Current.MemberCount)
            } else {
                memberStr = fmt.Sprintf("%d", fd.Current.MemberCount)
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

            fmt.Fprintf(&b, "| **%s** | %s | %s | %s | %d | %s |\n",
                html.EscapeString(fd.FactionTag), html.EscapeString(fd.FactionName),
                treasury, memberStr, fd.Current.OwnedBases, html.EscapeString(changeStr))
        }
        b.WriteString("\n")
    }

    // ... rest of existing report ...
}
```

#### HTML Report - Add Faction Summary Section

Add after the summary bar and before the changes table:

```html
<!-- Add to summary bar -->
<h2>Faction Summary</h2>
<div class="faction-summary">
{{range .FactionDiffs}}
  <div class="faction-card">
    <div class="faction-header">
      <span class="faction-tag">{{.FactionTag}}</span>
      <span class="faction-name">{{.FactionName}}</span>
      {{if .IsNew}}<span class="new-badge">NEW</span>{{end}}
    </div>
    <div class="faction-stats">
      <div class="faction-stat">
        <div class="label">Treasury</div>
        <div class="value {{formatCreditsClass .TreasuryDelta}}">{{formatCredits .TreasuryDelta}}</div>
      </div>
      <div class="faction-stat">
        <div class="label">Members</div>
        <div class="value">{{.MemberCountDelta}} ({{.Current.MemberCount}} total)</div>
      </div>
      <div class="faction-stat">
        <div class="label">Bases</div>
        <div class="value">{{.Current.OwnedBases}}</div>
      </div>
    </div>
    {{if len .FacilityChanges}}
    <details>
      <summary>Facilities ({{len .FacilityChanges}} changes)</summary>
      <ul class="change-list faction-changes">
        {{range .FacilityChanges}}
        <li>{{.}}</li>
        {{end}}
      </ul>
    </details>
    {{end}}
    {{if len .StorageItemChanges}}
    <details>
      <summary>Storage ({{len .StorageItemChanges}} changes)</summary>
      <ul class="change-list faction-changes">
        {{range .StorageItemChanges}}
        <li>{{.}}</li>
        {{end}}
      </ul>
    </details>
    {{end}}
  </div>
{{end}}
</div>
```

Add corresponding CSS:

```css
.faction-summary {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(400px, 1fr));
  gap: 1rem;
  margin-bottom: 2rem;
}
.faction-card {
  background: var(--smui-surface-1);
  border: 1px solid var(--smui-surface-2);
  padding: 1rem;
  border-radius: 4px;
}
.faction-header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 1rem;
  padding-bottom: 0.5rem;
  border-bottom: 1px solid var(--smui-surface-2);
}
.faction-tag {
  color: var(--smui-aurora-green);
  font-weight: 600;
}
.faction-name {
  color: var(--smui-text-secondary);
}
.faction-stats {
  display: flex;
  gap: 1rem;
  margin-bottom: 0.5rem;
}
.faction-stat {
  flex: 1;
}
.faction-stat .label {
  color: var(--smui-text-muted);
  font-size: 0.7rem;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}
.faction-stat .value {
  color: var(--smui-text-primary);
  font-size: 1.1rem;
  font-weight: 600;
}
.new-badge {
  background: var(--smui-aurora-green);
  color: var(--smui-surface-0);
  font-size: 0.65rem;
  padding: 0.1rem 0.4rem;
  border-radius: 2px;
  text-transform: uppercase;
}
.faction-changes {
  margin-top: 0.5rem;
  font-size: 0.8rem;
}
```

### 8. Main Flow Updates

```go
func main() {
    // ... existing setup ...

    // Initialize faction collector
    factionCollector := &FactionCollector{
        AgentsByFaction:   make(map[string][]string),
        FounderCandidates: make(map[string]string),
        ExistingFactions:  make(map[string]bool),
    }

    // Check existing factions from database
    existingFactions, _ := getExistingFactions(db)
    for _, fid := range existingFactions {
        factionCollector.ExistingFactions[fid] = true
    }

    // Pass faction collector to collectSnapshots
    if !*reportOnly {
        collectSnapshots(db, kb, agentList, *delay, today, logger, factionCollector)
    }

    // Load and compute faction diffs
    todayFactions, _ := loadFactionSnapshots(db, today)
    prevFactions, _ := loadFactionSnapshots(db, prevDate)
    factionDiffs := computeFactionDiffs(todayFactions, prevFactions)

    // Generate reports with faction data
    writeMarkdownReport(*outputPath+".md", today, prevDate, diffs, factionDiffs)
    writeHTMLReport(*outputPath+".html", today, prevDate, nextDate, diffs, factionDiffs)
}

// Update collectSnapshots signature
func collectSnapshots(db *sql.DB, kb knowledge.Base, agentList []string, delaySec int, today string, logger *log.Logger, fc *FactionCollector) {
    // Track all agents for faction mapping
    for _, agentID := range agentList {
        fc.AddAgent(agentID, "") // faction_id will be set during capture
    }

    for i, agentID := range agentList {
        // ... existing delay logic ...

        snap := captureAgent(agentID, kb, fc, logger)
        if err := saveSnapshot(db, snap, today); err != nil {
            logger.Printf("  Failed to save snapshot: %v", err)
        }

        // Save faction data if captured
        if snap.FactionData != nil {
            if err := saveFactionSnapshot(db, snap.FactionData, today); err != nil {
                logger.Printf("  Failed to save faction snapshot: %v", err)
            }
        }

        // ... existing notAtStation logic ...
    }
}
```

### 9. Update regenerateAllReports

```go
func regenerateAllReports(db *sql.DB, outputPath string, logger *log.Logger) error {
    // ... existing code ...

    for i, date := range dates {
        // ... existing prev/next date logic ...

        // Load snapshots for this date
        snaps, err := loadSnapshots(db, date)
        // ... error handling ...

        var prevSnaps map[string]*AgentSnapshot
        // ... load prevSnaps ...

        // Load faction snapshots
        todayFactions, _ := loadFactionSnapshots(db, date)
        var prevFactions map[string]*FactionSnapshot
        if prevDate != "" {
            prevFactions, _ = loadFactionSnapshots(db, prevDate)
        }

        // Compute diffs
        diffs := computeDiffs(snaps, prevSnaps)
        factionDiffs := computeFactionDiffs(todayFactions, prevFactions)

        // Generate reports
        // ... existing report generation with factionDiffs ...
    }
}
```

## Implementation Order

1. **Step 1**: Add `ViewFactionStorageAt` method to game client
2. **Step 2**: Add new data structures (FactionSnapshot, FactionFacility, FactionStorage)
3. **Step 3**: Extend AgentSnapshot with FactionData field
4. **Step 4**: Add database schema migration for faction_snapshots table
5. **Step 5**: Implement FactionCollector for tracking agents by faction
6. **Step 6**: Implement captureFactionData function
7. **Step 7**: Implement storage/load functions for faction snapshots
8. **Step 8**: Implement diff computation for factions
9. **Step 9**: Update report generation (Markdown and HTML)
10. **Step 10**: Update main flow to collect and report faction data
11. **Step 11**: Test with real faction data

## Open Questions

1. **Facility discovery**: `facility action=faction_list` only shows facilities at the current station.
   - If the founder is at a station without faction facilities, we won't discover other stations' storage.
   - Future enhancement: Track known storage stations and query them even when agent is elsewhere.

2. **Failed collection handling**: What if founder collection fails but other members succeed?
   - Decision: Try next-lowest-numbered agent as fallback (can be implemented later).

3. **Report integration**: Faction data is a separate section. Agents could reference their faction.
   - Consider adding faction tag to agent card in HTML report.