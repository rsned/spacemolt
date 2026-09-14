package knowledge

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// connections.last_updated_tick was written as a literal 0 on every insert and
// never touched again: all 2,168 rows in the live KB read 0, so the column
// carried no information at all. That mattered on 2026-09-14, when the table
// was found to be accumulating phantom rows and there was no way to tell a row
// captured minutes ago from one imported months ago.
func TestRememberSystem_StampsConnectionTick(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := context.Background()

	if err := kb.RememberSystem(ctx, System{
		ID:              "saiph",
		Name:            "Saiph",
		Position:        game.Position{X: 1, Y: 1},
		LastUpdatedTick: 1000,
		Connections:     []SystemConnection{{SystemID: "first_step", Distance: 218}},
	}); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}

	conns, err := kb.GetConnections(ctx)
	if err != nil {
		t.Fatalf("GetConnections: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("got %d connections, want 1", len(conns))
	}
	if conns[0].LastUpdatedTick != 1000 {
		t.Errorf("LastUpdatedTick = %d, want 1000", conns[0].LastUpdatedTick)
	}
}

// Re-observing the same lane must move the marker forward.
func TestRememberSystem_AdvancesConnectionTick(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := context.Background()

	for _, tick := range []int64{1000, 2500} {
		if err := kb.RememberSystem(ctx, System{
			ID: "saiph", Name: "Saiph", Position: game.Position{X: 1, Y: 1},
			LastUpdatedTick: tick,
			Connections:     []SystemConnection{{SystemID: "first_step", Distance: 218}},
		}); err != nil {
			t.Fatalf("RememberSystem(%d): %v", tick, err)
		}
	}

	conns, _ := kb.GetConnections(ctx)
	if len(conns) != 1 || conns[0].LastUpdatedTick != 2500 {
		t.Errorf("tick did not advance: %+v", conns)
	}
}

// A capture with no tick — a map import, say — must not blank a real stamp.
// The same stickiness the systems table already applies to description and
// is_stronghold: accept better information, never erase it.
func TestRememberSystem_ZeroTickDoesNotEraseConnectionStamp(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := context.Background()

	if err := kb.RememberSystem(ctx, System{
		ID: "saiph", Name: "Saiph", Position: game.Position{X: 1, Y: 1},
		LastUpdatedTick: 1000,
		Connections:     []SystemConnection{{SystemID: "first_step", Distance: 218}},
	}); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}
	if err := kb.RememberSystem(ctx, System{
		ID: "saiph", Name: "Saiph", Position: game.Position{X: 1, Y: 1},
		LastUpdatedTick: 0,
		Connections:     []SystemConnection{{SystemID: "first_step", Distance: 218}},
	}); err != nil {
		t.Fatalf("RememberSystem (no tick): %v", err)
	}

	conns, _ := kb.GetConnections(ctx)
	if len(conns) != 1 || conns[0].LastUpdatedTick != 1000 {
		t.Errorf("a tickless capture erased the stamp: %+v", conns)
	}
}

// The map import reconciles authoritatively (it deletes edges the map no longer
// lists), so it is the path that self-heals phantom rows — and it must stamp
// the freshness marker too, or a re-import silently resets every lane to
// "unknown age".
func TestUpsertSystemFromMap_StampsAndAdvancesConnectionTick(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := context.Background()

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "saiph", Name: "Saiph", PositionX: 1, PositionY: 1,
		Connections: []string{"first_step"}, LastUpdatedTick: 1000,
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap: %v", err)
	}
	conns, _ := kb.GetConnections(ctx)
	if len(conns) != 1 || conns[0].LastUpdatedTick != 1000 {
		t.Fatalf("first import did not stamp: %+v", conns)
	}

	// A later import of the same lane must move the marker forward, which
	// INSERT OR IGNORE cannot do.
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "saiph", Name: "Saiph", PositionX: 1, PositionY: 1,
		Connections: []string{"first_step"}, LastUpdatedTick: 2500,
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap (again): %v", err)
	}
	conns, _ = kb.GetConnections(ctx)
	if len(conns) != 1 || conns[0].LastUpdatedTick != 2500 {
		t.Errorf("re-import did not advance the marker: %+v", conns)
	}
}

// The reconciliation is what makes the map authoritative, and it is also the
// hazard: it deletes any stored lane the public map does not list. Permanent
// wormholes exist (two are discovered through the smuggling chain) and are not
// public map edges, so this pins the behaviour that would silently destroy them
// if they were ever stored as connection rows.
func TestUpsertSystemFromMap_PrunesLanesAbsentFromTheMap(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := context.Background()

	if err := kb.RememberSystem(ctx, System{
		ID: "hadar", Name: "Hadar", Position: game.Position{X: 1, Y: 1},
		LastUpdatedTick: 500,
		Connections: []SystemConnection{
			{SystemID: "public_neighbour", Distance: 100},
			{SystemID: "gsc_0036", Distance: 9999}, // wormhole-shaped: not a map edge
		},
	}); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "hadar", Name: "Hadar", PositionX: 1, PositionY: 1,
		Connections: []string{"public_neighbour"}, LastUpdatedTick: 1000,
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap: %v", err)
	}

	conns, _ := kb.GetConnections(ctx)
	for _, c := range conns {
		if c.ToSystem == "gsc_0036" {
			t.Fatal("expected the non-map lane to be pruned; if this ever needs " +
				"to survive, wormhole rows need a protected marker first")
		}
	}
}
