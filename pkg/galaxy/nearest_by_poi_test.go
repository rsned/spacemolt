package galaxy

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestFindNearestByPOIType_Station(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	defer func() { _ = kb.Close() }()

	// Two systems, connected; a station at sys-b with public access.
	// Connections must be embedded in System.Connections for MemoryKB.GetConnections to work.
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:       "sys-a",
		Name:     "Alpha",
		Position: game.Position{X: 0, Y: 0},
		Connections: []knowledge.SystemConnection{
			{SystemID: "sys-b", Distance: 1},
		},
	}); err != nil {
		t.Fatalf("remember sys-a: %v", err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:       "sys-b",
		Name:     "Beta",
		Position: game.Position{X: 1, Y: 0},
		Connections: []knowledge.SystemConnection{
			{SystemID: "sys-a", Distance: 1},
		},
	}); err != nil {
		t.Fatalf("remember sys-b: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "poi-b-1", SystemID: "sys-b", Type: "station", Name: "Beta Station"}); err != nil {
		t.Fatalf("remember poi: %v", err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: "base-b-1", POIID: "poi-b-1", PublicAccess: true}); err != nil {
		t.Fatalf("remember base: %v", err)
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}

	results, err := FindNearestByPOIType(ctx, kb, g, "sys-a", "station", 3)
	if err != nil {
		t.Fatalf("FindNearestByPOIType: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SystemID != "sys-b" {
		t.Errorf("expected sys-b, got %s", results[0].SystemID)
	}
	if results[0].Hops != 1 {
		t.Errorf("expected 1 hop, got %d", results[0].Hops)
	}
}

func TestFindNearestByPOIType_StrongholdExcluded(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	defer func() { _ = kb.Close() }()

	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:   "sys-a",
		Name: "Alpha",
		Connections: []knowledge.SystemConnection{
			{SystemID: "sys-b", Distance: 1},
		},
	}); err != nil {
		t.Fatalf("remember sys-a: %v", err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:           "sys-b",
		Name:         "Beta",
		IsStronghold: true,
		Connections: []knowledge.SystemConnection{
			{SystemID: "sys-a", Distance: 1},
		},
	}); err != nil {
		t.Fatalf("remember sys-b: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "poi-b-1", SystemID: "sys-b", Type: "station"}); err != nil {
		t.Fatalf("remember poi: %v", err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: "base-b-1", POIID: "poi-b-1", PublicAccess: true}); err != nil {
		t.Fatalf("remember base: %v", err)
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}

	results, err := FindNearestByPOIType(ctx, kb, g, "sys-a", "station", 3)
	if err != nil {
		t.Fatalf("FindNearestByPOIType: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (stronghold excluded), got %d", len(results))
	}
}

func TestFindNearestByPOIType_OtherType(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	defer func() { _ = kb.Close() }()

	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:   "sys-a",
		Name: "Alpha",
		Connections: []knowledge.SystemConnection{
			{SystemID: "sys-b", Distance: 1},
		},
	}); err != nil {
		t.Fatalf("remember sys-a: %v", err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:   "sys-b",
		Name: "Beta",
		Connections: []knowledge.SystemConnection{
			{SystemID: "sys-a", Distance: 1},
		},
	}); err != nil {
		t.Fatalf("remember sys-b: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "poi-b-1", SystemID: "sys-b", Type: "asteroid_belt"}); err != nil {
		t.Fatalf("remember poi: %v", err)
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}

	results, err := FindNearestByPOIType(ctx, kb, g, "sys-a", "asteroid_belt", 3)
	if err != nil {
		t.Fatalf("FindNearestByPOIType: %v", err)
	}
	if len(results) != 1 || results[0].SystemID != "sys-b" {
		t.Errorf("expected one result at sys-b, got %+v", results)
	}
}

// An outpost is faction-members-only: an outsider's dock returns "Access
// denied". The KB still types its POI "station" and stores public_access=1, so
// before this gate a stranded shuttle was routed to one and parked there for
// two days (Ironlight Ferrous Range, Goldcrest, 2026-11-02..04). The nearest
// station must skip outposts and find the real one a jump further out.
func TestFindNearestByPOIType_OutpostExcluded(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	defer func() { _ = kb.Close() }()

	for _, s := range []knowledge.System{
		{ID: "goldcrest", Name: "Goldcrest", Connections: []knowledge.SystemConnection{{SystemID: "lhs_1140", Distance: 1}}},
		{ID: "lhs_1140", Name: "LHS 1140", Connections: []knowledge.SystemConnection{{SystemID: "goldcrest", Distance: 1}}},
	} {
		if err := kb.RememberSystem(ctx, s); err != nil {
			t.Fatalf("remember %s: %v", s.ID, err)
		}
	}
	for _, p := range []knowledge.POI{
		{ID: "ferrous_range", SystemID: "goldcrest", Type: "station"},
		{ID: "crossroads", SystemID: "lhs_1140", Type: "station"},
	} {
		if err := kb.RememberPOI(ctx, p); err != nil {
			t.Fatalf("remember poi %s: %v", p.ID, err)
		}
	}
	for _, b := range []knowledge.SpaceBase{
		{ID: "b-ferrous", POIID: "ferrous_range", PublicAccess: true, Type: "outpost"},
		{ID: "b-crossroads", POIID: "crossroads", PublicAccess: true, Type: "station"},
	} {
		if err := kb.RememberBase(ctx, b); err != nil {
			t.Fatalf("remember base %s: %v", b.ID, err)
		}
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}
	results, err := FindNearestByPOIType(ctx, kb, g, "goldcrest", "station", 1)
	if err != nil {
		t.Fatalf("FindNearestByPOIType: %v", err)
	}
	if len(results) != 1 || results[0].SystemID != "lhs_1140" {
		t.Fatalf("nearest station = %+v, want lhs_1140 (goldcrest holds only an outpost)", results)
	}
}
