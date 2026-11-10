package knowledge

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestUpsertMissionTemplate_StoresReputation(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{MissionID: "the_archive_report", TemplateID: "the_archive_report",
		Title: "The Archive Report", Type: "delivery",
		Rewards: &serverapi.MissionRewards{Credits: 8000, Reputation: 3, PirateRep: 1}}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "b", "s", 1); err != nil {
		t.Fatal(err)
	}
	var rep, pirate int
	if err := kb.db.QueryRowContext(ctx, `SELECT rewards_reputation, rewards_pirate_rep FROM mission_templates WHERE id='the_archive_report'`).Scan(&rep, &pirate); err != nil {
		t.Fatal(err)
	}
	if rep != 3 || pirate != 1 {
		t.Fatalf("rep=%d pirate=%d", rep, pirate)
	}
	// A changed reputation reward is a diff like any other reward.
	entry.Rewards.Reputation = 5
	res, err := kb.UpsertMissionTemplate(ctx, entry, "b", "s", 2)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Diffs {
		if d.Field == "rewards_reputation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no rewards_reputation diff in %+v", res.Diffs)
	}
}

func TestRecordMissionUnlocks_Edges(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := t.Context()
	unlocks := []serverapi.MissionUnlock{
		{Available: false,
			Mission: serverapi.MissionPointer{MissionID: "joining_the_investigators", Title: "Joining the Investigators", BaseID: "first_step_memorial_station", SystemID: "first_step"},
			StillRequires: []serverapi.MissionPrerequisite{{
				Mission:  serverapi.MissionPointer{MissionID: "the_listening_foundry", Title: "The Listening Foundry"},
				NextStep: serverapi.MissionPointer{MissionID: "signal_triage"},
			}}},
		{Available: true, Mission: serverapi.MissionPointer{MissionID: "m2", Title: "Two"}},
	}
	n, err := kb.RecordMissionUnlocks(ctx, "processing_the_data", unlocks, "craftsman-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("want 3 edges, got %d", n)
	}
	// Re-recording is idempotent.
	if _, err = kb.RecordMissionUnlocks(ctx, "processing_the_data", unlocks, "craftsman-1", 11); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = kb.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mission_prerequisites`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("want 3 rows, got %d", count)
	}
	var base string
	if err = kb.db.QueryRowContext(ctx, `SELECT mission_base_id FROM mission_prerequisites
		WHERE mission_id='joining_the_investigators' AND requires_mission_id='the_listening_foundry'`).Scan(&base); err != nil {
		t.Fatal(err)
	}
	if base != "first_step_memorial_station" {
		t.Fatalf("base %q", base)
	}
	// Without the completed template id only still_requires edges are known.
	n, err = kb.RecordMissionUnlocks(ctx, "", unlocks, "craftsman-1", 12)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestRecordMissionCompletion_Reputation(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := t.Context()
	c := MissionCompletion{AgentID: "craftsman-1", MissionID: "1600377fd1e4823242f5deb4cd004e37", TemplateID: "the_archive_report",
		Title: "The Archive Report", CreditsEarned: 8000, ReputationChanges: map[string]int{"outerrim": 3},
		SkillXPGained: map[string]int{"exploration": 90}, Tick: 2369679}
	if err := kb.RecordMissionCompletion(ctx, c); err != nil {
		t.Fatal(err)
	}
	// The same completion seen twice (play_as + worker, or a retry) is one row.
	if err := kb.RecordMissionCompletion(ctx, c); err != nil {
		t.Fatal(err)
	}
	var rep, tmpl string
	var count int
	if err := kb.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(reputation_changes), MAX(template_id) FROM mission_completions`).Scan(&count, &rep, &tmpl); err != nil {
		t.Fatal(err)
	}
	if count != 1 || rep != `{"outerrim":3}` || tmpl != "the_archive_report" {
		t.Fatalf("count=%d rep=%s tmpl=%s", count, rep, tmpl)
	}
}

// v0.687.0: the recipes a completion taught are stored; a later record
// without them (e.g. a backfill from an older ledger) does not erase them.
func TestRecordMissionCompletion_RecipesUnlocked(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := t.Context()
	c := MissionCompletion{AgentID: "craftsman-1", MissionID: "m1", Title: "Teach",
		RecipesUnlocked: []string{"build_focused_crystal", "build_power_cell"}}
	if err := kb.RecordMissionCompletion(ctx, c); err != nil {
		t.Fatal(err)
	}
	c.RecipesUnlocked = nil
	if err := kb.RecordMissionCompletion(ctx, c); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := kb.db.QueryRowContext(ctx, `SELECT recipes_unlocked FROM mission_completions WHERE mission_id='m1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != `["build_focused_crystal","build_power_cell"]` {
		t.Fatalf("recipes_unlocked = %s", got)
	}
	if err := kb.RecordMissionCompletion(ctx, MissionCompletion{MissionID: "m2"}); err != nil {
		t.Fatal(err)
	}
	if err := kb.db.QueryRowContext(ctx, `SELECT recipes_unlocked FROM mission_completions WHERE mission_id='m2'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != `[]` {
		t.Fatalf("no recipes: got %s, want []", got)
	}
}
