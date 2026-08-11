package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// The pirate BASELINE, not reputation, is the durable record that chain-2
// mission 1 was completed: reputation floats with activity and decays back
// toward baseline, so a high reputation on a default baseline is a temporary
// state, not an unlock. Everything starts at -30/-30; an_introduction raises
// the baseline to 10.
func TestSmugglingUnlockedReadsBaselineNotReputation(t *testing.T) {
	standing := func(rep, base int) *game.State {
		return &game.State{Player: game.Player{
			Standings: map[string]game.EmpireStanding{
				"pirate_voss": {Reputation: rep, Baseline: base},
				"solarian":    {Reputation: 20, Baseline: 20},
			},
		}}
	}
	// Prove the baseline is what is read: if the predicate keyed on reputation
	// it would pass the "high rep on a default baseline" case below.
	for _, tc := range []struct {
		name  string
		state *game.State
		want  bool
	}{
		{"default hostile agent", standing(-30, -30), false},
		{"unlocked, rep sitting at baseline", standing(10, 10), true},
		{"unlocked and earning above baseline", standing(29, 10), true},
		{"high rep on a default baseline is NOT the unlock", standing(45, -30), false},
		{"nil state", nil, false},
		{"no standings yet (fresh worker)", &game.State{}, false},
		{"standings present, no pirate block", &game.State{Player: game.Player{
			Standings: map[string]game.EmpireStanding{"solarian": {Reputation: 20, Baseline: 20}},
		}}, false},
	} {
		if got := smugglingUnlocked(tc.state); got != tc.want {
			t.Errorf("%s: smugglingUnlocked = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// pirateStrongholds is the live standings key set: the server retired the single
// generic "pirates" block and now sends one per stronghold, because each crew
// keeps its own books. pkg/assets was corrected for this on 2026-08-07 (a leader
// at reputation 16-17 with all nine crews read as "baseline 0, needs 10");
// pkg/worker kept reading the retired key, so smuggllingUnlocked could never
// return true for any agent, on any board, ever.
var pirateStrongholds = []string{
	"pirate_crix", "pirate_dross", "pirate_kael", "pirate_korr", "pirate_mera",
	"pirate_nyx", "pirate_sable", "pirate_thane", "pirate_voss",
}

// The live wire shape, verified against data/assets.db on 2026-08-11: 122 agents
// x 9 pirate_* keys, and ZERO rows under "pirates". A fixture carrying the
// retired key is why this stayed green while production could not unlock.
func TestSmugglingUnlockedReadsTheLiveStrongholdKeys(t *testing.T) {
	all := func(base int) map[string]game.EmpireStanding {
		m := map[string]game.EmpireStanding{"solarian": {Reputation: 20, Baseline: 20}}
		for _, k := range pirateStrongholds {
			m[k] = game.EmpireStanding{Reputation: base, Baseline: base}
		}
		return m
	}
	unlocked := &game.State{Player: game.Player{Standings: all(pirateUnlockBaseline)}}
	if !smugglingUnlocked(unlocked) {
		t.Error("an agent at baseline 10 with all nine stronghold keys reads as locked")
	}
	hostile := &game.State{Player: game.Player{Standings: all(-30)}}
	if smugglingUnlocked(hostile) {
		t.Error("an agent at the -30 hostile default reads as unlocked")
	}
	// The retired key alone must NOT satisfy the gate: if it did, this whole
	// class of drift would go unnoticed again the next time a key is renamed.
	retired := &game.State{Player: game.Player{Standings: map[string]game.EmpireStanding{
		"pirates": {Reputation: 10, Baseline: pirateUnlockBaseline},
	}}}
	if smugglingUnlocked(retired) {
		t.Error("the retired \"pirates\" key still satisfies the unlock gate")
	}
	// One crew is enough. Standings are per-stronghold and an_introduction is
	// granted by one giver, so the unlock arrives at one crew first; requiring
	// all nine would report a genuinely unlocked agent as locked.
	one := &game.State{Player: game.Player{Standings: map[string]game.EmpireStanding{
		"pirate_kael": {Reputation: 10, Baseline: pirateUnlockBaseline},
		"pirate_voss": {Reputation: -30, Baseline: -30},
	}}}
	if !smugglingUnlocked(one) {
		t.Error("a single crew at baseline 10 reads as locked")
	}
}

// The XP total is what a climbing worker's trip line leads with, so it must
// survive the shapes a board actually produces: several missions batched to one
// destination, entries with no rewards block at all, and multi-skill awards.
func TestTripSkillXPTotalsAcrossTheBatch(t *testing.T) {
	withXP := func(m map[string]int) missionCandidate {
		return missionCandidate{Entry: serverapi.MissionBoardEntry{
			Rewards: &serverapi.MissionRewards{Credits: 2000, SkillXP: m},
		}}
	}
	trip := []missionCandidate{
		withXP(map[string]int{"smuggling": 250}),
		withXP(map[string]int{"smuggling": 250}),
		{Entry: serverapi.MissionBoardEntry{}},                                     // no rewards block
		{Entry: serverapi.MissionBoardEntry{Rewards: &serverapi.MissionRewards{}}}, // rewards, no XP
		withXP(map[string]int{"smuggling": 75, "nebula_attunement": 35}),
	}
	// SMUGGLING ONLY. This total decides whether the trip is framed as bought
	// for XP, and that framing is a claim about the smuggling climb — the 35
	// nebula_attunement buys nothing toward it.
	if got := tripSkillXP(trip); got != 575 {
		t.Errorf("tripSkillXP = %d, want 575 (250+250+75; nebula_attunement excluded)", got)
	}
	if got := tripSkillXP(nil); got != 0 {
		t.Errorf("tripSkillXP(nil) = %d, want 0", got)
	}
	// A credits-only trip must fall through to the plain line, not the XP one.
	if got := tripSkillXP([]missionCandidate{{Entry: serverapi.MissionBoardEntry{
		Rewards: &serverapi.MissionRewards{Credits: 5000},
	}}}); got != 0 {
		t.Errorf("credits-only trip reported %d XP, want 0", got)
	}
	// The live mis-framing: unmarked_cargo pays navigation+trading and NO
	// smuggling, yet was advertised as "+45 XP" on a level-0 smuggling canary,
	// which is what sent me chasing a bootstrap path that does not exist.
	if got := tripSkillXP([]missionCandidate{withXP(map[string]int{"navigation": 20, "trading": 25})}); got != 0 {
		t.Errorf("a trip with no smuggling XP reported %d, want 0", got)
	}
}
