package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// aWordInPrivate is the LIVE board entry that exposed the type-keyed XP credit
// on 2026-07-29. Three level-0 smuggling canaries (fighter-2, explorer-1,
// explorer-2) sat at treasure_cache_trading_post with nothing acceptable
// because this — the only non-contraband smuggling XP on that board — was
// rejected for being 7 credits short of the floor.
//
// Shape is from the mission_templates row, NOT from a guess: type "delivery"
// (it is NOT typed smuggling and its chain_next is empty), rewards_credits 500,
// rewards_skill_xp {"nebula_attunement":35,"smuggling":50}, provided_items {}.
// The single objective is a dock_at_base, which is why its item cost is 0 and
// its net is exactly reward minus fuel.
func aWordInPrivate() serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: "a_word_in_private", Type: missionTypeDelivery, Title: "A Word in Private",
		Rewards: &serverapi.MissionRewards{
			Credits: 500,
			SkillXP: map[string]int{"nebula_attunement": 35, "smuggling": 50},
		},
		Objectives: []serverapi.MissionObjective{{
			Type: missionObjectiveDock, TargetBaseID: "treasure_cache_trading_post", SystemID: "treasure_cache",
		}},
	}
}

// The XP credit must key on whether the entry PAYS smuggling XP, not on whether
// the server typed the mission "smuggling". a_word_in_private is typed delivery,
// so it reaches buildMissionCandidate with allowSmuggling=false and — before the
// fix — skipped the gateNet adjustment entirely, losing 50 XP that the gate's
// own rate (missionSmugglingXPCreditValue) prices at 1250 credits. It was then
// refused over a 7-credit shortfall.
func TestDeliveryTypedMissionPayingSmugglingXPGetsTheXPCredit(t *testing.T) {
	dist := map[string]int{"treasure_cache": 1}
	noAsk := func(string) (float64, bool) { return 0, false }
	fuel := func(jumps int) float64 { return 7 } // explorer-1's live cost: net 493

	c, reason := buildMissionCandidate(aWordInPrivate(), dist, noAsk, fuel, false, 1, missionMinNet, missionTicksPerJumpTest)
	if reason != "" {
		t.Fatalf("a delivery paying 50 smuggling XP must clear the floor, got: %s", reason)
	}
	// Net stays the honest CREDIT number — the XP credit is a gate device only and
	// must never inflate what gets recorded and reported.
	if c.Net != 493 {
		t.Errorf("recorded Net = %v, want 493 (the real credit net, unflattered by the XP credit)", c.Net)
	}
}

// The XP credit must not become an unbudgeted licence to lose money. The
// smuggling branch books negative-net runs against missionSmugglingXPBudget
// (mission.go:908); a delivery-typed entry never reaches that accounting, so if
// the credit could drag a loss-making mission over the floor it would spend
// credits on XP with nothing tracking the spend. 1250 credits of XP credit is
// enough to rescue a net as low as -750, so the guard is load-bearing.
func TestXPCreditCannotRescueALossMakingDelivery(t *testing.T) {
	e := aWordInPrivate()
	e.Rewards.Credits = 150
	dist := map[string]int{"treasure_cache": 1}
	noAsk := func(string) (float64, bool) { return 0, false }
	fuel := func(jumps int) float64 { return 900 } // net -750; -750 + 1250 == the floor exactly

	_, reason := buildMissionCandidate(e, dist, noAsk, fuel, false, 1, missionMinNet, missionTicksPerJumpTest)
	if reason == "" {
		t.Fatal("a loss-making delivery must stay rejected: the XP credit is not a budgeted loss path")
	}
}

// Guard the existing smuggling-typed behaviour: that path already credited XP
// and must be untouched by the fix.
func TestSmugglingTypedMissionKeepsItsXPCredit(t *testing.T) {
	e := aWordInPrivate()
	e.Type = missionTypeSmuggling
	dist := map[string]int{"treasure_cache": 1}
	noAsk := func(string) (float64, bool) { return 0, false }
	fuel := func(jumps int) float64 { return 7 }

	if _, reason := buildMissionCandidate(e, dist, noAsk, fuel, true, 1, missionMinNet, missionTicksPerJumpTest); reason != "" {
		t.Fatalf("smuggling-typed entry must still clear the floor on XP: %s", reason)
	}
}

// A mission paying NO smuggling XP gets no credit, so an ordinary marginal
// delivery stays rejected. Without this, the fix would quietly drop the profit
// floor for the whole fleet.
func TestDeliveryWithoutSmugglingXPStillHitsTheFloor(t *testing.T) {
	e := aWordInPrivate()
	e.Rewards.SkillXP = map[string]int{"nebula_attunement": 35, "trading": 25}
	dist := map[string]int{"treasure_cache": 1}
	noAsk := func(string) (float64, bool) { return 0, false }
	fuel := func(jumps int) float64 { return 7 }

	_, reason := buildMissionCandidate(e, dist, noAsk, fuel, false, 1, missionMinNet, missionTicksPerJumpTest)
	if reason == "" {
		t.Fatal("a marginal delivery paying no smuggling XP must still be refused")
	}
}

// A server-rejected accept must not be retried. The live loop: all three
// canaries re-accepted the same L1-gated courier every ~10s forever, because
// the accept-failure path dropped the candidate from the trip without marking
// it attempted — so the very next pass re-priced and re-accepted it. 12 failed
// accepts in 4 minutes across 3 workers, indefinitely.
func TestFailedAcceptIsNotRetriedOnTheNextPass(t *testing.T) {
	entry := boardEntry("courier_l1_only", "steel", 2, "sol_station", "sol", 3000, 0)
	fc := &fakeClient{
		state:     missionState(true, 5000, 0),
		acceptErr: errSkillRequired{},
		raw:       map[string][]byte{"missions": boardJSON(t, entry)},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	deps := missionDeps(fc, store, missionKB())
	deps.State = &missionRunState{}

	for pass := range 2 {
		if err := Missions(context.Background(), deps); err != nil {
			t.Fatalf("pass %d: Missions: %v", pass+1, err)
		}
	}

	accepts := 0
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "accept:courier_l1_only") {
			accepts++
		}
	}
	if accepts != 1 {
		t.Errorf("a rejected accept was retried: %d accept calls across 2 passes, want 1 (calls: %v)", accepts, fc.calls)
	}
}

// errSkillRequired models the server's refusal verbatim.
type errSkillRequired struct{}

func (errSkillRequired) Error() string {
	return "skill_required: Smuggling missions require smuggling level 1."
}
