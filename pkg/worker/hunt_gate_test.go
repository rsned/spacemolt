package worker

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func combatEntry(id string, difficulty int) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: id, Type: "combat", Title: id, Difficulty: difficulty,
		Objectives: []serverapi.MissionObjective{{Type: "kill_creature", Quantity: 3}},
	}
}

// The whole point of the gate: difficulty decides, never reward.
func TestHuntAdmissibleDifficultyCap(t *testing.T) {
	for _, tc := range []struct {
		id    string
		diff  int
		cap   int
		admit bool
	}{
		{"first_hunt_belt_grazers", 1, 1, true},
		{"grazer_cull", 2, 1, false},
		{"grazer_cull", 2, 2, true},
		{"ice_field_thinning", 2, 2, true},
		{"nebula_drift_hunt", 2, 2, true},
		{"starfall_prospector_defense", 4, 2, false},
		{"leviathan_bounty", 6, 2, false},
		{"smugglers_route", 7, 2, false},
	} {
		ok, reason, _ := huntAdmissible(combatEntry(tc.id, tc.diff), tc.cap, true, nil)
		if ok != tc.admit {
			t.Errorf("%s (diff %d, cap %d): admitted = %v, want %v (reason %q)",
				tc.id, tc.diff, tc.cap, ok, tc.admit, reason)
		}
		if !ok && reason == "" {
			t.Errorf("%s: a refusal must carry a reason", tc.id)
		}
	}
}

// leviathan_bounty is the mission a reward-maximising selector picks first: the
// best XP in the table, 8,000cr, AND repeatable, so it would be chosen forever.
// The Molt Leviathan hunts ships and fights to the death. It must stay refused
// however good its numbers look.
func TestHuntAdmissibleRefusesLeviathanOnAnyReward(t *testing.T) {
	e := combatEntry("leviathan_bounty", 6)
	e.Rewards = &serverapi.MissionRewards{Credits: 1_000_000}
	if ok, _, _ := huntAdmissible(e, 2, true, nil); ok {
		t.Fatal("a difficulty-6 mission must be refused no matter how large the reward")
	}

	// wildlifeOnly=false is what makes this test mean anything. leviathan_bounty
	// is ALSO outside the wildlife allowlist, so with gate 2 on it is refused
	// twice over and the assertion above passes even if the difficulty check is
	// deleted entirely — proven by removing that branch, which left this test
	// green. Lifting gate 2 leaves the difficulty cap as the only thing standing
	// between the fleet and a boss that fights to the death.
	for capLevel := range 3 {
		ok, reason, _ := huntAdmissible(e, capLevel, false, nil)
		if ok {
			t.Errorf("cap %d admitted the leviathan with gate 2 lifted — "+
				"the difficulty cap is not doing its job", capLevel)
		}
		if !strings.Contains(reason, "difficulty") {
			t.Errorf("cap %d refused for %q; the reason must name difficulty, "+
				"or some other gate is silently doing the work", capLevel, reason)
		}
	}
}

// Gate 2 is what lets gate 1 rise to 2 without admitting the two combat
// missions that shoot back.
func TestHuntAdmissibleWildlifeOnly(t *testing.T) {
	for _, id := range []string{"pirate_bounty", "convoy_defense"} {
		if ok, reason, _ := huntAdmissible(combatEntry(id, 2), 2, true, nil); ok {
			t.Errorf("%s must be refused while wildlifeOnly is set", id)
		} else if reason == "" {
			t.Errorf("%s: refusal needs a reason", id)
		}
		if ok, _, _ := huntAdmissible(combatEntry(id, 2), 2, false, nil); !ok {
			t.Errorf("%s must be admitted once wildlifeOnly is lifted", id)
		}
	}
}

// Non-combat types are none of this gate's business.
func TestHuntAdmissibleRejectsNonCombat(t *testing.T) {
	e := combatEntry("some_delivery", 1)
	e.Type = "delivery"
	if ok, _, _ := huntAdmissible(e, 1, true, nil); ok {
		t.Error("a delivery mission must not be admitted by the hunt gate")
	}
}

