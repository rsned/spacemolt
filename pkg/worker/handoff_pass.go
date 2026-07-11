package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/handoff"
)

// HandoffPass fulfills pending handoff records owned by this agent
// (d.AgentID) at its current docked station: withdraw the staged stock from
// storage into cargo, gift it to the resolved recipient, and mark the
// record done with however much actually moved. Records held by another
// agent, or staged at a station this worker isn't currently docked at, are
// left untouched — those belong to a different pass (a different agent's,
// or this agent's next visit). A worker that isn't currently docked
// anywhere is a no-op pass, not an error.
//
// Every matching record is processed; a single record's failure (bad
// recipient, invalid plan state) transitions that record to failed and
// moves on to the next one. The returned error is reserved for a failure of
// the pass itself — e.g. the queue file could not be read — not a
// per-record outcome.
func (d *WorkerDispatch) HandoffPass(ctx context.Context, q *handoff.Queue) error {
	if q == nil {
		return nil
	}
	state := d.Client.GetState()
	if state == nil || !state.Doc || state.Player.DockedAtBase == "" {
		return nil
	}
	station := state.Player.DockedAtBase

	recs, err := q.List()
	if err != nil {
		return fmt.Errorf("handoff pass: list queue: %w", err)
	}
	for _, rec := range recs {
		if rec.Status != handoff.StatusPending || rec.Holder != d.AgentID || rec.Station != station {
			continue
		}
		d.fulfillHandoff(ctx, q, rec)
	}
	return nil
}

// fulfillHandoff processes one pending record already confirmed to be held
// by this agent at the currently-docked station. It rejects a self-targeted
// record as an invalid plan state (controller decision 2), resolves the
// recipient's in-game username, withdraws and gifts staged stock in
// cargo-sized batches, and transitions the record to done (with the actual
// MovedQty, which may be less than Qty on short storage) or failed. A
// transient failure (e.g. a mid-pass disconnect) is logged and the record is
// left pending for the next pass to retry — it is never marked done or
// failed on transient grounds.
func (d *WorkerDispatch) fulfillHandoff(ctx context.Context, q *handoff.Queue, rec handoff.Record) {
	fail := func(msg string) {
		if _, terr := q.Transition(rec.ID, handoff.StatusPending, handoff.StatusFailed, func(r *handoff.Record) {
			r.Error = msg
		}); terr != nil {
			fmt.Fprintf(d.Out, "handoff: transition %s to failed: %v\n", rec.ID, terr) //nolint:errcheck
			return
		}
		fmt.Fprintf(d.Out, "handoff %s: failed: %s\n", rec.ID, msg) //nolint:errcheck
	}

	if rec.Recipient == d.AgentID {
		fail(fmt.Sprintf("recipient %q is the holder's own agent id — invalid plan state", rec.Recipient))
		return
	}

	username, err := UsernameFor(d.agentsDir(), rec.Recipient)
	if err != nil {
		fail(fmt.Sprintf("resolve recipient %s: %v", rec.Recipient, err))
		return
	}

	moved, definitive, err := d.moveHandoffStock(ctx, rec, username)
	if err != nil {
		fmt.Fprintf(d.Out, "handoff %s: %v (leaving pending)\n", rec.ID, err) //nolint:errcheck
		return
	}
	if !definitive {
		// No progress this pass against stock the queue believes is
		// present (e.g. cargo hold full) — leave pending, retry next pass.
		return
	}
	if _, terr := q.Transition(rec.ID, handoff.StatusPending, handoff.StatusDone, func(r *handoff.Record) {
		r.MovedQty = moved
	}); terr != nil {
		fmt.Fprintf(d.Out, "handoff: transition %s to done: %v\n", rec.ID, terr) //nolint:errcheck
		return
	}
	fmt.Fprintf(d.Out, "handoff %s: done, moved %d/%d %s to %s\n", rec.ID, moved, rec.Qty, rec.ItemID, rec.Recipient) //nolint:errcheck
}

// moveHandoffStock withdraws and gifts up to rec.Qty units of rec.ItemID in
// cargo-hold-sized batches, mirroring Deliver's cargo-refresh discipline:
// the live client does not update state.Ship.Cargo on WithdrawItems or
// SendGift, so GetCargo is called after each. It returns the total actually
// moved and whether the outcome is definitive:
//
//   - definitive=true, err=nil: either the full quantity moved, or a
//     withdraw came back with fewer units than requested for a definitive
//     reason (isShortSupplyErr — the source really doesn't have any more).
//     The caller marks the record done either way, MovedQty = moved.
//   - definitive=false, err=nil: the pass made no progress for a transient,
//     non-error reason (the cargo hold has no free space this pass). The
//     caller leaves the record pending.
//   - err != nil: a real (non-short-supply) client error — transient by
//     assumption (e.g. a disconnect). The caller leaves the record pending
//     and logs it; nothing already gifted in this pass is undone.
func (d *WorkerDispatch) moveHandoffStock(ctx context.Context, rec handoff.Record, username string) (moved int, definitive bool, err error) {
	remaining := rec.Qty
	for remaining > 0 {
		free := cargoFreeSpace(d.Client.GetState())
		if free <= 0 {
			return moved, moved > 0, nil
		}
		want := remaining
		if want > free {
			want = free
		}

		before := cargoCount(d.Client.GetState(), rec.ItemID)
		werr := d.Client.WithdrawItems(ctx, rec.ItemID, float64(want))
		if werr != nil && !isShortSupplyErr(werr) {
			return moved, moved > 0, fmt.Errorf("withdraw %s: %w", rec.ItemID, werr)
		}
		if cerr := d.Client.GetCargo(ctx); cerr != nil {
			return moved, moved > 0, fmt.Errorf("refresh cargo after withdraw %s: %w", rec.ItemID, cerr)
		}
		got := cargoCount(d.Client.GetState(), rec.ItemID) - before
		short := got < want

		if got > 0 {
			if gerr := d.Client.SendGift(ctx, map[string]any{
				"recipient": username,
				"item_id":   rec.ItemID,
				"quantity":  float64(got),
			}); gerr != nil {
				return moved, moved > 0, fmt.Errorf("gift %s to %s: %w", rec.ItemID, rec.Recipient, gerr)
			}
			if cerr := d.Client.GetCargo(ctx); cerr != nil {
				return moved, moved > 0, fmt.Errorf("refresh cargo after gift %s: %w", rec.ItemID, cerr)
			}
			moved += got
			remaining -= got
		}

		if short {
			// Definitive: this batch's withdraw came up short of what was
			// asked for — the source is exhausted, another pass will not
			// find more. Whatever moved so far stands.
			return moved, true, nil
		}
	}
	return moved, true, nil
}
