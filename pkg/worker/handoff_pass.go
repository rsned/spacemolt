package worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/rsned/spacemolt/pkg/handoff"
)

// handoffTransition is the shape of Queue.Transition, extracted so
// WorkerDispatch.handoffPersist can be overridden by tests to inject a
// persist error (write/rename failure) at an exact call without needing a
// corrupt or read-only queue file — mirrors the craftPollSleep override
// pattern (see dispatch.go).
type handoffTransition func(q *handoff.Queue, id string, from, to handoff.Status, mutate func(*handoff.Record)) (bool, error)

// defaultHandoffPersist is handoffPersist's default: a direct pass-through to
// Queue.Transition.
func defaultHandoffPersist(q *handoff.Queue, id string, from, to handoff.Status, mutate func(*handoff.Record)) (bool, error) {
	return q.Transition(id, from, to, mutate)
}

// errHandoffRecordResolved signals that moveHandoffStock already brought the
// record it was processing to a terminal state itself — either by
// successfully transitioning it to failed, or by logging a best-effort
// attempt to when that transition also failed. Ownership of the record's
// fate passed to the batch loop the moment its own progress-persist call
// stopped succeeding as expected (a CAS loss or a write error), so the
// caller (fulfillHandoff) must not transition or log it further.
var errHandoffRecordResolved = errors.New("handoff: record already resolved during batch persist")

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
		ok, terr := d.handoffPersist(q, rec.ID, handoff.StatusPending, handoff.StatusFailed, func(r *handoff.Record) {
			r.Error = msg
		})
		if terr != nil {
			fmt.Fprintf(d.Out, "handoff: transition %s to failed: %v\n", rec.ID, terr) //nolint:errcheck
			return
		}
		if !ok {
			fmt.Fprintf(d.Out, "handoff %s: could not record failure %q — record was no longer pending (handled elsewhere)\n", rec.ID, msg) //nolint:errcheck
			return
		}
		fmt.Fprintf(d.Out, "handoff %s: failed: %s\n", rec.ID, msg) //nolint:errcheck
	}

	if rec.Recipient == d.AgentID {
		fail(fmt.Sprintf("recipient %q is the holder's own agent id — invalid plan state", rec.Recipient))
		return
	}

	// rec.MovedQty carries progress persisted by an earlier, partially
	// completed pass (see moveHandoffStock's per-batch CAS updates below).
	// Only the remainder still needs to move; if a prior pass already moved
	// everything (or more, which shouldn't happen but is handled the same
	// way), there's nothing left to withdraw or gift — just finalize.
	remaining := rec.Qty - rec.MovedQty
	if remaining <= 0 {
		ok, terr := d.handoffPersist(q, rec.ID, handoff.StatusPending, handoff.StatusDone, nil)
		if terr != nil {
			fmt.Fprintf(d.Out, "handoff: transition %s to done: %v\n", rec.ID, terr) //nolint:errcheck
			return
		}
		if !ok {
			fmt.Fprintf(d.Out, "handoff %s: could not finalize — record was no longer pending (handled elsewhere)\n", rec.ID) //nolint:errcheck
			return
		}
		fmt.Fprintf(d.Out, "handoff %s: done, moved %d/%d %s to %s (already complete from a prior pass)\n", //nolint:errcheck
			rec.ID, rec.MovedQty, rec.Qty, rec.ItemID, rec.Recipient)
		return
	}

	username, err := UsernameFor(d.agentsDir(), rec.Recipient)
	if err != nil {
		fail(fmt.Sprintf("resolve recipient %s: %v", rec.Recipient, err))
		return
	}

	movedThisPass, definitive, err := d.moveHandoffStock(ctx, q, rec, username, remaining)
	if err != nil {
		if !errors.Is(err, errHandoffRecordResolved) {
			fmt.Fprintf(d.Out, "handoff %s: %v (leaving pending)\n", rec.ID, err) //nolint:errcheck
		}
		// errHandoffRecordResolved means moveHandoffStock already logged
		// (and, where possible, persisted) the record's terminal outcome —
		// nothing more to do here.
		return
	}
	if !definitive {
		// No progress this pass against stock the queue believes is
		// present (e.g. cargo hold full) — leave pending, retry next pass.
		return
	}

	// The per-batch persists inside moveHandoffStock already brought the
	// live record's MovedQty to baseline + everything moved this pass (each
	// batch persisted its own delta against the live record under lock) —
	// this transition only needs to flip the status. Adding movedThisPass
	// again here would double-count the last batch.
	ok, terr := d.handoffPersist(q, rec.ID, handoff.StatusPending, handoff.StatusDone, nil)
	if terr != nil {
		fmt.Fprintf(d.Out, "handoff: transition %s to done: %v\n", rec.ID, terr) //nolint:errcheck
		return
	}
	if !ok {
		fmt.Fprintf(d.Out, "handoff %s: could not finalize after moving %d this pass — record was no longer pending (handled elsewhere)\n", rec.ID, movedThisPass) //nolint:errcheck
		return
	}
	fmt.Fprintf(d.Out, "handoff %s: done, moved %d this pass (%d/%d %s to %s)\n", //nolint:errcheck
		rec.ID, movedThisPass, rec.MovedQty+movedThisPass, rec.Qty, rec.ItemID, rec.Recipient)
}

