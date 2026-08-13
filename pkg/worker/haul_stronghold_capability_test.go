package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

// The stronghold guard exists because an agent at the -30 hostile default is
// destroyed on arrival, so a lucrative stronghold bid is worse than useless to
// it. But the guard was written when NO agent could dock at a stronghold, and
// it is unconditional: it drops those routes for everyone, forever.
//
// That is now leaving money on the floor. On 2026-08-12 the three most valuable
// opportunities on the whole board were stronghold-destined -- 144,480 /
// 123,280 / 81,360 -- and strongholds carried 25% of available gross from 16%
// of the rows at 1.8x the average margin. Eleven agents hold the unlock and can
// dock there safely.
//
// The capability is per-agent and already readable: smugglingUnlocked reads the
// live pirate baseline off Player.Standings.

func strongholdSystems() []knowledge.System {
	return []knowledge.System{
		{ID: "alhena", Name: "Alhena", IsStronghold: true},
		{ID: "haven", Name: "Haven"},
	}
}

func strongholdOppSet() []market.ArbitrageOpportunity {
	return []market.ArbitrageOpportunity{
		{ItemID: "liquid_hydrogen", FromSystemName: "Haven", ToSystemName: "Alhena", GrossProfit: 144480},
		{ItemID: "steel_plate", FromSystemName: "Haven", ToSystemName: "Haven", GrossProfit: 5000},
	}
}

// unlockedState returns a state whose player holds the pirate unlock.
func unlockedState(baseline int) *game.State {
	return &game.State{Player: game.Player{Standings: map[string]game.EmpireStanding{
		"pirate_voss": {Baseline: baseline},
	}}}
}

func TestStrongholdRefsAreEmptyForAnUnlockedAgent(t *testing.T) {
	refs := strongholdRefsFor(unlockedState(10), strongholdSystems())

	if len(refs) != 0 {
		t.Fatalf("an agent holding the unlock must have no stronghold refs to avoid; got %v", refs)
	}
	kept, dropped := dropStrongholdOpps(strongholdOppSet(), refs)
	if len(kept) != 2 || len(dropped) != 0 {
		t.Fatalf("unlocked agent must keep stronghold routes: kept %d dropped %v", len(kept), dropped)
	}
}

// The safety property that must not regress: 115 of 122 agents are still
// hostile, and for them a stronghold route is a death sentence.
func TestStrongholdRefsStillGuardALockedAgent(t *testing.T) {
	refs := strongholdRefsFor(&game.State{}, strongholdSystems())

	if len(refs) == 0 {
		t.Fatal("a locked agent must still avoid strongholds")
	}
	kept, dropped := dropStrongholdOpps(strongholdOppSet(), refs)
	if len(kept) != 1 || len(dropped) != 1 {
		t.Fatalf("locked agent must drop the stronghold route: kept %d dropped %v", len(kept), dropped)
	}
	if kept[0].ItemID != "steel_plate" {
		t.Fatalf("wrong route survived: %+v", kept[0])
	}
}

// Baseline 10 is the unlock threshold; anything below it is still hostile. A
// >= comparison that slipped to > would silently strand every agent sitting
// exactly at the granted value.
func TestStrongholdCapabilityHonoursTheExactThreshold(t *testing.T) {
	if len(strongholdRefsFor(unlockedState(10), strongholdSystems())) != 0 {
		t.Error("baseline exactly 10 must count as unlocked")
	}
	if len(strongholdRefsFor(unlockedState(9), strongholdSystems())) == 0 {
		t.Error("baseline 9 is still hostile and must be guarded")
	}
}

// A nil state means we could not read standings this pass. That must read as
// LOCKED: assuming capability we cannot see is how a hauler flies into a
// stronghold it cannot dock at.
func TestUnreadableStandingsAreTreatedAsLocked(t *testing.T) {
	if len(strongholdRefsFor(nil, strongholdSystems())) == 0 {
		t.Fatal("unreadable standings must fall back to guarding strongholds")
	}
}
