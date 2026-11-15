package knowledge

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func intelFixture() []serverapi.IntelEntry {
	return []serverapi.IntelEntry{{
		SystemID: "eventide_core", Name: "Eventide Core", PoliceLevel: 0,
		SubmittedBy: "p1", SubmitterName: "Ace", SubmittedAtTick: 1000, AutoSynced: true,
		Connections: []serverapi.IntelConnection{{SystemID: "moth_run", Name: "Moth Run", Distance: 12}},
		POIs: []serverapi.IntelPOI{
			{ID: "stolen_mass", Type: "planet", Name: "Stolen Mass", Position: serverapi.Position{X: 2, Y: 1},
				Hazard: &serverapi.POIHazard{Type: "tidal_shear", Severity: 4}, ReadAtTick: 990},
			{ID: "eventide_belt", Type: "asteroid_belt", Name: "Eventide Belt", Class: "metallic", ReadAtTick: 995,
				Resources: []serverapi.IntelResource{{ResourceID: "iron_ore", Richness: 40, Remaining: 900, MaxRemaining: 1000}}},
		},
	}}
}

// The faction's intel pool lands in its own tables for anyone to query, and
// each POI hazard is merged into pois so the sweep's hazard check sees it.
func TestStoreFactionIntel(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	if err := kb.StoreFactionIntel(ctx, "crft", intelFixture(), 1100, "2026-11-15T20:00:00Z"); err != nil {
		t.Fatal(err)
	}
	var name, submitter string
	var police int
	if err := kb.db.QueryRow(`SELECT name, police_level, submitter_name FROM faction_intel_systems WHERE faction_id='crft' AND system_id='eventide_core'`).
		Scan(&name, &police, &submitter); err != nil || name != "Eventide Core" || submitter != "Ace" {
		t.Fatalf("system row %q %d %q, %v", name, police, submitter, err)
	}
	var n int
	_ = kb.db.QueryRow(`SELECT COUNT(*) FROM faction_intel_pois WHERE faction_id='crft' AND system_id='eventide_core'`).Scan(&n)
	if n != 2 {
		t.Fatalf("%d intel POIs, want 2", n)
	}
	var rem int
	if err := kb.db.QueryRow(`SELECT remaining FROM faction_intel_resources WHERE faction_id='crft' AND poi_id='eventide_belt' AND resource_id='iron_ore'`).Scan(&rem); err != nil || rem != 900 {
		t.Fatalf("resource remaining %d, %v; want 900", rem, err)
	}
	_ = kb.db.QueryRow(`SELECT COUNT(*) FROM faction_intel_connections WHERE faction_id='crft' AND system_id='eventide_core' AND to_system_id='moth_run'`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d intel connections, want 1", n)
	}
	if typ, sev, _ := readHazard(t, kb, "stolen_mass"); typ != "tidal_shear" || sev != 4 {
		t.Fatalf("pois hazard %q/%d, want tidal_shear/4", typ, sev)
	}

	// A re-sync replaces the system's POIs and resources: a depleted deposit
	// that is gone from intel is gone from the table.
	e := intelFixture()
	e[0].POIs[1].Resources = nil
	e[0].SubmittedAtTick = 2000
	if err := kb.StoreFactionIntel(ctx, "crft", e, 2100, "2026-11-15T21:00:00Z"); err != nil {
		t.Fatal(err)
	}
	_ = kb.db.QueryRow(`SELECT COUNT(*) FROM faction_intel_resources WHERE faction_id='crft'`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d resources after re-sync, want 0", n)
	}
}
