package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// scopeClient is a client docked at the given base (empty = undocked).
func scopeClient(dockedAt string) *fakeClient {
	fc := &fakeClient{state: missionState(false, 5000, 0), raw: map[string][]byte{}}
	fc.state.Doc = dockedAt != ""
	fc.state.Player.DockedAtBase = dockedAt

	return fc
}

func scopeKB(t *testing.T) knowledge.Base {
	t.Helper()
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	for _, b := range []knowledge.SpaceBase{
		{ID: "frontier_station", POIID: "mobile_capital", Empire: "outerrim"},
		{ID: "treasure_cache_trading_post", POIID: "treasure_cache_tp", Empire: "outerrim"},
		{ID: "grand_exchange_station", POIID: "grand_exchange", Empire: "nebula"},
	} {
		if err := kb.RememberBase(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	return kb
}

// The purse is per-empire, so the ratio must be measured over the empire whose
// board this pass is reading. A galaxy-wide sample over-discounts the solvent
// empires and under-discounts the broke ones at the same time.
func TestMissionPayoutScopeNarrowsToTheDockedEmpire(t *testing.T) {
	deps := MissionDeps{
		KB:     scopeKB(t),
		Client: scopeClient("frontier_station"),
	}
	empire, bases := missionPayoutScope(context.Background(), deps)
	if empire != "outerrim" {
		t.Errorf("empire = %q, want outerrim", empire)
	}
	// Both outerrim bases, and NOT the nebula one.
	if len(bases) != 2 {
		t.Fatalf("bases = %v, want the two outerrim bases", bases)
	}
	for _, b := range bases {
		if b == "grand_exchange_station" {
			t.Errorf("bases = %v must not include another empire's station", bases)
		}
	}
}

// Undocked, or a station the KB has no base row for, must fall back to the
// galaxy-wide sample — the wider one. A tiny sample would trip the MinSamples
// floor and silently price everything at face value, which is the direction
// that loses money.
func TestMissionPayoutScopeFallsBackToGalaxy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		docked string
	}{
		{"undocked", ""},
		{"station not in the KB", "who_knows_station"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := MissionDeps{KB: scopeKB(t), Client: scopeClient(tc.docked)}
			empire, bases := missionPayoutScope(context.Background(), deps)
			if empire != "galaxy" || bases != nil {
				t.Errorf("scope = (%q, %v), want (galaxy, nil)", empire, bases)
			}
		})
	}
}

// The undocked guard exists to avoid a pointless KB round trip on every pass
// of an undocked worker. Asserting only the returned scope does NOT test it:
// GetBase("") errors anyway, so the guard can be deleted and the scope is still
// "galaxy" — the first version of this test passed for that wrong reason.
// What the guard actually buys is the query not happening.
func TestMissionPayoutScopeSkipsTheLookupWhenUndocked(t *testing.T) {
	kb := &countingBaseKB{Base: scopeKB(t)}
	deps := MissionDeps{KB: kb, Client: scopeClient("")}

	if empire, bases := missionPayoutScope(context.Background(), deps); empire != "galaxy" || bases != nil {
		t.Errorf("scope = (%q, %v), want (galaxy, nil)", empire, bases)
	}
	if kb.baseCalls != 0 {
		t.Errorf("GetBase called %d times while undocked, want 0", kb.baseCalls)
	}
}

// A nil KB or client must not panic a mission pass.
func TestMissionPayoutScopeHandlesMissingDeps(t *testing.T) {
	if empire, bases := missionPayoutScope(context.Background(), MissionDeps{}); empire != "galaxy" || bases != nil {
		t.Errorf("scope = (%q, %v), want (galaxy, nil)", empire, bases)
	}
}

// The station roster is fixed galaxy data, so it is resolved once and reused;
// re-querying it every ~11s on every worker computes a number that cannot have
// changed.
func TestEmpireBaseIDsAreCachedAfterFirstLookup(t *testing.T) {
	empireBases.mu.Lock()
	empireBases.m = map[string][]string{}
	empireBases.mu.Unlock()

	kb := &countingBaseKB{Base: scopeKB(t)}
	deps := MissionDeps{KB: kb, Client: scopeClient("frontier_station")}

	for range 5 {
		if _, bases := missionPayoutScope(context.Background(), deps); len(bases) != 2 {
			t.Fatalf("bases = %v, want 2", bases)
		}
	}
	if kb.calls != 1 {
		t.Errorf("GetBaseIDsByEmpire called %d times, want 1 — the roster is fixed", kb.calls)
	}
}

// countingBaseKB counts empire-roster and base lookups.
type countingBaseKB struct {
	knowledge.Base
	calls     int
	baseCalls int
}

func (c *countingBaseKB) GetBase(ctx context.Context, id string) (*knowledge.SpaceBase, error) {
	c.baseCalls++

	return c.Base.GetBase(ctx, id)
}

func (c *countingBaseKB) GetBaseIDsByEmpire(ctx context.Context, empire string) ([]string, error) {
	c.calls++

	return c.Base.GetBaseIDsByEmpire(ctx, empire)
}
