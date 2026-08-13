package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/ovdash"

	_ "modernc.org/sqlite"
)

// Test fixtures: reuse the shapes from pkg/ovdash tests by writing files
// directly (the fixture helpers there are package-private).
func writeFixtures(t *testing.T) (kb, market, statusDir string) {
	t.Helper()
	// Smallest possible KB: one system, no lanes.
	dir := t.TempDir()
	kb = filepath.Join(dir, "kb.db")
	mustExec(t, kb, []string{
		`CREATE TABLE systems (id TEXT PRIMARY KEY, name TEXT NOT NULL,
			position_x REAL NOT NULL, position_y REAL NOT NULL,
			police_level INTEGER DEFAULT 0, empire TEXT DEFAULT '',
			is_stronghold BOOLEAN DEFAULT 0, last_visited_tick INTEGER DEFAULT 0)`,
		`CREATE TABLE connections (from_system TEXT, to_system TEXT, distance REAL)`,
		`INSERT INTO systems VALUES ('sol','Sol',0,0,10,'solarian',0,1)`,
		`CREATE TABLE pois (id TEXT PRIMARY KEY, system_id TEXT NOT NULL,
			name TEXT NOT NULL, type TEXT NOT NULL, description TEXT,
			position_x REAL NOT NULL, position_y REAL NOT NULL,
			base_id TEXT, last_updated_tick INTEGER DEFAULT 0,
			class TEXT DEFAULT '', hidden BOOLEAN NOT NULL DEFAULT 0)`,
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y, class, hidden) VALUES
			('sol_star','sol','Sol','sun',0,0,'G2V',0),
			('earth','sol','Earth','planet',1,0,'terran',0)`,
	})
	market = filepath.Join(dir, "market.db")
	mustExec(t, market, []string{
		`CREATE TABLE haul_results (realized_profit REAL, sold_at TEXT)`,
		`CREATE TABLE freight_results (carrier_payout REAL, fuel_cost REAL, outcome TEXT, finished_at TEXT)`,
		`CREATE TABLE mission_results (credits_earned REAL, item_cost REAL, fuel_cost REAL, finished_at TEXT)`,
	})
	statusDir = filepath.Join(dir, "overmind")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(statusDir, "haul-status.json"), map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339),
		"workers": []map[string]any{{
			"agent_id": "hauler-0", "role": "hauler", "system": "Sol",
			"docked": true, "credits": 42.0, "healthy": true, "seen": true,
		}},
	})
	return kb, market, statusDir
}

func TestServerEndpoints(t *testing.T) {
	kb, market, statusDir := writeFixtures(t)
	srv, err := newServer(context.Background(), serverConfig{
		KBPath: kb, MarketPath: market, StatusDir: statusDir, DistDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	srv.refresh(context.Background(), time.Now()) // one manual loop turn

	ts := httptest.NewServer(srv.mux())
	defer ts.Close()

	var systems []map[string]any
	getJSON(t, ts.URL+"/api/overmind/systems", &systems)
	if len(systems) != 1 || systems[0]["id"] != "sol" {
		t.Fatalf("systems: %+v", systems)
	}

	var snap ovdash.Snapshot
	getJSON(t, ts.URL+"/api/overmind/agents", &snap)
	if len(snap.Agents) != 1 || snap.Agents[0].SystemID != "sol" || snap.Agents[0].Fleet != "haul" {
		t.Fatalf("agents: %+v", snap)
	}

	var acct ovdash.Accounting
	getJSON(t, ts.URL+"/api/overmind/accounting", &acct)
	if acct.TotalCredits != 42 || acct.Agents != 1 {
		t.Fatalf("accounting: %+v", acct)
	}
}

func mustExec(t *testing.T, dbPath string, stmts []string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close() //nolint:errcheck
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSystemPOIsEndpoint(t *testing.T) {
	kb, market, statusDir := writeFixtures(t)
	srv, err := newServer(context.Background(), serverConfig{
		KBPath: kb, MarketPath: market, StatusDir: statusDir, DistDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	ts := httptest.NewServer(srv.mux())
	defer ts.Close()

	var pois []map[string]any
	getJSON(t, ts.URL+"/api/overmind/system/sol/pois", &pois)
	if len(pois) != 2 || pois[0]["id"] != "sol_star" || pois[1]["class"] != "terran" {
		t.Fatalf("pois: %+v", pois)
	}

	resp, err := http.Get(ts.URL + "/api/overmind/system/atlantis/pois") //nolint:gosec,noctx
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown system: want 404, got %d", resp.StatusCode)
	}
}

func TestAdminRemoveEndpoint(t *testing.T) {
	kb, market, statusDir := writeFixtures(t)
	srv, err := newServer(context.Background(), serverConfig{
		KBPath: kb, MarketPath: market, StatusDir: statusDir, DistDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	ts := httptest.NewServer(srv.mux())
	defer ts.Close()

	// No haul socket listening in this fixture dir: degraded offline mode.
	resp, err := http.Post(ts.URL+"/api/overmind/fleets/haul/agents/hauler-0/remove", "application/json", nil) //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove: want 200, got %d", resp.StatusCode)
	}
	var res ovdash.AdminResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if res.Status != "recorded_offline" {
		t.Fatalf("status = %q, want recorded_offline", res.Status)
	}
	if _, err := os.Stat(filepath.Join(statusDir, "haul-overrides.json")); err != nil {
		t.Fatalf("overrides sidecar not created: %v", err)
	}

	// Unknown fleet label: 400.
	resp2, err := http.Post(ts.URL+"/api/overmind/fleets/nope/agents/hauler-0/remove", "application/json", nil) //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close() //nolint:errcheck
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown fleet: want 400, got %d", resp2.StatusCode)
	}
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec,noctx // test-only fixed localhost URL
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
