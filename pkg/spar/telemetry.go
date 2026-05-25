package spar

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// formatTickRow renders one participant's per-tick battle state.
func formatTickRow(tick int64, p game.BattleParticipant) string {
	return fmt.Sprintf("tick %d | %-12s | zone=%-7s | stance=%-6s | hull %3d%% | shield %3d%%",
		tick, p.Username, p.Zone, p.Stance, p.HullPct, p.ShieldPct)
}

// formatSummary renders a combatant's end-of-match ship state.
func formatSummary(username string, st *game.State) string {
	hullPct := 0
	if st.MaxHull > 0 {
		hullPct = int(st.Hull / st.MaxHull * 100)
	}
	status := "alive"
	if st.Hull <= 0 {
		status = "DESTROYED"
	}
	return fmt.Sprintf("  %-12s | hull %.0f/%.0f (%d%%) | %s", username, st.Hull, st.MaxHull, hullPct, status)
}
