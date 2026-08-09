package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntFindGround asked the galaxy for the nearest systems that merely CONTAIN
// a belt, then applied the belt rule to whatever came back. The search limit
// therefore bounded RAW belt systems, not qualifying ones — so a crowd of
// disqualified belts nearer than the first good one pushed the answer off the
// end of the list and the pass reported no hunting ground at all.
//
// This is not hypothetical. Measured against the live KB on 2026-08-09 from
// void_gate — which is frontier_station, the home of BOTH hunt agents — the
// first qualifying belt system is rank 66 of 322 belt systems by jump
// distance, and huntPOISearchLimit is 25. Belts also get no relaxed-policing
// second pass, so the fleet would never have found a belt to hunt.
//
// The existing belt-rule tests could not catch it: they build a two-system KB,
// where a limit of 25 is never binding.
func TestHuntFindGroundLooksPastDisqualifiedBelts(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()

	// Home, plus more one-jump belt systems than the search limit. Every one
	// of them carries a station, so every one fails the belt rule.
	home := knowledge.System{
		ID: "home", Name: "Home", PoliceLevel: 55, LastVisitedTick: 1,
	}
	decoys := huntPOISearchLimit + 5
	for i := range decoys {
		id := fmt.Sprintf("decoy_%02d", i)
		home.Connections = append(home.Connections, knowledge.SystemConnection{SystemID: id})
		_ = kb.RememberSystem(ctx, knowledge.System{
			ID: id, Name: id, PoliceLevel: 55, LastVisitedTick: 1,
			Connections: []knowledge.SystemConnection{{SystemID: "home"}},
		})
		_ = kb.RememberPOI(ctx, knowledge.POI{
			ID: id + "_belt", SystemID: id, Type: huntPOITypeBelt,
			Resources: []game.POIResource{{ResourceID: huntBeltResource, Richness: 40, Remaining: 30000}},
		})
		// The disqualifier: a station means the belt is worked out.
		_ = kb.RememberPOI(ctx, knowledge.POI{ID: id + "_station", SystemID: id, Type: "station"})
	}

	// One qualifying belt, strictly further away than every decoy.
	home.Connections = append(home.Connections, knowledge.SystemConnection{SystemID: "hop"})
	_ = kb.RememberSystem(ctx, knowledge.System{
		ID: "hop", Name: "Hop", PoliceLevel: 55, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "home"}, {SystemID: "good"}},
	})
	_ = kb.RememberSystem(ctx, knowledge.System{
		ID: "good", Name: "Good", PoliceLevel: huntPolicedMinLevel, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "hop"}},
	})
	_ = kb.RememberPOI(ctx, knowledge.POI{
		ID: "good_belt", SystemID: "good", Type: huntPOITypeBelt,
		Resources: []game.POIResource{{ResourceID: huntBeltResource, Richness: 40, Remaining: 30000}},
	})
	_ = kb.RememberSystem(ctx, home)

	sys, poi, why := huntFindGround(ctx, kb, "home", huntPOITypeBelt, nil)
	if poi != "good_belt" {
		t.Fatalf("poi = %q (system %q, why %q), want good_belt — %d disqualified belts "+
			"nearer than it must not exhaust the search limit of %d",
			poi, sys, why, decoys, huntPOISearchLimit)
	}
	if sys != "good" {
		t.Errorf("system = %q, want good", sys)
	}
}

// The nearest QUALIFYING ground still wins: this must not become
// richest-anywhere. Two qualifying systems, and the closer one takes it.
func TestHuntFindGroundStillPrefersNearestQualifying(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()

	_ = kb.RememberSystem(ctx, knowledge.System{
		ID: "home", Name: "Home", PoliceLevel: 55, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "near"}},
	})
	_ = kb.RememberSystem(ctx, knowledge.System{
		ID: "near", Name: "Near", PoliceLevel: huntPolicedMinLevel, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "home"}, {SystemID: "far"}},
	})
	_ = kb.RememberPOI(ctx, knowledge.POI{
		ID: "near_belt", SystemID: "near", Type: huntPOITypeBelt,
		Resources: []game.POIResource{{ResourceID: huntBeltResource, Richness: 10, Remaining: 100}},
	})
	_ = kb.RememberSystem(ctx, knowledge.System{
		ID: "far", Name: "Far", PoliceLevel: huntPolicedMinLevel, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "near"}},
	})
	// Far is far richer — and must still lose, because hops are fuel.
	_ = kb.RememberPOI(ctx, knowledge.POI{
		ID: "far_belt", SystemID: "far", Type: huntPOITypeBelt,
		Resources: []game.POIResource{{ResourceID: huntBeltResource, Richness: 99, Remaining: 999999}},
	})

	if _, poi, why := huntFindGround(ctx, kb, "home", huntPOITypeBelt, nil); poi != "near_belt" {
		t.Errorf("poi = %q (why %q), want near_belt — nearest qualifying, not richest", poi, why)
	}
}
