package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
)

// attackOrRetarget sends `attack` outside a battle and `battle target` inside
// one.
//
// Inside a battle a repeat attack never fires an extra volley: against a
// player it only re-points the target, and against a pirate it re-applies the
// reputation penalty and summons that stronghold's pirates again (skill.md,
// "Do not re-issue attack"). Live 2026-11-21 it also got no terminal reply, so
// the REPL waited out the 3-tick timeout holding its command lock, and
// reload and stance typed meanwhile queued behind it. `battle target` is
// applied at the next battle tick and replies at once.
//
// A stale in-battle flag (the fight already over) gets not_in_battle back, and
// the attack then goes out as asked.
func attackOrRetarget(ctx context.Context, client game.GameClient, targetID string, out io.Writer) error {
	if st := client.GetState(); st != nil && st.InBattle {
		err := client.Battle(ctx, "target", map[string]any{"target_id": targetID})
		var se *game.ServerError
		if !errors.As(err, &se) || se.Code != "not_in_battle" {
			if err == nil {
				fmt.Fprintf(out, "In battle: retargeted to %s (battle target) instead of re-issuing attack\n", targetID) //nolint:errcheck
			}
			return err
		}
	}
	return client.Attack(ctx, targetID)
}
