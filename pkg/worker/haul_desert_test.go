package worker

import (
	"context"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// A hauler parked where the board has nothing inside DefaultHaulMaxJumps cannot
// earn its way out: every candidate is beyond the radius that lets it see
// anything at all, so it idles indefinitely. Observed 2026-09-20 with 8 of 16
// haulers showing an empty hold while 111 opportunities sat unclaimed, and
// "haul: no opportunities within 5 jumps; idling" 415 times in one log slice.
// HaulAnyDistanceNet only rescues the fat tier; an ordinary board strands them.
//
// The escape is time-based: hold the tight radius while it is plausibly just a
// lull, then widen in steps until the hauler can see work again.
func TestDesertRadius_HoldsTheTightRadiusThroughAShortLull(t *testing.T) {
	for _, passes := range []int{0, 1, HaulDesertPatience - 1} {
		if got := desertRadius(passes, 5); got != 5 {
			t.Errorf("desertRadius(%d, 5) = %d, want 5 -- a short lull must not scatter the fleet", passes, got)
		}
	}
}

func TestDesertRadius_WidensInStepsOnceStuck(t *testing.T) {
	tests := []struct {
		passes int
		want   int
	}{
		{HaulDesertPatience, 10},
		{HaulDesertPatience*2 - 1, 10},
		{HaulDesertPatience * 2, 20},
		{HaulDesertPatience * 3, 40},
	}
	for _, tt := range tests {
		if got := desertRadius(tt.passes, 5); got != tt.want {
			t.Errorf("desertRadius(%d, 5) = %d, want %d", tt.passes, got, tt.want)
		}
	}
}

// The widening stops at the hard backstop. Past it the radius is meaningless --
// nothing survives the HaulMaxHaulJumps drop anyway -- and an unbounded doubling
// would silently disable the cap.
func TestDesertRadius_StopsAtTheHardBackstop(t *testing.T) {
	for _, passes := range []int{HaulDesertPatience * 4, HaulDesertPatience * 20, 100000} {
		got := desertRadius(passes, 5)
		if got != HaulMaxHaulJumps {
			t.Errorf("desertRadius(%d, 5) = %d, want the %d backstop", passes, got, HaulMaxHaulJumps)
		}
	}
}

// A non-default base must escalate from where the operator set it, not from 5.
func TestDesertRadius_EscalatesFromTheConfiguredBase(t *testing.T) {
	if got := desertRadius(HaulDesertPatience, 3); got != 6 {
		t.Errorf("desertRadius(patience, 3) = %d, want 6", got)
	}
	// A base already at or past the backstop has nowhere to widen to.
	if got := desertRadius(HaulDesertPatience*3, HaulMaxHaulJumps); got != HaulMaxHaulJumps {
		t.Errorf("got %d, want %d", got, HaulMaxHaulJumps)
	}
}

// The counter is what turns a radius into a behaviour: it must count only
// CONSECUTIVE dry passes, so a single claim puts the hauler back on the tight
// radius rather than leaving it roaming the galaxy forever.
func TestHaulDesert_CountsConsecutiveDryPassesAndResetsOnWork(t *testing.T) {
	d := &haulDesert{}
	for range HaulDesertPatience {
		d.dry()
	}
	if got := d.radius(5); got != 10 {
		t.Fatalf("radius after %d dry passes = %d, want 10", HaulDesertPatience, got)
	}
	d.worked()
	if got := d.radius(5); got != 5 {
		t.Errorf("radius after a claim = %d, want the tight 5 back", got)
	}
}

// nil is the disabled case: every fleet that is not haul, and every existing
// test, must keep exactly today's fixed radius.
func TestHaulDesert_NilKeepsTheFixedRadius(t *testing.T) {
	var d *haulDesert
	d.dry()
	d.worked()
	if got := d.radius(5); got != 5 {
		t.Errorf("nil radius = %d, want the unchanged 5", got)
	}
}

// End-to-end: a hauler parked 6 jumps from the only buy station sees nothing at
// the default radius and idles. That is correct for a lull and wrong forever --
// the three 1900-cargo congregations sat at player stations with only occasional
// demand and simply stopped working. After HaulDesertPatience dry passes the
// radius widens and the same opportunity becomes claimable.
func TestHaul_DesertEscapeClaimsBeyondTheDefaultRadius(t *testing.T) {
	// a -1- b -2- c -3- d -4- e -5- f -6- g(buy) -7- h(sell)
	chain := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	var edges [][2]string
	for i := 0; i+1 < len(chain); i++ {
		edges = append(edges, [2]string{chain[i], chain[i+1]})
	}
	systems := make([]knowledge.System, 0, len(chain))
	for _, s := range chain {
		systems = append(systems, knowledge.System{ID: s})
	}
	newDeps := func(d *haulDesert) (HaulDeps, *fakeStore) {
		f := &fakeStore{
			available: []market.ArbitrageOpportunity{opp(1, "g", "h", 100)},
			claims:    map[int]bool{1: true},
			admitOK:   true,
		}
		fc := &fakeClient{state: &game.State{System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100}}
		kb := &fakeKB{systems: systems, conns: undirected(edges...)}
		return HaulDeps{Client: fc, KB: kb, Market: f, AgentID: "trader-x", Out: io.Discard, Desert: d}, f
	}

	// Baseline: the buy station is 6 jumps out, past DefaultHaulMaxJumps.
	deps, f := newDeps(nil)
	if err := Haul(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if len(f.admitCalls) != 0 {
		t.Fatalf("a 6-jump opportunity must be out of reach at the default radius, got %d claims", len(f.admitCalls))
	}

	// Same board, same position, after the drought.
	d := &haulDesert{}
	for range HaulDesertPatience {
		d.dry()
	}
	deps, f = newDeps(d)
	if err := Haul(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if len(f.admitCalls) == 0 {
		t.Fatal("after the patience window the widened radius must reach the 6-jump buy station")
	}
}

// The drought counter must actually advance from inside the step, or the escape
// never triggers in production however correct desertRadius is.
func TestHaul_DryPassAdvancesTheDroughtCounter(t *testing.T) {
	f := &fakeStore{available: nil}
	fc := &fakeClient{state: &game.State{System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100}}
	kb := &fakeKB{systems: []knowledge.System{{ID: "a"}}, conns: undirected()}
	d := &haulDesert{}
	if err := Haul(context.Background(), HaulDeps{Client: fc, KB: kb, Market: f, AgentID: "trader-x", Out: io.Discard, Desert: d}); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	n := d.dryN
	d.mu.Unlock()
	if n != 1 {
		t.Errorf("dry passes = %d, want 1 after an empty board", n)
	}
}

// Widening the radius lets a stranded hauler SEE distant work but leaves it in
// the dead region; the durable fix is to move it somewhere dense. Empire
// capitals and their neighbours nearly always have something trading, so the
// second stage of the escape relocates there.
func TestNearestCapital_PicksTheClosestOne(t *testing.T) {
	// krynn -1- x -2- y, and haven 4 hops the other way.
	g := navigation.JumpGraphFromConnections(undirected(
		[2]string{"y", "x"}, [2]string{"x", "krynn"},
		[2]string{"y", "p"}, [2]string{"p", "q"}, [2]string{"q", "r"}, [2]string{"r", "haven"},
	))
	caps := []string{"krynn", "haven", "sol", "nexus_prime"}
	got, hops, ok := nearestCapital(g, "y", caps)
	if !ok || got != "krynn" || hops != 2 {
		t.Errorf("nearestCapital = %q/%d/%v, want krynn/2/true", got, hops, ok)
	}
}

// Standing in a capital is not a desert problem the relocation can solve, and
// autopiloting to where you already are burns a pass for nothing.
func TestNearestCapital_IgnoresTheCapitalYouAreStandingIn(t *testing.T) {
	g := navigation.JumpGraphFromConnections(undirected([2]string{"sol", "x"}, [2]string{"x", "krynn"}))
	caps := []string{"sol", "krynn"}
	got, hops, ok := nearestCapital(g, "sol", caps)
	if !ok || got != "krynn" || hops != 2 {
		t.Errorf("nearestCapital from sol = %q/%d/%v, want krynn/2/true", got, hops, ok)
	}
}

func TestNearestCapital_ReportsNoneWhenUnreachable(t *testing.T) {
	g := navigation.JumpGraphFromConnections(undirected([2]string{"a", "b"}))
	caps := []string{"sol", "krynn"}
	if got, _, ok := nearestCapital(g, "a", caps); ok {
		t.Errorf("nearestCapital = %q, want none reachable", got)
	}
}

// Staging: the free option (a wider radius) gets its window first, and only a
// hauler still dry after that spends fuel crossing the map.
func TestDesertRelocationIsDueOnlyAfterTheWideningWindow(t *testing.T) {
	if relocationDue(HaulDesertPatience) {
		t.Error("relocation must not fire while the cheap widening has not had its window")
	}
	if !relocationDue(HaulDesertRelocate) {
		t.Error("relocation must fire once the widening window has passed")
	}
	if !relocationDue(HaulDesertRelocate * 3) {
		t.Error("relocation must keep firing while the drought continues")
	}
}

// The capital list is read off the map, not hardcoded: exactly the five
// police_level-100 systems, one per empire. A hardcoded list silently rots when
// an empire's capital moves.
func TestCapitalSystems_SelectsThePoliceLevel100Systems(t *testing.T) {
	systems := []knowledge.System{
		{ID: "sol", PoliceLevel: 100},
		{ID: "haven", PoliceLevel: 100},
		{ID: "ironhearth", PoliceLevel: 80},
		{ID: "dross", PoliceLevel: 55},
		{ID: "nowhere", PoliceLevel: 0},
	}
	got := capitalSystems(systems)
	if len(got) != 2 || got[0] != "sol" || got[1] != "haven" {
		t.Errorf("capitalSystems = %v, want [sol haven]", got)
	}
}
