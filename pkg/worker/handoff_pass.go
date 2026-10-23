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
// fate passed to moveHandoffStock the moment its own progress-persist call
// stopped succeeding as expected (a CAS loss or a write error), so the
// caller (fulfillHandoff) must not transition or log it further.
var errHandoffRecordResolved = errors.New("handoff: record already resolved during progress persist")

// HandoffPass fulfills pending handoff records owned by this agent
// (d.AgentID) at its current docked station: gift the staged stock straight
// from storage to the resolved recipient, and mark the
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
// recipient's in-game username, gifts the staged stock from storage, and
// transitions the record to done (with the actual
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
	// completed pass (see moveHandoffStock's CAS update below).
	// Only the remainder still needs to move; if a prior pass already moved
	// everything (or more, which shouldn't happen but is handled the same
	// way), there's nothing left to gift — just finalize.
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

	movedThisPass, err := d.moveHandoffStock(ctx, q, rec, username, remaining)
	if err != nil {
		if !errors.Is(err, errHandoffRecordResolved) {
			fmt.Fprintf(d.Out, "handoff %s: %v (leaving pending)\n", rec.ID, err) //nolint:errcheck
		}
		// errHandoffRecordResolved means moveHandoffStock already logged
		// (and, where possible, persisted) the record's terminal outcome —
		// nothing more to do here.
		return
	}

	// A pass that read storage and moved NOTHING across the whole record (nothing
	// now, nothing from a prior pass) means the holder's storage is empty of
	// this item — the queue's belief is stale. Don't fake-done a zero handoff
	// (a downstream craft would proceed and fail blaming the wrong node): fail
	// the record with a replan-flavored error so the runner parks the node
	// needs-operator. A partial move (rec.MovedQty+movedThisPass > 0) still
	// completes as done below.
	if rec.MovedQty+movedThisPass == 0 {
		fail(fmt.Sprintf("nothing moved for %s at %s — holder storage empty, replan needed", rec.ItemID, rec.Station))
		return
	}

	// moveHandoffStock already added this pass's delta to the live record's
	// MovedQty under lock; this transition only flips the status. Adding
	// movedThisPass again here would double-count it.
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

// moveHandoffStock gifts up to remaining units of rec.ItemID (the
// still-outstanding balance — rec.Qty minus whatever a prior pass already
// moved) straight out of station storage with send_gift source=storage. The
// goods never enter the hold, so cargo space and item size play no part: the
// 2026-10-22 adamantite handoff (2,837 units of a size-2 ore, 100-capacity
// hold) failed as "holder storage empty" when this withdrew a cargo-sized
// batch first, because the oversized withdraw's "not enough cargo space"
// reply read as short supply.
//
// The gift is sized from a fresh view_storage, capped at what is held, so it
// is never oversized. After it lands, progress is persisted by CAS-updating
// the still-pending record's MovedQty with a DELTA against the live record
// under lock (r.MovedQty += gifted) — never a precomputed cumulative — so a
// concurrent writer's update between this record's pickup and this persist
// is preserved rather than clobbered.
//
// A persist that returns ok=false means the record left pending under us
// (another actor already transitioned it): the gift stands and the record is
// left as that actor set it. A persist that errors after the gift landed is
// escalated to failed carrying the gifted-but-unpersisted quantity, because
// leaving the record pending with a stale, too-low MovedQty would make the
// next pass re-gift those same units. Both are signalled to the caller via
// errHandoffRecordResolved so it does not also transition or log the record.
//
// It returns the quantity moved this pass:
//
//   - err=nil: storage was read and what it held (up to remaining) was
//     gifted — possibly nothing, when storage holds none. The caller marks
//     the record done, or failed when nothing has ever moved.
//   - err is errHandoffRecordResolved: the record is already in its terminal
//     state (or a best-effort attempt was logged); the caller must not touch it.
//   - err != nil (any other error): storage could not be read, or the gift
//     failed — transient by assumption (e.g. a disconnect). Nothing moved; the
//     caller leaves the record pending for the next pass.
func (d *WorkerDispatch) moveHandoffStock(ctx context.Context, q *handoff.Queue, rec handoff.Record, username string, remaining int) (moved int, err error) {
	if serr := d.Client.ViewStorage(ctx); serr != nil {
		return 0, fmt.Errorf("view storage: %w", serr)
	}
	// An undecodable frame is no information, never an empty storage: failing
	// the record on it would park a plan node for a read glitch.
	held, ok := consignHeld(d.Client.GetRawJSON("storage"))
	if !ok {
		return 0, fmt.Errorf("view storage: no readable storage frame")
	}
	give := min(remaining, held[rec.ItemID])
	if give <= 0 {
		return 0, nil
	}

	if gerr := d.Client.SendGift(ctx, map[string]any{
		"recipient": username,
		"item_id":   rec.ItemID,
		"quantity":  float64(give),
		"source":    "storage",
	}); gerr != nil {
		return 0, fmt.Errorf("gift %s to %s: %w", rec.ItemID, rec.Recipient, gerr)
	}

	ok, terr := d.handoffPersist(q, rec.ID, handoff.StatusPending, handoff.StatusPending, func(r *handoff.Record) {
		r.MovedQty += give
	})
	switch {
	case terr != nil:
		// TODO: a disk failure between the gift succeeding and BOTH this
		// escalation attempt and its own transition failing leaves a residual
		// window where units are gifted but unaccounted for anywhere in the
		// queue; would need manual reconciliation tooling if ever observed.
		msg := fmt.Sprintf("gifted %d %s to %s but failed to persist progress: %v", give, rec.ItemID, rec.Recipient, terr)
		if _, ferr := d.handoffPersist(q, rec.ID, handoff.StatusPending, handoff.StatusFailed, func(r *handoff.Record) {
			r.MovedQty += give
			r.Error = msg
		}); ferr != nil {
			fmt.Fprintf(d.Out, "handoff %s: CRITICAL: %s; also failed to mark the record failed: %v\n", rec.ID, msg, ferr) //nolint:errcheck
		} else {
			fmt.Fprintf(d.Out, "handoff %s: failed: %s\n", rec.ID, msg) //nolint:errcheck
		}
		return give, errHandoffRecordResolved
	case !ok:
		fmt.Fprintf(d.Out, "handoff %s: record left pending during the gift (handled elsewhere) — leaving it as set\n", rec.ID) //nolint:errcheck
		return give, errHandoffRecordResolved
	}
	return give, nil
}