// A board entry with no kill objective is not huntable even if it is combat.
//
// The mission id must be one the wildlife allowlist ACCEPTS, and the difficulty
// must be under the cap, or an earlier gate refuses the entry first and this
// test proves nothing. An earlier version used the id "odd", which is not on the
// allowlist: gate 2 rejected it before the kill-objective check ran, so deleting
// that check left this test green.
func TestHuntAdmissibleNeedsAKillObjective(t *testing.T) {
	e := combatEntry("grazer_cull", 1) // on the allowlist, under the cap
	e.Objectives = []serverapi.MissionObjective{{Type: "dock_at_base"}}

	// Sanity: with a kill objective this entry IS admissible, so anything the
	// assertion below catches is the missing objective and nothing else.
	if ok, reason, _ := huntAdmissible(combatEntry("grazer_cull", 1), 1, true, nil); !ok {
		t.Fatalf("fixture is wrong: grazer_cull should be admissible, got %q", reason)
	}

	ok, reason, _ := huntAdmissible(e, 1, true, nil)
	if ok {
		t.Error("combat mission with no kill objective must be refused")
	}
	if !strings.Contains(reason, "kill_creature") {
		t.Errorf("refused for %q; the reason must name the missing kill objective, "+
			"or another gate is doing the work", reason)
	}
}

// Defaults are the safe end: cap 1, wildlife only.
func TestHuntGateDefaults(t *testing.T) {
	if huntDefaultMaxDifficulty != 1 {
		t.Errorf("default cap = %d, want 1", huntDefaultMaxDifficulty)
	}
	if !huntWildlifeOnlyDefault {
		t.Error("wildlife-only must default on")
	}
}

// completedWith builds a completed-mission history entry: the predecessor id
// and the mission it chains to.
func completedWith(id, chainNext string) serverapi.ViewCompletedMissionResponse {
	return serverapi.ViewCompletedMissionResponse{
		TemplateID: id, Title: id, ChainNext: chainNext,
		CompletionTime: "2026-08-09T00:00:00Z",
	}
}

// The chain exemption: a difficulty-2 mission is admitted only when THIS agent
// completed the mission that chains to it.
func TestHuntAdmitsAnEarnedChainContinuation(t *testing.T) {
	e := combatEntry("cracking_the_shell", 2)
	earned := map[string]string{"cracking_the_shell": "first_hunt_belt_grazers"}

	ok, reason, waived := huntAdmissible(e, 1, true, earned)
	if !ok {
		t.Fatalf("an earned continuation must be admitted over the cap, refused: %s", reason)
	}
	if waived != "first_hunt_belt_grazers" {
		t.Errorf("waived = %q, want the predecessor named so the exemption is visible", waived)
	}
}

// The same difficulty, no earned entry: still refused. Without this the
// exemption would be indistinguishable from raising the cap to 2.
func TestHuntRefusesAnUnearnedMissionAtTheSameDifficulty(t *testing.T) {
	earned := map[string]string{"cracking_the_shell": "first_hunt_belt_grazers"}
	ok, reason, waived := huntAdmissible(combatEntry("grazer_cull", 2), 1, true, earned)
	if ok {
		t.Fatal("grazer_cull is not a chain continuation and must stay refused at difficulty 2")
	}
	if !strings.Contains(reason, "difficulty") {
		t.Errorf("refusal reason %q must name difficulty", reason)
	}
	if waived != "" {
		t.Errorf("waived = %q, want empty for a refused mission", waived)
	}
}

// The exemption waives gate 1 ONLY. A chain that continues into something that
// shoots back is still refused.
func TestHuntChainExemptionCannotBypassWildlifeOnly(t *testing.T) {
	e := combatEntry("pirate_bounty", 2)
	earned := map[string]string{"pirate_bounty": "cracking_the_shell"}
	ok, reason, _ := huntAdmissible(e, 1, true, earned)
	if ok {
		t.Fatal("an earned continuation must still pass gate 2; wildlife-only is not waivable")
	}
	if !strings.Contains(reason, "wildlife") {
		t.Errorf("refusal reason %q must name the wildlife gate", reason)
	}
}

