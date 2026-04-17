package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/dataservice"
	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func newTestDeps(t *testing.T) (dataservice.Deps, func()) {
	t.Helper()
	ctx := context.Background()

	kb := knowledge.NewMemoryKB()

	// Fixture: sys-a -> sys-b, station at sys-b, public access.
	// Connections are embedded in System.Connections for MemoryKB.GetConnections to work.
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
	if err := kb.RememberPOI(ctx, knowledge.POI{
		ID:       "poi-b-1",
		SystemID: "sys-b",
		Type:     "station",
		Name:     "Beta Station",
	}); err != nil {
		t.Fatalf("remember poi: %v", err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{
		ID:           "base-b-1",
		POIID:        "poi-b-1",
		PublicAccess: true,
	}); err != nil {
		t.Fatalf("remember base: %v", err)
	}

	g := &galaxy.GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}

	deps := dataservice.Deps{
		KB:    kb,
		Graph: g,
		Tick:  func() int64 { return 100 },
	}
	return deps, func() { _ = kb.Close() }
}

func TestNearest_PlaintextHappy(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"station", "from", "sys-a"})
	if err != nil {
		t.Fatalf("HandlePlaintext: %v", err)
	}
	if !strings.Contains(reply, "Beta") {
		t.Errorf("reply missing destination name: %q", reply)
	}
	if !strings.Contains(reply, "1 hop") {
		t.Errorf("reply missing hop count: %q", reply)
	}
}

func TestNearest_PlaintextMissingFrom(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	_, err := h.HandlePlaintext(context.Background(), deps, []string{"station"})
	if err == nil {
		t.Fatalf("expected error for missing 'from'")
	}
	if !strings.Contains(err.Error(), "from") {
		t.Errorf("error message should mention 'from': %v", err)
	}
}

func TestNearest_PlaintextBadGrammar(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	_, err := h.HandlePlaintext(context.Background(), deps, []string{"station", "at", "sys-a"})
	if err == nil {
		t.Fatalf("expected error for bad connective")
	}
}

func TestNearest_PlaintextNoResults(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"wormhole", "from", "sys-a"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(strings.ToLower(reply), "no accessible") {
		t.Errorf("expected no-results message, got %q", reply)
	}
}

func TestNearest_JSONHappy(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	out, err := h.HandleJSON(context.Background(), deps, map[string]any{
		"poi_type":    "station",
		"from_system": "sys-a",
	})
	if err != nil {
		t.Fatalf("HandleJSON: %v", err)
	}
	if out["from_system"] != "sys-a" {
		t.Errorf("from_system: got %v", out["from_system"])
	}
	results, ok := out["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results not a slice of maps, got %T", out["results"])
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0]["system_id"] != "sys-b" {
		t.Errorf("system_id: got %v", results[0]["system_id"])
	}
}

func TestNearest_JSONMissingField(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	_, err := h.HandleJSON(context.Background(), deps, map[string]any{
		"poi_type": "station",
	})
	if err == nil {
		t.Fatalf("expected error for missing from_system")
	}
	if !strings.Contains(err.Error(), "from_system") {
		t.Errorf("error should name field: %v", err)
	}
}

func TestNearest_PlaintextReplyWithinBudget(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"station", "from", "sys-a"})
	if err != nil {
		t.Fatalf("HandlePlaintext: %v", err)
	}
	if len(reply) > dataservice.MaxReplyChars {
		t.Errorf("reply exceeds MaxReplyChars: %d > %d", len(reply), dataservice.MaxReplyChars)
	}
}
