package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// TestPayoutRatioDiscountsTheGateNet pins that the gate scores a candidate on
// what the empire actually pays, not on the advertised reward.
//
// The treasury stopped being replenished on 2026-07-23 and payouts decayed to
// ~37% of advertised while still awarding full skill XP. Priced at face value
// the gate happily takes a 27-jump run advertised at 2000 that settles for 370
// against ~600 of fuel.
func TestPayoutRatioDiscountsTheGateNet(t *testing.T) {
	dist := map[string]int{"sol": 1}
	ask := func(string) (float64, bool) { return 1, true } // 10 units at 1cr: keeps the reward term dominant
	fuel := func(jumps int) float64 { return 900 }
	e := boardEntry("m1", "steel", 10, "sol_station", "sol", 2000, 1000)

	// At face value 2000 - 10 - 900 = 1090, comfortably over missionMinNet.
	if _, reason := buildMissionCandidate(e, dist, ask, fuel, false, 1, missionMinNet, 6, 1); reason != "" {
		t.Fatalf("at face value this must pass the gate, got refusal: %s", reason)
	}
	// At the observed 0.37 the real net is 740 - 10 - 900 = -170: a losing run.
	if _, reason := buildMissionCandidate(e, dist, ask, fuel, false, 1, missionMinNet, 6, 0.37); reason == "" {
		t.Fatal("discounted below the floor, the gate must refuse the run")
	}
}

// TestSmugglingBuyingXPBelowLevel3 pins the exemption: only agents still
// climbing to L3 ignore realized economics on smuggling, because XP is paid in
// full and the level is the objective. At L3 the reason to overpay is gone.
func TestSmugglingBuyingXPBelowLevel3(t *testing.T) {
	st := func(level int) *game.State {
		s := &game.State{}
		s.Player.Skills = map[string]game.Skill{skillSmuggling: {Level: level}}

		return s
	}
	for _, tc := range []struct {
		level int
		want  bool
	}{{0, true}, {1, true}, {2, true}, {3, false}, {7, false}} {
		if got := smugglingBuyingXP(st(tc.level)); got != tc.want {
			t.Errorf("smuggling level %d: buying XP = %v, want %v", tc.level, got, tc.want)
		}
	}
	// No player payload yet must read as still-climbing: a fresh worker should
	// not have money rules applied to it on the strength of missing data.
	if !smugglingBuyingXP(nil) {
		t.Error("nil state must read as still climbing")
	}
	if !smugglingBuyingXP(&game.State{}) {
		t.Error("absent skills must read as still climbing")
	}
}
