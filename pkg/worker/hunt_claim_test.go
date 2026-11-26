package worker

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func huntClaimDeps(dir, agent string, now time.Time) HuntDeps {
	return HuntDeps{AgentID: agent, AgentsDir: dir, NowFn: func() time.Time { return now }}
}

func TestHuntClaimKeepsOtherHuntersOffAGround(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := huntNeedTestNow
	a, b := huntClaimDeps(dir, "pirate-5", now), huntClaimDeps(dir, "pirate-9", now)

	huntClaimGround(a, io.Discard, "pollux", "pollux_outer_belt", []string{"belt_grazer"})
	if got := huntClaimedPOIs(b, []string{"belt_grazer"}); !slices.Equal(got, []string{"pollux_outer_belt"}) {
		t.Fatalf("pirate-9 must see pirate-5's claim, got %v", got)
	}
	if got := huntClaimedPOIs(a, []string{"belt_grazer"}); len(got) != 0 {
		t.Errorf("a hunter's own claim never bars it, got %v", got)
	}
	if got := huntClaimedPOIs(b, []string{"etchmaw"}); len(got) != 0 {
		t.Errorf("a claim for another species does not bar the ground, got %v", got)
	}

	// A claim not renewed for huntClaimFor lapses: a stranded hunter cannot
	// hold a ground.
	later := huntClaimDeps(dir, "pirate-9", now.Add(huntClaimFor))
	if got := huntClaimedPOIs(later, []string{"belt_grazer"}); len(got) != 0 {
		t.Errorf("a claim %s old must lapse, got %v", huntClaimFor, got)
	}

	// A new claim replaces the old one; a release drops it.
	huntClaimGround(a, io.Discard, "levy", "levy_ice_fields", nil)
	if got := huntClaimedPOIs(b, []string{"belt_grazer"}); !slices.Equal(got, []string{"levy_ice_fields"}) {
		t.Errorf("one claim per hunter, and a claim with no species bars all, got %v", got)
	}
	huntReleaseClaim(a)
	if got := huntClaimedPOIs(b, nil); len(got) != 0 {
		t.Errorf("a released claim is gone, got %v", got)
	}
}

// The board's only ground is claimed by another hunter: the need is not
// taken. A hunter already holding the need shares the ground instead.
func TestHuntNeedJobLeavesAClaimedGroundAlone(t *testing.T) {
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{creatures: huntGrazers("c1")})
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	other := huntClaimDeps(deps.AgentsDir, "pirate-other", huntNeedTestNow)
	for _, s := range kb.sightings {
		huntClaimGround(other, io.Discard, s.SystemID, s.POIID, []string{s.Species})
	}
	if deps.AgentID == "" || deps.AgentsDir == "" {
		t.Skip("fixture has no agent dir; claims are off")
	}
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "accept:") {
		t.Fatalf("accepted a need whose only ground another hunter holds; calls %v\n%s", f.calls, log.String())
	}
	if !strings.Contains(log.String(), "claimed by other hunters") {
		t.Errorf("the skip must be logged, got:\n%s", log.String())
	}
}
