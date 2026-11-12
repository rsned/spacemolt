package knowledge

import (
	"context"
	"testing"
)

// A live get_system save replaces the system's outgoing lanes, empty list
// included; a plain save only adds. The Sanctum (no lanes) had been saved
// with Nexus Prime's two lanes, 2026-11-11.
func TestRememberSystemReplaceConnections(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	for _, id := range []string{"node_alpha", "node_beta", "nexus_prime"} {
		if err := kb.RememberSystem(ctx, System{ID: id, Name: id, LastUpdatedTick: 1}); err != nil {
			t.Fatal(err)
		}
	}
	bad := System{ID: "the_sanctum", Name: "The Sanctum", LastUpdatedTick: 1,
		Connections: []SystemConnection{{SystemID: "node_alpha", Distance: 353}, {SystemID: "node_beta", Distance: 585}}}
	if err := kb.RememberSystem(ctx, bad); err != nil {
		t.Fatal(err)
	}

	lanes := func(id string) int {
		s, err := kb.GetSystem(ctx, id)
		if err != nil || s == nil {
			t.Fatalf("GetSystem(%s): %v", id, err)
		}
		return len(s.Connections)
	}

	// A plain save with no lanes leaves the stored ones alone.
	if err := kb.RememberSystem(ctx, System{ID: "the_sanctum", Name: "The Sanctum", LastUpdatedTick: 2}); err != nil {
		t.Fatal(err)
	}
	if n := lanes("the_sanctum"); n != 2 {
		t.Fatalf("plain save changed lanes: %d, want 2", n)
	}

	// A live save with an empty list clears them.
	if err := kb.RememberSystem(ctx, System{ID: "the_sanctum", Name: "The Sanctum", LastUpdatedTick: 3, ReplaceConnections: true}); err != nil {
		t.Fatal(err)
	}
	if n := lanes("the_sanctum"); n != 0 {
		t.Fatalf("live empty save kept %d lanes, want 0", n)
	}

	// A live save keeps listed lanes and drops the rest.
	if err := kb.RememberSystem(ctx, System{ID: "nexus_prime", Name: "nexus_prime", LastUpdatedTick: 4,
		Connections: []SystemConnection{{SystemID: "node_alpha", Distance: 353}, {SystemID: "node_beta", Distance: 585}}}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberSystem(ctx, System{ID: "nexus_prime", Name: "nexus_prime", LastUpdatedTick: 5, ReplaceConnections: true,
		Connections: []SystemConnection{{SystemID: "node_alpha", Distance: 353}}}); err != nil {
		t.Fatal(err)
	}
	if n := lanes("nexus_prime"); n != 1 {
		t.Fatalf("nexus_prime lanes = %d, want 1", n)
	}
}
