package knowledge

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestMissionTemplatesHasProceduralColumn(t *testing.T) {
	kb := newTestSQLiteKB(t)
	var n int
	if err := kb.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name='procedural'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("mission_templates.procedural column missing (got %d)", n)
	}
}

func TestEnsureMissionTemplatesProceduralCol_AddsToLegacy(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Legacy table shape: no `procedural` column.
	if _, err := db.Exec(`CREATE TABLE mission_templates (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}

	if err := ensureMissionTemplatesProceduralCol(db); err != nil {
		t.Fatalf("ensure (add): %v", err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name='procedural'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("procedural column not added (got %d)", n)
	}
	// Idempotent: second run is a no-op, not an error.
	if err := ensureMissionTemplatesProceduralCol(db); err != nil {
		t.Fatalf("ensure (idempotent): %v", err)
	}
}

func TestUpsertMissionTemplate_ProceduralCourier(t *testing.T) {
	kb := newTestSQLiteKB(t)
	entry := serverapi.MissionBoardEntry{
		MissionID:     "smuggling_courier_treasure_cache_trading_post_frontier_station_pirate_moonshine~57df1053f32e",
		Type:          "smuggling",
		Title:         "Border Job: Pirate Moonshine to Frontier Station",
		Rewards:       &serverapi.MissionRewards{Credits: 300, SkillXP: map[string]int{"smuggling": 50}},
		ProvidedItems: map[string]int{"pirate_moonshine": 5},
		Objectives: []serverapi.MissionObjective{{
			Type: "deliver_item", ItemID: "pirate_moonshine", Quantity: 5,
			TargetBaseID: "frontier_station", SystemID: "altais",
		}},
	}
	res, err := kb.UpsertMissionTemplate(context.Background(), entry, "treasure_cache_trading_post", "treasure_cache", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Inserted {
		t.Fatalf("expected insert, got %+v", res)
	}

	const wantID = "smuggling_courier_treasure_cache_trading_post_frontier_station_pirate_moonshine"
	var procedural int
	if err := kb.db.QueryRow(
		`SELECT procedural FROM mission_templates WHERE id = ?`, wantID,
	).Scan(&procedural); err != nil {
		t.Fatalf("synthetic-id row not found: %v", err)
	}
	if procedural != 1 {
		t.Fatalf("want procedural=1, got %d", procedural)
	}

	var locs int
	if err := kb.db.QueryRow(
		`SELECT COUNT(*) FROM mission_template_locations WHERE mission_id = ? AND base_id = ?`,
		wantID, "treasure_cache_trading_post",
	).Scan(&locs); err != nil {
		t.Fatal(err)
	}
	if locs != 1 {
		t.Fatalf("want 1 sighting row, got %d", locs)
	}
}
