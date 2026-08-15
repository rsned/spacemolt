package ovdash

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

func rosterFixtures(t *testing.T) (assetsPath, kbPath string) {
	t.Helper()
	dir := t.TempDir()
	assetsPath = filepath.Join(dir, "assets.db")
	kbPath = filepath.Join(dir, "kb.db")

	cfg := assets.DefaultConfig()
	cfg.DBPath = assetsPath
	st, err := assets.Open(cfg)
	if err != nil {
		t.Fatalf("assets.Open: %v", err)
	}
	defer st.Close() //nolint:errcheck
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if err := st.UpsertIdentity(ctx, assets.Identity{PlayerID: "p1", AgentID: "trader-1", Username: "Tessa"}, now); err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := st.UpsertProfile(ctx, assets.Profile{PlayerID: "p1", Credits: 100, CapturedAt: now.Add(-30 * time.Hour)}); err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := st.ReplaceHulls(ctx, "p1", []assets.Hull{
		{ShipID: "s1", ClassID: "analysis", ClassName: "Analysis", IsActive: true, CargoUsed: 12},
	}, now); err != nil {
		t.Fatalf("hulls: %v", err)
	}

	kb, err := sql.Open(sqliteDriver, kbPath)
	if err != nil {
		t.Fatalf("kb open: %v", err)
	}
	defer kb.Close() //nolint:errcheck
	if _, err := kb.Exec(`CREATE TABLE ships (id TEXT PRIMARY KEY, cargo_capacity INTEGER);
		INSERT INTO ships VALUES ('analysis', 260)`); err != nil {
		t.Fatalf("kb seed: %v", err)
	}

	return assetsPath, kbPath
}

func TestLoadRosterDecoratesCargoAndStale(t *testing.T) {
	assetsPath, kbPath := rosterFixtures(t)
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	rows, err := LoadRoster(context.Background(), assetsPath, kbPath, now, 24*time.Hour)
	if err != nil {
		t.Fatalf("LoadRoster: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Ship == nil || r.Ship.CargoCapacity != 260 {
		t.Errorf("cargo capacity not decorated from catalog: %+v", r.Ship)
	}
	if !r.Stale {
		t.Errorf("a 30h-old capture must be stale at a 24h threshold")
	}

	// Absent ledger is the pre-deploy state: no rows, no error.
	none, err := LoadRoster(context.Background(), filepath.Join(t.TempDir(), "missing.db"), kbPath, now, time.Hour)
	if err != nil || none != nil {
		t.Errorf("missing ledger: rows=%v err=%v, want nil/nil", none, err)
	}
}

func TestLoadSheetDecoratesHulls(t *testing.T) {
	assetsPath, kbPath := rosterFixtures(t)
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	sheet, err := LoadSheet(context.Background(), assetsPath, kbPath, "trader-1", now, 24*time.Hour)
	if err != nil || sheet == nil {
		t.Fatalf("LoadSheet: sheet=%v err=%v", sheet, err)
	}
	if len(sheet.Hulls) != 1 || sheet.Hulls[0].CargoCapacity != 260 {
		t.Errorf("sheet hulls not decorated: %+v", sheet.Hulls)
	}
	if missing, err := LoadSheet(context.Background(), assetsPath, kbPath, "nobody", now, time.Hour); err != nil || missing != nil {
		t.Errorf("unknown agent: sheet=%v err=%v, want nil/nil", missing, err)
	}
}
