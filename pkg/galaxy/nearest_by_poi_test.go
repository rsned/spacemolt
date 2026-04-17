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
