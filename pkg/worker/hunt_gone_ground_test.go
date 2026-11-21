package worker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestHuntGoneGround(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("transit to x: travel to x failed: timeout"), ""},
		{errors.New("transit to smelt_reef_frostfeld: travel to smelt_reef_frostfeld failed: Unknown destination: smelt_reef_frostfeld"), "smelt_reef_frostfeld"},
		{errors.New("Unknown destination: charged_reef_trappist_1; held"), "charged_reef_trappist_1"},
	} {
		if got := huntGoneGround(tc.err); got != tc.want {
			t.Errorf("huntGoneGround(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// huntGoneTravelFake refuses travel to one POI the way the server refuses a
// POI id that no longer exists.
type huntGoneTravelFake struct {
	game.GameClient
	f       *huntFake
	gone    string
	travels []string
}

func (w *huntGoneTravelFake) Travel(ctx context.Context, poi string) (*game.TravelResult, error) {
	w.travels = append(w.travels, poi)
	if poi == w.gone {
		return nil, errors.New("Unknown destination: " + poi)
	}
	return w.f.Travel(ctx, poi)
}

// Observed live on pirate-13, 2026-11-17: the KB still listed
// smelt_reef_frostfeld, a belt the server renamed in October. It outranked
// every real belt, travel answered "Unknown destination", and the pass ended —
// every pass, for a day. The sweep must move on to the next ground, and later
// passes must not fly back out to the dead id.
func TestHuntSkipsAGroundTheServerNoLongerKnows(t *testing.T) {
	t.Cleanup(huntGone.reset)
	const stale = "smelt_reef_pollux"
	pass := func() (*huntGoneTravelFake, string) {
		f := newHuntFake(t, huntFakeOpts{
			board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
			creatures: []huntCreature{{id: "g1"}},
		})
		w := &huntGoneTravelFake{GameClient: f, f: f, gone: stale}
		var log strings.Builder
		deps := huntDeps(f, &log)
		deps.Client = w
		_ = deps.KB.RememberPOI(context.Background(), knowledge.POI{
			ID: stale, SystemID: "pollux", Type: "asteroid_belt",
			Resources: []game.POIResource{{ResourceID: "iron_ore", Richness: 40, Remaining: 200000}},
		})
		if err := Hunt(context.Background(), deps); err != nil {
			t.Fatalf("Hunt: %v", err)
		}
		return w, log.String()
	}

	w, log := pass()
	if !slices.Contains(w.travels, stale) {
		t.Fatalf("fixture: the stale belt must be the first choice, travels %v\n%s", w.travels, log)
	}
	if !slices.Contains(w.travels, "pollux_outer_belt") || w.f.huntCalls == 0 {
		t.Fatalf("must move on to the real belt and hunt there; travels %v, hunts %d\n%s", w.travels, w.f.huntCalls, log)
	}
	if !strings.Contains(log, "no longer exists") {
		t.Errorf("the dead ground must be logged, got:\n%s", log)
	}

	w, log = pass()
	if slices.Contains(w.travels, stale) {
		t.Errorf("a later pass flew to the dead ground again; travels %v\n%s", w.travels, log)
	}
	if w.f.huntCalls == 0 {
		t.Errorf("the later pass must still hunt the real belt\n%s", log)
	}
}
