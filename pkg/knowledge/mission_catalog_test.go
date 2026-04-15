package knowledge

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestDiffMissionRows_NoChange(t *testing.T) {
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Type:       "mining",
		Title:      "Iron Supply Run",
		Difficulty: 1,
	}
	row := missionRowFromEntry(entry)
	diffs := diffMissionRows(row, row)
	if len(diffs) != 0 {
		t.Fatalf("expected no diffs, got %+v", diffs)
	}
}

func TestDiffMissionRows_TitleChanged(t *testing.T) {
	a := missionRowFromEntry(serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	})
	b := missionRowFromEntry(serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Run (updated)",
	})
	diffs := diffMissionRows(a, b)
	if len(diffs) != 1 || diffs[0].Field != "title" {
		t.Fatalf("expected single title diff, got %+v", diffs)
	}
	if diffs[0].OldValue != "Iron Supply Run" || diffs[0].NewValue != "Iron Run (updated)" {
		t.Fatalf("unexpected diff values: %+v", diffs[0])
	}
}

func TestDiffMissionRows_ObjectivesChanged(t *testing.T) {
	a := missionRowFromEntry(serverapi.MissionBoardEntry{
		MissionID:  "m",
		TemplateID: "m",
		Title:      "t",
		Objectives: []serverapi.MissionObjective{
			{Type: "mine_resource", Description: "Mine 30 iron", ItemID: "iron_ore", Quantity: 30},
		},
	})
	b := missionRowFromEntry(serverapi.MissionBoardEntry{
		MissionID:  "m",
		TemplateID: "m",
		Title:      "t",
		Objectives: []serverapi.MissionObjective{
			{Type: "mine_resource", Description: "Mine 40 iron", ItemID: "iron_ore", Quantity: 40},
		},
	})
	diffs := diffMissionRows(a, b)
	if len(diffs) != 1 || diffs[0].Field != "objectives" {
		t.Fatalf("expected single objectives diff, got %+v", diffs)
	}
}

func TestMissionRowFromEntry_EncodesMaps(t *testing.T) {
	entry := serverapi.MissionBoardEntry{
		MissionID:  "m",
		TemplateID: "m",
		Title:      "t",
		Rewards: &serverapi.MissionRewards{
			Credits: 1000,
			SkillXP: map[string]int{"mining": 15},
			Items:   map[string]int{"fuel": 5},
		},
	}
	row := missionRowFromEntry(entry)
	if row.RewardsCredits != 1000 {
		t.Fatalf("credits: got %d", row.RewardsCredits)
	}
	var xp map[string]int
	if err := jsonUnmarshalString(row.RewardsSkillXP, &xp); err != nil {
		t.Fatalf("unmarshal xp: %v", err)
	}
	if !reflect.DeepEqual(xp, map[string]int{"mining": 15}) {
		t.Fatalf("xp mismatch: %+v", xp)
	}
}

func TestMemoryKB_UpsertMissionTemplate_Insert(t *testing.T) {
	kb := NewMemoryKB()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
		Type:       "mining",
	}
	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !res.Inserted || len(res.Diffs) != 0 {
		t.Fatalf("expected insert with no diffs, got %+v", res)
	}
}

func TestMemoryKB_UpsertMissionTemplate_UnchangedReinsert(t *testing.T) {
	kb := NewMemoryKB()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 200)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if res.Inserted {
		t.Fatalf("expected Inserted=false on second call")
	}
	if len(res.Diffs) != 0 {
		t.Fatalf("expected no diffs, got %+v", res.Diffs)
	}
}

func TestMemoryKB_UpsertMissionTemplate_ChangedTitle(t *testing.T) {
	kb := NewMemoryKB()
	ctx := t.Context()
	original := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, original, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	changed := original
	changed.Title = "Iron Run (updated)"
	res, err := kb.UpsertMissionTemplate(ctx, changed, "grand_exchange_station", "haven", 200)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if res.Inserted {
		t.Fatalf("expected Inserted=false")
	}
	if len(res.Diffs) != 1 || res.Diffs[0].Field != "title" {
		t.Fatalf("expected title diff, got %+v", res.Diffs)
	}
}

