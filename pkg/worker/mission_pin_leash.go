package worker

// missionPinLeashJumps is how far from its pin a still-locked worker may be
// sent by a non-chain mission.
//
// One jump, not zero: the board at the pin frequently offers work whose first
// objective is the pin's own neighbour, and refusing everything would leave the
// agent idle for no gain. One hop is close enough that the next dry pass walks
// it straight back to the giver.
const missionPinLeashJumps = 1

// missionPinLeashRejects reports whether a pinned, still-locked worker should
// refuse a mission for taking it too far from its pin.
//
// The pirate unlock is granted by one chain sold at ONE station. A worker that
// lacks it has exactly one job while pinned there: be present when
// `a_word_in_private` — the only entry that is not gated behind smuggling
// level 1 — rotates onto that board. Every other mission on offer grants no
// smuggling XP, pays a fraction of face value, and lands the worker somewhere
// new to accept another.
//
// Measured on wave 1 (2026-09-14): six workers reached the giver, then drifted
// to 16, 12, 11, 9, 7 and 3 jumps away over the following two hours, none of
// them any closer to the unlock. The fleet count stayed at 48 of 172.
//
// Three cases are deliberately NOT leashed:
//
//   - smuggling missions, which ARE the chain and legitimately run long
//     (engineer-5's Across the Line was 17 jumps);
//   - workers that already hold the unlock, which have no reason to sit at the
//     giver and would otherwise idle there exactly as parked graduates did;
//   - an unmeasurable distance (jumpsFromPin < 0), because a missing BFS entry
//     means we could not measure the route, not that it is far. Refusing on
//     absent data would strand a worker whose graph is incomplete.
func missionPinLeashRejects(pinned, unlocked bool, missionType string, jumpsFromPin int) bool {
	if !pinned || unlocked {
		return false
	}
	if missionType == missionTypeSmuggling {
		return false
	}
	if jumpsFromPin < 0 {
		return false
	}

	return jumpsFromPin > missionPinLeashJumps
}
