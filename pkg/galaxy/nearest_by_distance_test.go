package galaxy

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func addStationSystem(t *testing.T, kb knowledge.Base, id string, x, y float64, stronghold bool) {
	t.Helper()
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, knowledge.System{ID: id, Name: id, Position: game.Position{X: x, Y: y}, IsStronghold: stronghold}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: id + "-st", SystemID: id, Type: "station"}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: id + "-base", POIID: id + "-st", PublicAccess: !stronghold}); err != nil {
		t.Fatal(err)
	}
}

// A pathfinder ship ends at the closest station in a straight line, never
// at a stronghold (dead end without the pirate unlock) and never at a
// system with no coordinates (its (0,0) would look closest).
func TestNearestStationByDistanceSkipsStronghold(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addStationSystem(t, kb, "a_public", 100, 0, false)
	addStationSystem(t, kb, "b_stronghold", 10, 0, true)
	addStationSystem(t, kb, "c_unplaced", 0, 0, false)
	addStationSystem(t, kb, "d_far", 5000, 0, false)

	id, ok, err := NearestStationByDistance(context.Background(), kb, 0, 0)
	if err != nil || !ok || id != "a_public" {
		t.Fatalf("got %q ok=%v err=%v, want a_public", id, ok, err)
	}
}

func TestNearestStationByDistanceNone(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addStationSystem(t, kb, "only_stronghold", 10, 0, true)
	_, ok, err := NearestStationByDistance(context.Background(), kb, 0, 0)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want false,nil", ok, err)
	}
}