// moveHandoffStock withdraws and gifts up to remaining units of rec.ItemID
// (the still-outstanding balance — rec.Qty minus whatever a prior pass
// already moved) in cargo-hold-sized batches, mirroring Deliver's
// cargo-refresh discipline: the live client does not update state.Ship.Cargo
// on WithdrawItems or SendGift, so GetCargo is called after each. After every
// batch that successfully gifts at least one unit, it persists progress by
// CAS-updating the still-pending record's MovedQty with a DELTA against the
// live record under lock (r.MovedQty += the batch's own gifted quantity) —
// never a precomputed cumulative — so a concurrent writer's update to
// MovedQty between this record's pickup and this persist is preserved
// rather than clobbered, and a transient failure partway through a
// multi-batch pass does not lose the batches that already landed. The next
// pass resumes from the remainder instead of re-gifting the full quantity.
//
// Every persist's CAS result is checked: a false ok means the record left
// pending under us (another actor already transitioned it, e.g. to failed)
// — processing stops immediately, no further batches are withdrawn or
// gifted. A non-nil error means the persist itself failed (e.g. a write
// error) after the gift already landed in the recipient's cargo; that is
// escalated to a failed transition carrying the gifted-but-unpersisted
// quantity, because leaving the record pending with a stale, too-low
// MovedQty would cause the next pass to re-gift those same units. Both of
// these outcomes are logged (and, for the escalation, persisted) internally
// here, signaled to the caller via errHandoffRecordResolved so it does not
// also try to transition or log the record.
//
// It returns the total actually moved this pass and whether the outcome is
// definitive:
//
//   - definitive=true, err=nil: either the full remaining quantity moved, or
//     a withdraw came back with fewer units than requested for a definitive
//     reason (isShortSupplyErr — the source really doesn't have any more).
//     The caller marks the record done.
//   - definitive=false, err=nil: the pass made no progress for a transient,
//     non-error reason (the cargo hold has no free space this pass). The
//     caller leaves the record pending.
//   - err is errHandoffRecordResolved: the record has already been left in
//     its terminal state (or a best-effort attempt logged) by this
//     function; the caller must not touch it further.
//   - err != nil (any other error): a real (non-short-supply) client error
//     — transient by assumption (e.g. a disconnect). The caller leaves the
//     record pending and logs it; whatever was already gifted (and
//     persisted) in this pass stands, and is not re-attempted next pass.
func (d *WorkerDispatch) moveHandoffStock(ctx context.Context, q *handoff.Queue, rec handoff.Record, username string, remaining int) (moved int, definitive bool, err error) {
	for remaining > 0 {
		free := cargoFreeSpace(d.Client.GetState())
		if free <= 0 {
			return moved, moved > 0, nil
		}
		want := min(remaining, free)

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

			// Persist progress immediately so a later transient failure
			// within this same pass (or a crash) never loses a batch that
			// already landed in the recipient's cargo.
			gotThisBatch := got
			ok, terr := d.handoffPersist(q, rec.ID, handoff.StatusPending, handoff.StatusPending, func(r *handoff.Record) {
				r.MovedQty += gotThisBatch
			})
			switch {
			case terr != nil:
				// The gift already landed, but recording it failed (e.g. a
				// disk write error) — the on-disk MovedQty was NOT updated
				// by the call above (it errors before or during the
				// write/rename, never after a successful one). Escalate to
				// failed so an operator looks, instead of leaving a stale,
				// too-low MovedQty that would make the next pass re-gift
				// these same units.
				//
				// TODO: a disk failure between the gift succeeding and BOTH
				// this escalation attempt and its own transition failing
				// leaves a residual window where units are gifted but
				// unaccounted for anywhere in the queue; would need manual
				// reconciliation tooling if ever observed in practice.
				msg := fmt.Sprintf("gifted %d %s to %s but failed to persist progress: %v", gotThisBatch, rec.ItemID, rec.Recipient, terr)
				if _, ferr := d.handoffPersist(q, rec.ID, handoff.StatusPending, handoff.StatusFailed, func(r *handoff.Record) {
					r.MovedQty += gotThisBatch
					r.Error = msg
				}); ferr != nil {
					fmt.Fprintf(d.Out, "handoff %s: CRITICAL: %s; also failed to mark the record failed: %v\n", rec.ID, msg, ferr) //nolint:errcheck
				} else {
					fmt.Fprintf(d.Out, "handoff %s: failed: %s\n", rec.ID, msg) //nolint:errcheck
				}
				return moved, true, errHandoffRecordResolved
			case !ok:
				// CAS lost: the record left the pending status under us
				// (e.g. another actor already marked it done or failed).
				// Stop gifting further batches for a record that may
				// already be settled elsewhere.
				fmt.Fprintf(d.Out, "handoff %s: record left pending during pass (handled elsewhere) — stopping further batches\n", rec.ID) //nolint:errcheck
				return moved, true, errHandoffRecordResolved
			}
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
