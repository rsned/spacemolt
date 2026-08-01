package ovdash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

// TestLoadAssetCoverageAbsentDBIsNotAnError pins the pre-deploy state: a
// dbPath that does not exist on disk yields no rows and no error, so the
// dashboard renders "not deployed" rather than logging noise.
func TestLoadAssetCoverageAbsentDBIsNotAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "does-not-exist.db")
	rows, err := LoadAssetCoverage(context.Background(), p, time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("LoadAssetCoverage: %v (an absent file must not be an error)", err)
	}
	if rows != nil {
		t.Errorf("rows = %v, want nil", rows)
	}
}

// TestLoadAssetCoverageBrokenDBPropagatesError pins the fix for the finding
// that a query failure against a file that DOES exist (corruption, a missing
// table, SQLITE_BUSY) must be reported to the caller, not swallowed into
// (nil, nil) — swallowing it makes the caller's "keep last-good" branch
// unreachable and instead wipes s.assetCoverage on every transient failure.
func TestLoadAssetCoverageBrokenDBPropagatesError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(p, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadAssetCoverage(context.Background(), p, time.Now(), time.Hour)
	if err == nil {
		t.Fatal("LoadAssetCoverage must return an error for a broken DB that exists on disk, got nil")
	}
	if rows != nil {
		t.Errorf("rows = %v, want nil alongside the error", rows)
	}
}

// TestLoadAssetCoverageHealthyDBReturnsRows pins that the happy path (an
// existing, valid ledger) is unaffected by the absent/broken distinction.
func TestLoadAssetCoverageHealthyDBReturnsRows(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "assets.db")
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	st, err := assets.Open(assets.Config{DBPath: p})
	if err != nil {
		t.Fatalf("assets.Open: %v", err)
	}
	if err := st.UpsertIdentity(context.Background(), assets.Identity{
		PlayerID: "abc123", AgentID: "engineer-3", Username: "Artificer",
	}, now); err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	if err := st.UpsertProfile(context.Background(), assets.Profile{
		PlayerID: "abc123", Username: "Artificer", CapturedAt: now,
	}); err != nil {
		t.Fatalf("UpsertProfile: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := LoadAssetCoverage(context.Background(), p, now, time.Hour)
	if err != nil {
		t.Fatalf("LoadAssetCoverage: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("rows must not be empty for a healthy ledger with data")
	}
}
