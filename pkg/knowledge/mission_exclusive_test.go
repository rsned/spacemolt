package knowledge

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// A mission's location rows record where it has been SEEN on a board. They
// cannot express where it can be TAKEN: 11,365 of 11,389 templates have
// exactly one location row, so "one row" is the default, not a binding. The
// authoritative signal is the accept_mission refusal, which names the only
// station that offers it -- and the mission it names may have no location
// row at all, because nobody has ever seen it listed.
func TestSetMissionExclusiveBase(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)

	if _, err := kb.UpsertMissionTemplate(ctx, serverapi.MissionBoardEntry{
		MissionID: "frontier_extension", Title: "Frontier Extension", Type: "delivery",
	}, "", "", 100); err != nil {
		t.Fatalf("seed template: %v", err)
	}

	// The resolved id is stored, never the display name the refusal used.
	if err := kb.SetMissionExclusiveBase(ctx, "frontier_extension",
		"alpha_centauri_colonial_station", "accept_mission_refusal", 1916220); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, err := kb.MissionExclusiveBase(ctx, "frontier_extension")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("want a binding, got nil")
	}
	if got.BaseID != "alpha_centauri_colonial_station" {
		t.Errorf("base_id: got %q", got.BaseID)
	}
	if got.Source != "accept_mission_refusal" {
		t.Errorf("source: got %q", got.Source)
	}
	if got.Tick != 1916220 {
		t.Errorf("tick: got %d", got.Tick)
	}
	if got.SeenAt == "" {
		t.Error("seen_at must be stamped")
	}
}

// A mission with no binding recorded reads as nil, not as an empty string
// that a caller might route on.
func TestMissionExclusiveBaseUnsetIsNil(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)

	if _, err := kb.UpsertMissionTemplate(ctx, serverapi.MissionBoardEntry{
		MissionID: "no_questions_asked", Title: "No Questions Asked", Type: "smuggling",
	}, "", "", 100); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := kb.MissionExclusiveBase(ctx, "no_questions_asked")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("want nil for an unset binding, got %+v", got)
	}
}

// A later refusal wins: a mission can be moved by the server, and the newest
// authoritative statement is the one to route on.
func TestSetMissionExclusiveBaseOverwrites(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)

	if _, err := kb.UpsertMissionTemplate(ctx, serverapi.MissionBoardEntry{
		MissionID: "m1", Title: "M1", Type: "delivery",
	}, "", "", 1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = kb.SetMissionExclusiveBase(ctx, "m1", "old_station", "accept_mission_refusal", 1)
	if err := kb.SetMissionExclusiveBase(ctx, "m1", "new_station", "accept_mission_refusal", 2); err != nil {
		t.Fatalf("second set: %v", err)
	}
	got, _ := kb.MissionExclusiveBase(ctx, "m1")
	if got == nil || got.BaseID != "new_station" {
		t.Errorf("want new_station, got %+v", got)
	}
}

// Writing a binding for an unknown template must not silently no-op: the
// caller would believe it had recorded the giver.
func TestSetMissionExclusiveBaseUnknownMission(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)

	if err := kb.SetMissionExclusiveBase(ctx, "never_seen", "somewhere", "accept_mission_refusal", 1); err == nil {
		t.Error("want an error for a mission with no template row")
	}
}
