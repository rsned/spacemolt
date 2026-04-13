package knowledge

import (
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
