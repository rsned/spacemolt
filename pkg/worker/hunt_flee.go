package worker

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// A hunter is exempt from the standing battle gate (BattleGateClient) because
// it fights on purpose, so the gate's "caught in a fight -> flee" never runs
// for it. huntPolicy covers only the engagements the pass opens itself; these
// helpers cover everything else: a fight the hunter is pulled into at pass
// start, between engagements, or on a transit/dock leg. The pass sends the
// flee stance and ends; the next pass starts clean or flees again.

// huntInBattleErr reports whether err is the server refusing a command because
// the ship is in a battle.
func huntInBattleErr(err error) bool {
	if err == nil || notInBattle(err) {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "in_battle") || strings.Contains(m, "in battle") || strings.Contains(m, "in a battle")
}

// huntEscapeBattle sends the flee stance when the ship is in a battle the
// pass is not driving: the cached InBattle flag (battle_started / battle_update
// pushes) or cause, an error refused in_battle. It returns true when the ship
// was in a battle, and the caller must end the pass. A flee refused
// not-in-battle means the flag was stale: nothing to escape, false.
func huntEscapeBattle(ctx context.Context, deps HuntDeps, out io.Writer, who, when string, cause error) bool {
	st := deps.Client.GetState()
	if (st == nil || !st.InBattle) && !huntInBattleErr(cause) {
		return false
	}
	where := "an unknown POI"
	if st != nil {
		where = nonEmpty(st.CurrentPOI, st.System.ID)
	}
	if err := deps.Client.Battle(ctx, "stance", map[string]any{"stance": huntStanceFlee}); err != nil {
		if notInBattle(err) {
			fmt.Fprintf(out, "hunt[%s]: in-battle flag was stale %s (%v)\n", who, when, err) //nolint:errcheck
			return false
		}
		fmt.Fprintf(out, "hunt[%s]: CAUGHT IN A BATTLE %s at %s; flee stance failed: %v; pass ends\n", who, when, where, err) //nolint:errcheck
		return true
	}
	fmt.Fprintf(out, "hunt[%s]: CAUGHT IN A BATTLE %s at %s; fleeing, pass ends\n", who, when, where) //nolint:errcheck
	publishActivity(deps.SetActivity, "Hunt · fleeing an unstarted battle")
	return true
}
