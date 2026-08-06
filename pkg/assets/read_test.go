package assets

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadCarrierReportsMissingVsPresent pins the distinction the eligibility
// fallback depends on: "never captured" and "captured with zero debt" must not
// look the same. Conflating them is what made CarrierKnown's documented meaning
// false in slices 1-4.
func TestLoadCarrierReportsMissingVsPresent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if _, _, ok, err := st.LoadCarrier(ctx, "nobody"); err != nil || ok {
		t.Fatalf("LoadCarrier for an uncaptured agent = ok %v err %v, want ok=false err=nil", ok, err)
	}

	if err := st.UpsertCarrier(ctx, "abc123", Carrier{Tier: "licensed", OutstandingDebt: 0}, now); err != nil {
		t.Fatalf("UpsertCarrier: %v", err)
	}
	got, at, ok, err := st.LoadCarrier(ctx, "abc123")
	if err != nil || !ok {
		t.Fatalf("LoadCarrier after capture = ok %v err %v, want ok=true", ok, err)
	}
	if got.Tier != "licensed" {
		t.Errorf("Tier = %q, want licensed", got.Tier)
	}
	if !at.Equal(now) {
		t.Errorf("captured_at = %v, want %v", at, now)
	}
}

// TestLoadHullsResolvesBaseIDs pins that a caller-supplied resolver rewrites
// location_base_id into poi-id form. Without it a naive join against pois.id
// drops all five empire capitals and all seven pirate strongholds -- measured
// at 18 of craftsman-1's 20 hulls.
func TestLoadHullsResolvesBaseIDs(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	rows := []Hull{{ShipID: "s1", LocationBaseID: "confederacy_central_command"}}
	if err := st.ReplaceHulls(ctx, "abc123", rows, now); err != nil {
		t.Fatalf("ReplaceHulls: %v", err)
	}

	verbatim, _, _, err := st.LoadHulls(ctx, "abc123", nil)
	if err != nil {
		t.Fatalf("LoadHulls(nil resolver): %v", err)
	}
	if verbatim[0].LocationBaseID != "confederacy_central_command" {
		t.Errorf("nil resolver must leave the wire value alone, got %q", verbatim[0].LocationBaseID)
	}

	r := BaseResolver(func(b string) string {
		if b == "confederacy_central_command" {
			return "sol_central"
		}

		return b
	})
	resolved, _, _, err := st.LoadHulls(ctx, "abc123", r)
	if err != nil {
		t.Fatalf("LoadHulls(resolver): %v", err)
	}
	if resolved[0].LocationBaseID != "sol_central" {
		t.Errorf("LocationBaseID = %q, want sol_central", resolved[0].LocationBaseID)
	}
}

// TestNewBaseResolverMapsAliases pins that the resolver reads bases(id, poi_id)
// and leaves unknown ids alone. Live shape as of 2026-08-06: 43 rows, 15 where
// the two forms differ.
func TestNewBaseResolverMapsAliases(t *testing.T) {
	ctx := context.Background()
	db := openTestKnowledgeDB(t, [][2]string{
		{"confederacy_central_command", "sol_central"},
		{"central_nexus", "the_core"},
		{"nova_terra_central", ""}, // same-name base: poi_id empty
	})

	r, err := NewBaseResolver(ctx, db)
	if err != nil {
		t.Fatalf("NewBaseResolver: %v", err)
	}
	if got := r("confederacy_central_command"); got != "sol_central" {
		t.Errorf("capital alias = %q, want sol_central", got)
	}
	if got := r("nova_terra_central"); got != "nova_terra_central" {
		t.Errorf("empty poi_id must leave the id alone, got %q", got)
	}
	if got := r("not_a_base"); got != "not_a_base" {
		t.Errorf("unknown id must pass through, got %q", got)
	}
}

// openTestKnowledgeDB builds a throwaway db carrying only the bases columns the
// resolver reads.
func openTestKnowledgeDB(t *testing.T, rows [][2]string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE bases (id TEXT PRIMARY KEY, poi_id TEXT)`); err != nil {
		t.Fatalf("create bases: %v", err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO bases (id, poi_id) VALUES (?,?)`, r[0], r[1]); err != nil {
			t.Fatalf("insert base %s: %v", r[0], err)
		}
	}

	return db
}
