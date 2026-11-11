package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// A worker caught in a battle it did not start used to keep issuing its
// ordinary commands, each refused with in_battle, until the ship died:
// explorer-8 lost its survey vessel to wildlife at Oxbow Belt B on 2026-11-11,
// retrying "jump Chertan" for two minutes without once trying to escape. Only
// the hunt role ever sent a flee stance. The gate below runs in front of every
// standing/scheduled/task command for every other role.

// ErrFleeingBattle aborts the command (and enclosing loops) while the ship is
// trying to escape a battle. Loop-fatal: every command but a battle action is
// refused in combat, so retrying the loop body only burns the flee window.
var ErrFleeingBattle = errors.New("battle: fleeing")

// battleStanceFlee is the battle stance that disengages (several ticks of
// flee progress; `battle retreat` never exits a fight).
const battleStanceFlee = "flee"

// BattleClient is what the gate needs to send the flee stance.
type BattleClient interface {
	Battle(ctx context.Context, action string, payload map[string]any) error
}

// isBattleCommand reports whether tokens are themselves a combat action, which
// the gate must let through.
func isBattleCommand(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	switch strings.ToLower(tokens[0]) {
	case "battle", "attack", "flee", "hunt", "spar":
		return true
	}
	return false
}

// battleGate sends the flee stance and refuses the command when the ship is
// in a battle. nil when the ship is not in battle, the command is a combat
// action, or the caller opted out (nil client).
func battleGate(ctx context.Context, out io.Writer, st *game.State, tokens []string, bc BattleClient) error {
	if bc == nil || st == nil || !st.InBattle || isBattleCommand(tokens) {
		return nil
	}
	hull := ""
	if st.Ship.MaxHull > 0 {
		hull = fmt.Sprintf(", hull %.0f/%.0f", st.Ship.Hull, st.Ship.MaxHull)
	}
	if err := bc.Battle(ctx, "stance", map[string]any{"stance": battleStanceFlee}); err != nil {
		// The server saying we are not in a battle means our InBattle flag
		// is stale (a battle_ended missed across a reconnect): run the
		// command rather than hold the worker forever on a fight that ended.
		if notInBattle(err) {
			fmt.Fprintf(out, "⚔️  battle: in-battle flag was stale (%v); running %q\n", err, strings.Join(tokens, " ")) //nolint:errcheck
			return nil
		}
		fmt.Fprintf(out, "⚔️  battle: in combat at %s%s — flee stance failed: %v; holding %q\n", //nolint:errcheck
			nonEmpty(st.CurrentPOI, st.CurrentSystem), hull, err, strings.Join(tokens, " "))
	} else {
		fmt.Fprintf(out, "⚔️  battle: in combat at %s%s — fleeing; holding %q\n", //nolint:errcheck
			nonEmpty(st.CurrentPOI, st.CurrentSystem), hull, strings.Join(tokens, " "))
	}

	return fmt.Errorf("%w (at %s)", ErrFleeingBattle, nonEmpty(st.CurrentPOI, st.CurrentSystem))
}

// BattleGateClient arms the standing battle gate (flee when caught in combat)
// for every role except hunt, which fights on purpose and runs its own
// flee-at-hull policy.
func BattleGateClient(role string, c BattleClient) BattleClient {
	if role == "hunt" {
		return nil
	}

	return c
}

// notInBattle reports whether err is the server refusing a battle action
// because the ship is in no battle.
func notInBattle(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "not_in_battle") || strings.Contains(m, "not in a battle") || strings.Contains(m, "not in battle")
}
