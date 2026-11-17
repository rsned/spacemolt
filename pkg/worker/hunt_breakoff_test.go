package worker

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// huntHullView is the captured battle view with only the two hull
// percentages changed: Participants[0] is the creature, [1] is us.
func huntHullView(t *testing.T, self, enemy int) func(*serverapi.GetBattleStatusResponse) {
	t.Helper()
	return func(r *serverapi.GetBattleStatusResponse) {
		r.Participants[0].HullPct = enemy
		r.Participants[1].HullPct = self
	}
}

func TestHuntWornDownRetreatsWhenOnlyHunterLosing(t *testing.T) {
	if !huntWornDown(100, 50, 100, 80) {
		t.Error("hunter 100->50 while the creature only 100->80: want a retreat")
	}
}

func TestHuntWornDownFightsOnWhenTradeIsEven(t *testing.T) {
	if huntWornDown(100, 50, 100, 45) {
		t.Error("hunter 100->50, creature 100->45: the creature is losing faster, want fight on")
	}
}

// Above 50% own hull the trend never forces a retreat, however lopsided.
func TestHuntWornDownHoldsAboveHalfHull(t *testing.T) {
	if huntWornDown(100, 51, 100, 100) {
		t.Error("hunter still above 50%: want fight on")
	}
}

// The trend gate is a judgement over the fight: tick 1 records the start, a
// later tick at 50% with an unhurt creature breaks off.
func TestHuntPolicyCreatureRetreatsOnTrend(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35, creature: true}
	if act := p.Decide(huntView(t, huntHullView(t, 100, 100))); act.Payload["stance"] == "flee" {
		t.Fatalf("tick 1 at full hull: got %+v, want no break-off", act)
	}
	if p.startSelf != 100 || p.startEnemy != 100 {
		t.Fatalf("start hulls = %d/%d, want 100/100", p.startSelf, p.startEnemy)
	}
	act := p.Decide(huntView(t, huntHullView(t, 50, 80)))
	if act.Payload["stance"] != "flee" {
		t.Fatalf("worn to 50%% while the creature is at 80%%: got %+v, want a flee stance", act)
	}
	if p.outcome != huntFled || !strings.Contains(p.reason, "worn down") {
		t.Errorf("outcome=%v reason=%q, want a worn-down retreat", p.outcome, p.reason)
	}
}

// Losing hull no faster than the creature, the hunter fights on past 50%.
func TestHuntPolicyCreatureFightsOnWhenWinningTheTrade(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35, creature: true}
	p.Decide(huntView(t, huntHullView(t, 100, 100)))
	if act := p.Decide(huntView(t, huntHullView(t, 40, 30))); act.Payload["stance"] == "flee" {
		t.Fatalf("hunter 40%%, creature 30%%: got %+v, want fight on", act)
	}
	if p.reason != "" {
		t.Errorf("reason=%q, want no break-off", p.reason)
	}
}

// The trend is never judged before both start hulls are known.
func TestHuntPolicyCreatureWaitsForBothStartHulls(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35, creature: true}
	p.Decide(huntView(t, huntHullView(t, 0, 100))) // own hull unreported
	if p.startSelf != 0 || p.startEnemy != 0 {
		t.Fatalf("start hulls recorded from a tick with own hull unknown: %d/%d", p.startSelf, p.startEnemy)
	}
	// First tick with both known is the start: already at 45% with no history,
	// there is no trend yet, so no worn-down retreat.
	if act := p.Decide(huntView(t, huntHullView(t, 45, 100))); act.Payload["stance"] == "flee" {
		t.Fatalf("first known tick: got %+v, want no trend judgement yet", act)
	}
	if p.startSelf != 45 || p.startEnemy != 100 {
		t.Errorf("start hulls = %d/%d, want 45/100", p.startSelf, p.startEnemy)
	}
}

// The hard floor still applies to creature fights, even when the creature is
// nearly dead.
func TestHuntPolicyCreatureKeepsHardFloor(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35, creature: true}
	p.Decide(huntView(t, huntHullView(t, 100, 100)))
	act := p.Decide(huntView(t, huntHullView(t, 34, 10)))
	if act.Payload["stance"] != "flee" {
		t.Fatalf("hull 34%% under the 35%% floor: got %+v, want a flee stance", act)
	}
	if p.outcome != huntFled || !strings.Contains(p.reason, "flee threshold") {
		t.Errorf("outcome=%v reason=%q, want the hard-floor break-off", p.outcome, p.reason)
	}
}

// Bounty / other combat fights keep today's single-threshold policy.
func TestHuntPolicyBountyUnchanged(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35}
	p.Decide(huntView(t, huntHullView(t, 100, 100)))
	if act := p.Decide(huntView(t, huntHullView(t, 45, 90))); act.Payload["stance"] == "flee" {
		t.Fatalf("bounty fight at 45%% vs 90%%: got %+v, want no break-off above the floor", act)
	}
	if p.reason != "" {
		t.Errorf("reason=%q, want no break-off", p.reason)
	}
}