func TestMemoryKB_UpsertMissionTemplate_SecondLocation(t *testing.T) {
	kb := NewMemoryKB()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first: %v", err)
	}
	res, err := kb.UpsertMissionTemplate(ctx, entry, "market_prime_exchange", "market_prime", 200)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Inserted || len(res.Diffs) != 0 {
		t.Fatalf("expected unchanged re-sighting at a new base, got %+v", res)
	}
}

func TestSQLiteKB_UpsertMissionTemplate_InsertAndReinsert(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
		Type:       "mining",
		Difficulty: 1,
		Objectives: []serverapi.MissionObjective{
			{Type: "mine_resource", Description: "Mine 30 iron", ItemID: "iron_ore", Quantity: 30},
		},
		Rewards: &serverapi.MissionRewards{
			Credits: 1500,
			SkillXP: map[string]int{"mining": 15},
		},
	}

	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100)
	if err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	if !res.Inserted || len(res.Diffs) != 0 {
		t.Fatalf("expected insert with no diffs, got %+v", res)
	}

	res2, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 200)
	if err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	if res2.Inserted || len(res2.Diffs) != 0 {
		t.Fatalf("expected unchanged re-insert, got %+v", res2)
	}
}

func TestSQLiteKB_UpsertMissionTemplate_DetectsChanges(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first: %v", err)
	}
	entry.Title = "Iron Run (updated)"
	entry.Objectives = []serverapi.MissionObjective{
		{Type: "mine_resource", Description: "Mine 40 iron", ItemID: "iron_ore", Quantity: 40},
	}
	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 200)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Inserted {
		t.Fatalf("expected update, not insert")
	}
	fields := map[string]bool{}
	for _, d := range res.Diffs {
		fields[d.Field] = true
	}
	if !fields["title"] || !fields["objectives"] {
		t.Fatalf("expected title and objectives diffs, got %+v", res.Diffs)
	}

	var stored string
	row := kb.db.QueryRow("SELECT title FROM mission_templates WHERE id = ?", "iron_supply_run")
	if err := row.Scan(&stored); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stored != "Iron Run (updated)" {
		t.Fatalf("title not persisted, got %q", stored)
	}

	var count int
	if err := kb.db.QueryRow("SELECT COUNT(*) FROM mission_objectives WHERE mission_id = ?", "iron_supply_run").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 objective row, got %d", count)
	}
}

func TestSQLiteKB_UpsertMissionTemplate_SecondLocation(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "market_prime_exchange", "market_prime", 200); err != nil {
		t.Fatalf("second: %v", err)
	}
	var count int
	if err := kb.db.QueryRow("SELECT COUNT(*) FROM mission_template_locations WHERE mission_id = ?", "iron_supply_run").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 location rows, got %d", count)
	}
}

func TestUpsertMissionTemplate_RealFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/get_missions.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var resp serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.BaseID == "" {
		t.Fatalf("fixture missing base_id")
	}

	kb := NewMemoryKB()
	ctx := t.Context()

	var inserted, skipped int
	for _, entry := range resp.Missions {
		if entry.TemplateID == "" {
			skipped++
			continue
		}
		res, err := kb.UpsertMissionTemplate(ctx, entry, resp.BaseID, "haven", 100)
		if err != nil {
			t.Fatalf("upsert %s: %v", entry.MissionID, err)
		}
		if res.Inserted {
			inserted++
		}
	}
	if inserted == 0 {
		t.Fatalf("expected at least one non-procedural mission to be inserted")
	}
	if skipped == 0 {
		t.Fatalf("expected at least one procedural mission to be skipped")
	}

	// Re-run and confirm stability: no new inserts, no diffs.
	for _, entry := range resp.Missions {
		if entry.TemplateID == "" {
			continue
		}
		res, err := kb.UpsertMissionTemplate(ctx, entry, resp.BaseID, "haven", 200)
		if err != nil {
			t.Fatalf("re-upsert %s: %v", entry.MissionID, err)
		}
		if res.Inserted {
			t.Fatalf("%s: unexpected insert on re-run", entry.MissionID)
		}
		if len(res.Diffs) != 0 {
			t.Fatalf("%s: unexpected diffs on re-run: %+v", entry.MissionID, res.Diffs)
		}
	}
}
