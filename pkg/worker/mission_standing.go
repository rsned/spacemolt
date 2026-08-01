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

// smugglingXPExemptLevel is the smuggling level at which an agent stops being
// treated as "buying XP" and starts being held to normal economics.
//
// Below it the agent's whole purpose is to reach L3, which is what unlocks the
// chain-2 reputation mission; credits are incidental and a run that loses money
// is still a run that bought a level. At or above it there is nothing left to
// buy, so smuggling is judged on realized economics like every other category.
const smugglingXPExemptLevel = 3

// smugglingBuyingXP reports whether this agent should still ignore realized
// payout economics on smuggling runs.
//
// The empire treasury ran dry on 2026-07-23 and mission credits decayed to
// ~37% of advertised (server-side, confirmed by the operator: new money
// generation was disabled). Realized-ratio gating discounts advertised rewards
// so the fleet stops flying 27-jump runs for IOUs — but a sub-L3 agent must
// keep flying them anyway, because XP is still paid in FULL and the level is
// the objective. Their credit spend is bounded separately by
// missionSmugglingXPBudget, not by the ratio.
//
// Unknown skills (no full player payload yet) read as still-climbing, matching
// smugglingUnlocked's forgiving default.
func smugglingBuyingXP(st *game.State) bool {
	if st == nil || st.Player.Skills == nil {
		return true
	}

	return st.Player.Skills[skillSmuggling].Level < smugglingXPExemptLevel
}