// Evidence is completion, not sight. An empty history — which is also what a
// server that never populates completed_missions looks like — refuses.
func TestHuntChainExemptionNeedsCompletionNotSight(t *testing.T) {
	e := combatEntry("cracking_the_shell", 2)
	for _, earned := range []map[string]string{nil, {}, {"ghosts_in_the_cloud": "cracking_the_shell"}} {
		if ok, _, _ := huntAdmissible(e, 1, true, earned); ok {
			t.Errorf("earned=%v admitted an unearned difficulty-2 mission", earned)
		}
	}
}

// The forgery F2 describes: the ACTIVE list landing under the completed key.
// Active missions carry chain_next but never completion_time, so nothing in
// one may buy a difficulty waiver.
func TestHuntChainEvidenceNeedsACompletionTime(t *testing.T) {
	ctx := context.Background()
	c := &fakeClient{raw: map[string][]byte{}}
	// Shaped exactly like an accepted-but-unfinished mission that chains on.
	c.raw["completed_missions"] = []byte(
		`{"missions":[{"template_id":"first_hunt_belt_grazers","title":"First Hunt","chain_next":"cracking_the_shell"}],"total_count":1}`)

	var log strings.Builder
	earned := huntEarnedContinuations(ctx, HuntDeps{Client: c, Out: &log}, &log)
	if len(earned) != 0 {
		t.Fatalf("earned = %v; an entry with no completion_time must not count", earned)
	}
	if !strings.Contains(log.String(), "no completion_time") {
		t.Errorf("must say why the entry was ignored, got:\n%s", log.String())
	}

	// The same entry WITH a completion time is real evidence.
	c.raw["completed_missions"] = []byte(
		`{"missions":[{"template_id":"first_hunt_belt_grazers","title":"First Hunt","chain_next":"cracking_the_shell","completion_time":"2026-08-09T00:00:00Z"}],"total_count":1}`)
	earned = huntEarnedContinuations(ctx, HuntDeps{Client: c, Out: io.Discard}, io.Discard)
	if earned["cracking_the_shell"] != "first_hunt_belt_grazers" {
		t.Errorf("earned = %v, want the completed predecessor credited", earned)
	}
}

// firstHuntEntry is the board entry for first_hunt_belt_grazers as the server
// actually posts it: difficulty 1, three kills, 1,000cr advertised. The reward
// matters here — 1,000cr is the figure the realized-payout gate would discount
// to ~370 if a combat mission ever reached it.
//
// Face value is what the mission genuinely pays: the captured completion
// (REAL-complete-mission-chain.json, craftsman-1, 2026-08-08) returned
// credits_earned 1000 against the advertised 1000, plus skill_xp_gained
// {weapons: 50, xenobiology: 15}.
func firstHuntEntry() serverapi.MissionBoardEntry {
	e := combatEntry("first_hunt_belt_grazers", 1)
	e.Rewards = &serverapi.MissionRewards{Credits: 1000}

	return e
}

