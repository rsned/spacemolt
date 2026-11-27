package worker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// What the newest look found decides the class, not how long ago a species
// was last seen.
func TestHuntClassifyGroundGoesByTheNewestLook(t *testing.T) {
	t.Parallel()
	now := huntNeedTestNow
	look := func(ago time.Duration, seen int) knowledge.WildlifeCoverage {
		return knowledge.WildlifeCoverage{CreaturesSeen: seen, ObservedUTC: huntAgo(ago)}
	}
	sighting := func(ago time.Duration) knowledge.WildlifeSighting {
		return knowledge.WildlifeSighting{Species: "belt_grazer", ObservedUTC: huntAgo(ago)}
	}
	for name, tc := range map[string]struct {
		looks     []knowledge.WildlifeCoverage
		sightings []knowledge.WildlifeSighting
		want      huntGroundClass
	}{
		"seen at a look 30h ago": {
			[]knowledge.WildlifeCoverage{look(30*time.Hour, 4)}, []knowledge.WildlifeSighting{sighting(30 * time.Hour)}, huntGroundSighted,
		},
		// The old rule called this "recently sighted": a sighting 5h old.
		"seen 5h ago, gone at a look 3h ago": {
			[]knowledge.WildlifeCoverage{look(3*time.Hour, 2), look(5*time.Hour, 4)}, []knowledge.WildlifeSighting{sighting(5 * time.Hour)}, huntGroundStale,
		},
		"seen at a look older than huntSightedFor": {
			[]knowledge.WildlifeCoverage{look(huntSightedFor+time.Hour, 4)}, []knowledge.WildlifeSighting{sighting(huntSightedFor + time.Hour)}, huntGroundStale,
		},
		"sighted, never looked at": {nil, []knowledge.WildlifeSighting{sighting(10 * time.Hour)}, huntGroundUnlooked},
	} {
		if got := huntClassifyGround(tc.looks, tc.sightings, now); got != tc.want {
			t.Errorf("%s: got %q want %q", name, huntGroundClassNames[got], huntGroundClassNames[tc.want])
		}
	}
}

// A need is not taken for a ground whose newest look found none of its
// species, though a need already held may still go there.
func TestHuntNeedJobWantsAGroundSeenAtTheLastLook(t *testing.T) {
	kb := huntNeedKBFixture(t)
	for _, s := range kb.sightings {
		kb.coverage = append(kb.coverage, knowledge.WildlifeCoverage{
			SystemID: s.SystemID, POIID: s.POIID, CreaturesSeen: 2, ObservedUTC: huntAgo(2 * time.Hour),
		})
	}
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{creatures: huntGrazers("c1")})
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "accept:") {
		t.Fatalf("accepted a need whose only ground was empty at the last look; calls %v\n%s", f.calls, log.String())
	}
	if !strings.Contains(log.String(), "seen at the last look") {
		t.Errorf("the skip must be logged, got:\n%s", log.String())
	}
	need := huntNeed{ItemID: "caustic_sac", Species: []string{kb.sightings[0].Species}}
	if _, poi, why := huntNeedGround(context.Background(), kb, kb, need, func(string) int { return 1 }, huntNearbyJumps, nil, huntNeedTestNow, huntPolicedMinLevel); poi == "" || !strings.Contains(why, "not seen at the last look") {
		t.Errorf("a held need may still try the ground as a last resort, got %q (%s)", poi, why)
	}
}

// A delivery leg cut short carries on from open space: no detour to a
// station first.
func TestHuntCarriesOnACutShortDelivery(t *testing.T) {
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(2)},
	})
	f.state.Ship.Cargo = []game.CargoItem{{ItemID: "caustic_sac", Quantity: 10}}
	f.state.Doc, f.state.CurrentPOI = false, "pollux_outer_belt"
	f.state.System.ID, f.state.CurrentSystem = "pollux", "pollux"
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	if deps.AgentID == "" || deps.AgentsDir == "" {
		t.Skip("fixture has no agent dir; sorties are off")
	}
	dir := filepath.Join(deps.AgentsDir, deps.AgentID)
	s := &huntdecide.Sortie{
		ID: "s1", JobKind: "need", JobID: "hex-caustic", ShipClass: "gauntlet",
		PendingResult: huntdecide.ResultInterrupted,
	}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(f.huntFake, "travel:levy_base") || !called(f.huntFake, "complete:hex-caustic") {
		t.Fatalf("the delivery must be carried on and completed; calls %v\n%s", f.calls, log.String())
	}
	if strings.Contains(log.String(), "relocating to") || strings.Contains(log.String(), "recovering to a station") {
		t.Errorf("no detour to a station before delivering:\n%s", log.String())
	}
	if left, _ := huntdecide.LoadSortie(dir); left != nil {
		t.Errorf("the sortie closes once delivered, still open: %+v", left)
	}

	// Resumes spent: the ordinary path takes the ship in instead.
	spent := &huntdecide.Sortie{PendingResult: huntdecide.ResultInterrupted, JobKind: "need", DeliveryResumes: huntMaxDeliveryResumes}
	if huntResumeDelivery(context.Background(), deps, &log, "pirate", spent, false) {
		t.Error("a delivery resumed huntMaxDeliveryResumes times is left to the station recovery")
	}
}

func TestHuntPassGetsALongerCommandCeiling(t *testing.T) {
	t.Parallel()
	deps := StandingDeps{CommandTimeout: 30 * time.Minute}
	for cmd, want := range map[string]time.Duration{"hunt": 2 * time.Hour, "deliver": 0, "mine": 30 * time.Minute} {
		if got := deps.commandTimeout([]string{cmd}); got != want {
			t.Errorf("%s: got %s want %s", cmd, got, want)
		}
	}
	if got := (StandingDeps{}).commandTimeout([]string{"hunt"}); got != 0 {
		t.Errorf("an opted-out caller stays uncapped, got %s", got)
	}
}
