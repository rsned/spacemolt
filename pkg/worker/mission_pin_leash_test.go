package worker

import "testing"

// Wave 1, 2026-09-14: six agents were pinned to treasure_cache_trading_post,
// the only station selling the chain's entry mission. All six reached it. Then
// every one drifted steadily AWAY — explorer-5 ended 16 jumps out, explorer-4
// 12, engineer-6 11 — because the only missions that board offers a locked
// agent are multi-stop deliveries, and each one lands it somewhere new to take
// another.
//
// Nothing on that board helps a locked L0 agent: smuggling work is gated at
// level 1, and delivery work grants no smuggling XP at all. So the drift buys
// nothing while costing the one thing that matters — being present when
// `a_word_in_private` rotates in. That is why the count sat at 48 of 172.
//
// The leash keeps a pinned, still-locked agent within reach of its pin. It must
// NOT apply to smuggling missions: those ARE the chain, and they legitimately
// run long (engineer-5's Across the Line was 17 jumps).
func TestMissionPinLeash(t *testing.T) {
	for _, tc := range []struct {
		name        string
		pinned      bool
		unlocked    bool
		missionType string
		jumpsFromPin int
		wantReject  bool
	}{
		{"locked agent, far delivery, rejected", true, false, "delivery", 4, true},
		{"locked agent, near delivery, allowed", true, false, "delivery", 1, false},
		{"locked agent, same system, allowed", true, false, "delivery", 0, false},
		// The chain itself must never be leashed.
		{"locked agent, far SMUGGLING, allowed", true, false, "smuggling", 17, false},
		// A graduate has no reason to sit at the giver.
		{"unlocked agent, far delivery, allowed", true, true, "delivery", 12, false},
		// An unpinned worker has no pin to be leashed to.
		{"unpinned agent, far delivery, allowed", false, false, "delivery", 12, false},
		// An unknown distance must not silently reject: a missing BFS entry
		// means we could not measure, not that the destination is far.
		{"locked agent, unmeasurable distance, allowed", true, false, "delivery", -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := missionPinLeashRejects(tc.pinned, tc.unlocked, tc.missionType, tc.jumpsFromPin)
			if got != tc.wantReject {
				t.Errorf("missionPinLeashRejects(pinned=%v, unlocked=%v, %s, %d jumps) = %v, want %v",
					tc.pinned, tc.unlocked, tc.missionType, tc.jumpsFromPin, got, tc.wantReject)
			}
		})
	}
}

// The leash must release the moment an agent graduates, or a fresh graduate
// would sit at the giver with nothing left to win — the exact waste this
// campaign already documented for parked graduates.
func TestMissionPinLeash_ReleasesOnGraduation(t *testing.T) {
	const far = 9
	if !missionPinLeashRejects(true, false, "delivery", far) {
		t.Fatal("a locked pinned agent was not leashed")
	}
	if missionPinLeashRejects(true, true, "delivery", far) {
		t.Error("the leash survived graduation")
	}
}