// TestCombatMissionsDoNotReachThePayoutGate pins the architectural fact that
// makes an XP-based exemption to the realized-payout gate unnecessary today.
//
// Background: mission CREDIT payouts across this game run at ~37% of face
// because the empire treasury is broke (dev-confirmed, 2026-07-23). Skill XP is
// still paid in FULL, and XP is the entire point of the hunt fleet — so a gate
// that judges first_hunt_belt_grazers on realized credits would refuse the one
// mission the fleet exists to run. Task 5 was specced to add an exemption for
// exactly that.
//
// The exemption turned out to be unnecessary, because the hunt fleet does not
// use the mission runner's gate at all:
//
//   - Selection goes huntBoardJob -> huntAdmissible (hunt.go), which never
//     consults payoutRatio, MissionPayoutRatio or RecordMissionResult.
//   - The mission runner's category switch has cases for exploration,
//     smuggling and delivery only; type "combat" falls to `default: continue`
//     and never reaches a candidate builder.
//
// So this test does not assert an exemption. It asserts the BYPASS: a combat
// entry sitting on the board is dropped by the category switch, silently and
// without being priced. If a future change routes combat missions through the
// mission runner, this test fails and the exemption gets reconsidered THEN,
// against real requirements rather than speculation.
//
// The delivery entry is a positive control and is not optional. Without it a
// harness that read no board at all — or that failed before the switch — would
// pass every assertion below while proving nothing. It also runs the pass under
// the real broke-treasury condition (payoutRatio 0.37), so the gate this test
// is about is demonstrably live during the run.
func TestCombatMissionsDoNotReachThePayoutGate(t *testing.T) {
	control := boardEntry("m1", "steel", 20, "sol_station", "sol", 30_000, 0)
	hunt := firstHuntEntry()
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    30_000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions": boardJSON(t, hunt, control),
		},
	}
	// The observed treasury ratio. The control's reward clears the floor even
	// discounted; the point is that the discount is switched ON for this pass.
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}, payoutRatio: 0.37, payoutSamples: 50}

	var log bytes.Buffer
	if err := Missions(context.Background(), missionDepsTo(fc, store, missionKB(), &log)); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	got := log.String()

	// Positive control: the pass really did read the board, reach the category
	// switch, price a candidate against payoutRatio and accept it.
	if !slices.Contains(fc.calls, "accept:m1") {
		t.Fatalf("control delivery mission was not accepted, so this test proves nothing "+
			"about the combat entry; calls=%v log:\n%s", fc.calls, got)
	}

	// The combat entry must never be accepted.
	if slices.Contains(fc.calls, "accept:"+hunt.MissionID) {
		t.Errorf("the mission runner accepted a combat mission; calls=%v", fc.calls)
	}
	// ...and must never be PRICED. Every candidate builder returns a reason on
	// refusal and the loop logs it as "missions: skip <id>", so a skip line for
	// this id is proof the entry reached the gate. Silence is the property:
	// the category switch dropped it before any pricing happened.
	if strings.Contains(got, hunt.MissionID) {
		t.Errorf("combat entry %s reached the mission runner's gate — it was priced, not dropped by "+
			"the category switch. The realized-payout exemption now matters; log:\n%s", hunt.MissionID, got)
	}
	// Nothing about it may reach the ratio's own input either: mission_results
	// is what MissionPayoutRatio is computed from.
	for _, r := range store.results {
		if r.MissionID == hunt.MissionID || r.TemplateID == hunt.MissionID {
			t.Errorf("a combat mission was recorded to mission_results (%+v); it would feed "+
				"the fleet-wide realized ratio", r)
		}
	}
}

// TestPayoutRatioNeverPricesACombatMission is the unit-level half of the same
// property, and covers the second barrier independently of the category switch.
//
// Even if a combat entry were handed straight to buildMissionCandidate, its
// FIRST statement is deliverShape, which refuses any non-delivery type before
// the reward is read — so payoutRatio (applied at mission_select.go:540) cannot
// touch it. Asserting both ratios return the SAME refusal is what makes this
// about the ratio rather than about refusal in general.
//
// The reason assertion is load-bearing. A combat entry carries a kill_creature
// objective, which is neither deliver_item nor dock_at_base, so it is refused a
// second time a few lines later on objective shape. Without pinning the reason
// to the TYPE check, deleting the type check leaves this test green.
func TestPayoutRatioNeverPricesACombatMission(t *testing.T) {
	e := firstHuntEntry()
	dist := map[string]int{"sol": 1, "haven": 0}
	ask := func(string) (float64, bool) { return 1, true }
	noFuel := func(int) float64 { return 0 }

	_, atFace := buildMissionCandidate(e, dist, ask, noFuel, false, 1, missionMinNet, missionTicksPerJumpTest, 1)
	_, atTreasury := buildMissionCandidate(e, dist, ask, noFuel, false, 1, missionMinNet, missionTicksPerJumpTest, 0.37)

	if atFace == "" {
		t.Fatal("a combat entry must never price as a delivery candidate")
	}
	if atFace != atTreasury {
		t.Errorf("the payout ratio changed a combat mission's outcome (face %q vs 0.37 %q); "+
			"it must be refused on shape before any pricing", atFace, atTreasury)
	}
	if !strings.Contains(atFace, `type "combat"`) {
		t.Errorf("refused for %q; the reason must name the mission TYPE. Any other reason means "+
			"the type check is gone and something further in — past the reward read — is doing the work", atFace)
	}
}
