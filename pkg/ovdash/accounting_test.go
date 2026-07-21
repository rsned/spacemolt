package ovdash

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func fixtureMarket(t *testing.T, now time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "market.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	in := now.Add(-time.Hour).Format(time.RFC3339)
	out := now.Add(-48 * time.Hour).Format(time.RFC3339)
	stmts := []string{
		`CREATE TABLE haul_results (realized_profit REAL, sold_at TEXT)`,
		`CREATE TABLE freight_results (carrier_payout REAL, fuel_cost REAL, outcome TEXT, finished_at TEXT)`,
		`CREATE TABLE mission_results (credits_earned REAL, item_cost REAL, fuel_cost REAL, finished_at TEXT)`,
		`INSERT INTO haul_results VALUES (1000, '` + in + `'), (9999, '` + out + `')`,
		`INSERT INTO freight_results VALUES (1402, 40, 'delivered', '` + in + `'),
			(500, 0, 'breached', '` + in + `'), (7777, 0, 'delivered', '` + out + `')`,
		`INSERT INTO mission_results VALUES (600, 100, 50, '` + in + `'), (8888, 0, 0, '` + out + `')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestLoadEarningsWindowsAndFilters(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	haul, freight, missions, err := LoadEarnings(context.Background(), fixtureMarket(t, now), now, 24*time.Hour)
	if err != nil {
		t.Fatalf("LoadEarnings: %v", err)
	}
	if haul.Total != 1000 || haul.Count != 1 {
		t.Fatalf("haul: %+v (48h-old row must be excluded)", haul)
	}
	if freight.Total != 1362 || freight.Count != 1 {
		t.Fatalf("freight: %+v (non-delivered and out-of-window excluded)", freight)
	}
	if missions.Total != 450 || missions.Count != 1 {
		t.Fatalf("missions: %+v (600-100-50)", missions)
	}
	if haul.PerHour != 1000.0/24.0 {
		t.Fatalf("per-hour = total/window hours, got %v", haul.PerHour)
	}
}

func TestBuildAccountingTotalsSnapshot(t *testing.T) {
	s := &Snapshot{
		Agents: []AgentState{
			{AgentID: "a", Credits: 100, Healthy: true, Seen: true},
			{AgentID: "b", Credits: 200, Healthy: false, Seen: true, Restarts: 3},
		},
		OffMap:      []AgentState{{AgentID: "c", Credits: 50, Healthy: true, Seen: false}},
		CapturedAt:  map[string]string{"haul": "2026-07-20T11:59:00Z", "craft": "2026-07-20T10:00:00Z"},
	}
	acct := BuildAccounting(s,
		SourceEarnings{Total: 240, PerHour: 10, Count: 2},
		SourceEarnings{Total: 24, PerHour: 1, Count: 1},
		SourceEarnings{Total: 48, PerHour: 2, Count: 4},
		24*time.Hour)
	if acct.TotalCredits != 350 || acct.Agents != 3 || acct.Healthy != 2 || acct.Unseen != 1 || acct.Restarts != 3 {
		t.Fatalf("counts wrong: %+v", acct)
	}
	if acct.CombinedPerHour != 13 {
		t.Fatalf("combined/hr: %v", acct.CombinedPerHour)
	}
	// Per-agent average divides by agents that are healthy AND seen.
	if acct.PerAgentPerHour != 13.0/1.0 {
		t.Fatalf("per-agent/hr: %v (only agent a is healthy+seen)", acct.PerAgentPerHour)
	}
	if acct.OldestCapture != "2026-07-20T10:00:00Z" {
		t.Fatalf("oldest capture: %q", acct.OldestCapture)
	}
}
