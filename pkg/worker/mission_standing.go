package worker

import "github.com/rsned/spacemolt/pkg/game"

const (
	// pirateFactionID keys the pirate block in Player.Standings.
	pirateFactionID = "pirates"
	// pirateUnlockBaseline is the pirate BASELINE an agent holds once chain-2
	// mission 1 (`an_introduction`) has been completed. Every agent starts at
	// -30 (the only hostile default); the mission raises the baseline to 10,
	// and because reputation only ever decays back toward baseline, that is
	// the durable "has done the chain and holds the reputation" signal.
	// Reputation alone is not: it floats and decays.
	pirateUnlockBaseline = 10
)

// smugglingUnlocked reports whether this agent has completed chain-2 mission 1
// and holds the pirate standing it grants.
//
// It selects the agent's smuggling policy:
//   - false — still climbing. Smuggling is an XP PURCHASE: the relaxed floor
//     and the bare-jump-time expiry allowance stay, because levels are the
//     payload and a late delivery still awards XP. Credit estimates for these
//     runs are not trustworthy and must be reported as such.
//   - true — the unlock is banked, so the agent is running couriers for money
//     and its results are the sample we use to settle the reward curve.
//
// Unknown standings (no full player payload yet) read as NOT unlocked, which
// keeps a fresh worker in the forgiving mode rather than silently applying
// money rules to an agent that is still climbing.
func smugglingUnlocked(st *game.State) bool {
	if st == nil || st.Player.Standings == nil {
		return false
	}
	s, ok := st.Player.Standings[pirateFactionID]

	return ok && s.Baseline >= pirateUnlockBaseline
}
