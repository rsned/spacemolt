package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/battlereplay"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// attackCaptureBattleBudget bounds one pass. Each battle costs a paged
// get_battle_log plus a summary, and every page waits for the reply to settle,
// so an unbounded drain would hold the worker for minutes. The queue is
// derived, not consumed, so whatever is left over is simply picked up next pass.
const attackCaptureBattleBudget = 5

// attackQueue is the KB surface this command needs, kept narrow so a worker
// running without a SQLite KB degrades to a no-op instead of failing.
type attackQueue interface {
	BattlesNeedingAttackCapture(ctx context.Context, limit int) ([]knowledge.BattleToCapture, error)
}

// CaptureWildlifeAttacks records what creatures shoot with, from the logs of
// battles already fought.
//
// With a battle id in args it captures that battle alone, which is how a
// specific fight — a death worth understanding — gets read on demand. With no
// args it drains the derived queue: battles containing a recorded kill for which
// wildlife_attacks holds nothing.
//
// This is the only path to a species' damage type, and a damage type is what a
// resistance fit is chosen against. Nothing else on the wire carries it:
// get_nearby gives hull and role, scan gives a danger phrase.
func (d *WorkerDispatch) CaptureWildlifeAttacks(ctx context.Context, args []string) error {
	if d.KB == nil || d.Client == nil {
		return nil
	}

	if len(args) > 0 && args[0] != "" {
		n, err := battlereplay.CaptureWildlifeAttacks(ctx, d.KB, d.Client, args[0], nil, d.logf)
		if err != nil {
			return err
		}
		fmt.Fprintf(d.Out, "wildlife attacks: battle %s -> %d rows\n", args[0], n) //nolint:errcheck

		return nil
	}

	q, ok := d.KB.(attackQueue)
	if !ok {
		return nil
	}
	battles, err := q.BattlesNeedingAttackCapture(ctx, attackCaptureBattleBudget)
	if err != nil {
		return err
	}
	if len(battles) == 0 {
		return nil
	}

	var (
		rows  int
		fails int
	)
	for _, b := range battles {
		n, err := battlereplay.CaptureWildlifeAttacks(ctx, d.KB, d.Client, b.BattleID, b.SpeciesByCreatureID, d.logf)
		if err != nil {
			// One unreadable battle must not strand the rest of the queue: a log
			// can be too old to fetch, and that battle would otherwise be retried
			// first on every future pass forever.
			fails++
			fmt.Fprintf(d.Out, "wildlife attacks: battle %s: %v\n", b.BattleID, err) //nolint:errcheck

			continue
		}
		rows += n
	}
	fmt.Fprintf(d.Out, "wildlife attacks: %d battles -> %d rows (%d failed)\n", //nolint:errcheck
		len(battles), rows, fails)

	return nil
}

// logf adapts the dispatcher's output to the fetcher's progress sink.
func (d *WorkerDispatch) logf(format string, args ...any) {
	fmt.Fprintf(d.Out, "wildlife attacks: "+format+"\n", args...) //nolint:errcheck
}
