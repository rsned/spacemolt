package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// missionEntry is a minimal board entry for seeding a template row.
func missionEntry(id, title string) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{MissionID: id, Title: title, Type: "delivery"}
}

// The refusal quotes a station's DISPLAY NAME, never its id. Station ids are
// dual-named (POI ids vs base ids), so the name has to be resolved before
// anything is stored -- a display name in an id column is the bug this whole
// column exists to avoid.
func TestParseMissionOnlyAvailableAt(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "the live refusal, verbatim",
			msg:  "mission_not_available: This mission is only available at Alpha Centauri Colonial Station.",
			want: "Alpha Centauri Colonial Station",
		},
		{
			name: "without the code prefix",
			msg:  "This mission is only available at Treasure Cache Trading Post.",
			want: "Treasure Cache Trading Post",
		},
		{
			name: "no trailing period",
			msg:  "This mission is only available at Sable Port Station",
			want: "Sable Port Station",
		},
		{
			name: "a different refusal must not match",
			msg:  "mission_not_available: You do not meet the requirements.",
			want: "",
		},
		{
			name: "an unrelated error must not match",
			msg:  "Your weapons cannot fire — magazine empty!",
			want: "",
		},
		{name: "empty", msg: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseMissionOnlyAvailableAt(tc.msg); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// Resolving goes display name -> base id against what we know of the galaxy,
// and must refuse rather than guess when the name is unknown.
func TestResolveStationName(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)

	if err := kb.RememberSystem(ctx, System{
		ID: "alpha_centauri", Name: "Alpha Centauri",
	}); err != nil {
		t.Fatalf("seed system: %v", err)
	}
	if err := kb.RememberPOI(ctx, POI{
		ID: "alpha_centauri_colonial_station", Name: "Alpha Centauri Colonial Station",
		SystemID: "alpha_centauri", Type: "station", LastUpdatedTick: 1,
	}); err != nil {
		t.Fatalf("seed poi: %v", err)
	}

	id, systemID, err := kb.ResolveStationName(ctx, "Alpha Centauri Colonial Station")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "alpha_centauri_colonial_station" {
		t.Errorf("id: got %q", id)
	}
	if systemID != "alpha_centauri" {
		t.Errorf("system: got %q", systemID)
	}

	// Case and surrounding whitespace should not defeat it.
	if id2, _, err := kb.ResolveStationName(ctx, "  alpha centauri colonial station "); err != nil || id2 != id {
		t.Errorf("case/space insensitive lookup failed: %q %v", id2, err)
	}

	// An unknown name must error, never return an empty id the caller might
	// store as though it had resolved.
	if _, _, err := kb.ResolveStationName(ctx, "Nowhere Station"); err == nil {
		t.Error("want an error for an unknown station")
	}
}

// End to end: the refusal craftsman-1 actually received becomes a stored
// binding carrying the RESOLVED id, not the display name it quoted.
func TestRecordMissionRefusal(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)

	if err := kb.RememberSystem(ctx, System{ID: "alpha_centauri", Name: "Alpha Centauri"}); err != nil {
		t.Fatalf("seed system: %v", err)
	}
	if err := kb.RememberPOI(ctx, POI{
		ID: "alpha_centauri_colonial_station", Name: "Alpha Centauri Colonial Station",
		SystemID: "alpha_centauri", Type: "station", LastUpdatedTick: 1,
	}); err != nil {
		t.Fatalf("seed poi: %v", err)
	}
	if _, err := kb.UpsertMissionTemplate(ctx, missionEntry("frontier_extension", "Frontier Extension"), "", "", 1); err != nil {
		t.Fatalf("seed template: %v", err)
	}

	ok, err := kb.RecordMissionRefusal(ctx, "frontier_extension",
		"mission_not_available: This mission is only available at Alpha Centauri Colonial Station.", 1916220)
	if err != nil || !ok {
		t.Fatalf("record: ok=%v err=%v", ok, err)
	}

	got, err := kb.MissionExclusiveBase(ctx, "frontier_extension")
	if err != nil || got == nil {
		t.Fatalf("read back: %+v %v", got, err)
	}
	if got.BaseID != "alpha_centauri_colonial_station" {
		t.Errorf("stored id: got %q", got.BaseID)
	}
	if got.SystemID != "alpha_centauri" {
		t.Errorf("stored system: got %q", got.SystemID)
	}
	if strings.Contains(got.BaseID, " ") {
		t.Errorf("a display name was stored in an id column: %q", got.BaseID)
	}
}

// A refusal for some other reason must be a quiet no-op, so a caller can pass
// every accept_mission error through without pre-filtering.
func TestRecordMissionRefusalIgnoresOtherErrors(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)
	if _, err := kb.UpsertMissionTemplate(ctx, missionEntry("m1", "M1"), "", "", 1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ok, err := kb.RecordMissionRefusal(ctx, "m1", "mission_not_available: You do not meet the requirements.", 1)
	if err != nil || ok {
		t.Errorf("want a quiet no-op, got ok=%v err=%v", ok, err)
	}
	if got, _ := kb.MissionExclusiveBase(ctx, "m1"); got != nil {
		t.Errorf("nothing should have been stored, got %+v", got)
	}
}

// An unresolvable station name must fail loudly rather than store a guess.
func TestRecordMissionRefusalUnknownStation(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)
	if _, err := kb.UpsertMissionTemplate(ctx, missionEntry("m1", "M1"), "", "", 1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := kb.RecordMissionRefusal(ctx, "m1",
		"This mission is only available at Nowhere Station.", 1); !errors.Is(err, ErrStationUnknown) {
		t.Errorf("want ErrStationUnknown, got %v", err)
	}
}
