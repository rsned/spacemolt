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
