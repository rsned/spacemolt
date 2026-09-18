package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/knowledge/knowledgetest"
)

// newKB opens a scratch knowledge base seeded with one system, one POI and
// the matching base row, so station-name resolution has something to find.
func newKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: knowledgetest.Path(t)})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "alpha_centauri", Name: "Alpha Centauri"}); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{
		ID: "alpha_centauri_colonial_station", Name: "Alpha Centauri Colonial Station",
		SystemID: "alpha_centauri", Type: "station", LastUpdatedTick: 1,
	}); err != nil {
		t.Fatalf("RememberPOI: %v", err)
	}
	return kb
}

// seedTemplate inserts a minimal mission_templates row; the binding update
// targets an existing row and fails with ErrMissionUnknown without one.
func seedTemplate(t *testing.T, kb *knowledge.SQLiteKB, id string) {
	t.Helper()
	if _, err := kb.UpsertMissionTemplate(context.Background(), serverapi.MissionBoardEntry{
		MissionID: id, Title: "Frontier Extension", Type: "delivery",
	}, "", "", 100); err != nil {
		t.Fatalf("seed template: %v", err)
	}
}

func TestBindResolvesStationNameAndRecordsBinding(t *testing.T) {
	kb := newKB(t)
	seedTemplate(t, kb, "frontier_extension")

	var out strings.Builder
	err := bind(context.Background(), kb, "frontier_extension",
		"Alpha Centauri Colonial Station", "operator", 1917226, &out)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	got, err := kb.MissionExclusiveBase(context.Background(), "frontier_extension")
	if err != nil {
		t.Fatalf("MissionExclusiveBase: %v", err)
	}
	if got == nil {
		t.Fatal("no binding recorded")
	}
	if got.BaseID != "alpha_centauri_colonial_station" {
		t.Errorf("BaseID = %q, want alpha_centauri_colonial_station", got.BaseID)
	}
	if got.SystemID != "alpha_centauri" {
		t.Errorf("SystemID = %q, want alpha_centauri", got.SystemID)
	}
	if got.Source != "operator" {
		t.Errorf("Source = %q, want operator", got.Source)
	}
	if got.Tick != 1917226 {
		t.Errorf("Tick = %d, want 1917226", got.Tick)
	}
}

func TestBindRejectsUnknownStationWithoutWriting(t *testing.T) {
	kb := newKB(t)
	seedTemplate(t, kb, "frontier_extension")

	var out strings.Builder
	err := bind(context.Background(), kb, "frontier_extension",
		"Nowhere Station", "operator", 1, &out)
	if !errors.Is(err, knowledge.ErrStationUnknown) {
		t.Fatalf("err = %v, want ErrStationUnknown", err)
	}
	got, _ := kb.MissionExclusiveBase(context.Background(), "frontier_extension")
	if got != nil {
		t.Errorf("binding written despite unresolved station: %+v", got)
	}
}

func TestBindRejectsUnknownMission(t *testing.T) {
	kb := newKB(t)

	var out strings.Builder
	err := bind(context.Background(), kb, "no_such_mission",
		"Alpha Centauri Colonial Station", "operator", 1, &out)
	if !errors.Is(err, knowledge.ErrMissionUnknown) {
		t.Fatalf("err = %v, want ErrMissionUnknown", err)
	}
}
