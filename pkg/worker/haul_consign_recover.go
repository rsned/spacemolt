package worker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/market"
)

const (
	// consignRecoverMaxAge bounds which gifts a hauler goes back for. A record
	// older than this predates collection tracking, or has been written off.
	consignRecoverMaxAge = 7 * 24 * time.Hour
	// consignRecoverMaxAttempts is how many failed trips a gift gets before it
	// is marked failed for the operator, so a hauler cannot wedge on it.
	consignRecoverMaxAttempts = 3
)

// markConsignCollected records that this hauler loaded its gift, which is what
// separates a finished route from one that left the goods behind. Best-effort:
// a missed mark costs one empty trip back, never the goods.
func markConsignCollected(deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity) {
	if deps.Handoff == nil {
		return
	}
	id := consignHandoffID(opp.ID, deps.AgentID)
	if _, err := deps.Handoff.Transition(id, handoff.StatusDone, handoff.StatusCollected, nil); err != nil {
		fmt.Fprintf(out, "haul: opp %d mark gift collected failed: %v\n", opp.ID, err) //nolint:errcheck
	}
}

// uncollectedConsignGift returns this hauler's newest-listed gift still marked
// done: moved into its storage, never loaded.
func uncollectedConsignGift(q *handoff.Queue, agentID string, now time.Time) (handoff.Record, bool, error) {
	recs, err := q.List()
	if err != nil {
		return handoff.Record{}, false, err
	}
	for _, r := range recs {
		if r.Status != handoff.StatusDone || r.Recipient != agentID || r.MovedQty < 1 ||
			!strings.HasPrefix(r.ID, consignHandoffPrefix) {
			continue
		}
		at, perr := time.Parse(time.RFC3339, r.UpdatedAt)
		if perr != nil || now.Sub(at) > consignRecoverMaxAge {
			continue
		}
		return r, true, nil
	}
	return handoff.Record{}, false, nil
}

// recoverConsignGifts returns one stranded gift to its collector. A consigned
// route can die after the gift lands (salvager-1 lost 1,144 units' route to a
// dropped connection on 2026-11-05), and a gift sits in the recipient's storage
// at the sending station, where neither the offer nor any route can see it.
// Back in the collector's storage the goods are offered again and the scanner
// re-lists them. It goes back from storage, so the hold stays free.
//
// resolve maps a base id to its system and POI. Returns true if it spent the
// pass on a gift.
func recoverConsignGifts(ctx context.Context, deps HaulDeps, out io.Writer,
	resolve func(context.Context, string) (string, string, error),
) bool {
	if deps.Handoff == nil {
		return false
	}
	rec, ok, err := uncollectedConsignGift(deps.Handoff, deps.AgentID, haulNow(deps))
	if err != nil {
		fmt.Fprintf(out, "haul: consign recovery: read queue: %v\n", err) //nolint:errcheck
		return false
	}
	if !ok {
		return false
	}
	err = returnConsignGift(ctx, deps, out, rec, resolve)
	if err == nil {
		return true
	}
	if ctx.Err() != nil {
		return true
	}
	next := handoff.StatusDone
	if rec.RecoverAttempts+1 >= consignRecoverMaxAttempts {
		next = handoff.StatusFailed
	}
	fmt.Fprintf(out, "haul: consign recovery %s attempt %d failed: %v\n", rec.ID, rec.RecoverAttempts+1, err) //nolint:errcheck
	if _, terr := deps.Handoff.Transition(rec.ID, handoff.StatusDone, next, func(r *handoff.Record) {
		r.RecoverAttempts++
		r.Error = "recover: " + err.Error()
	}); terr != nil {
		fmt.Fprintf(out, "haul: consign recovery %s record failed: %v\n", rec.ID, terr) //nolint:errcheck
	}
	return true
}

func returnConsignGift(ctx context.Context, deps HaulDeps, out io.Writer, rec handoff.Record,
	resolve func(context.Context, string) (string, string, error),
) error {
	system, poi, err := resolve(ctx, rec.Station)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", rec.Station, err)
	}
	recipient, err := UsernameFor("", rec.Holder)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "haul: returning stranded gift %s: up to %d %s to %s\n", rec.ID, rec.MovedQty, rec.ItemID, rec.Holder) //nolint:errcheck
	if err := haulAutopilot(ctx, deps, out, system, poi, nil); err != nil {
		return fmt.Errorf("transit: %w", err)
	}
	if err := dockIdempotent(ctx, deps.Client); err != nil {
		return fmt.Errorf("dock: %w", err)
	}
	if err := deps.Client.ViewStorage(ctx); err != nil {
		return fmt.Errorf("view storage: %w", err)
	}
	held, ok := consignHeld(deps.Client.GetRawJSON("storage"))
	if !ok {
		return fmt.Errorf("no storage reading at %s", rec.Station)
	}
	if qty := min(held[rec.ItemID], rec.MovedQty); qty > 0 {
		if err := deps.Client.SendGift(ctx, map[string]any{
			"recipient": recipient,
			"item_id":   rec.ItemID,
			"quantity":  qty,
			"source":    "storage",
		}); err != nil {
			return fmt.Errorf("gift: %w", err)
		}
		fmt.Fprintf(out, "haul: returned %d %s to %s\n", qty, rec.ItemID, rec.Holder) //nolint:errcheck
	}
	if _, err := deps.Handoff.Transition(rec.ID, handoff.StatusDone, handoff.StatusCollected, nil); err != nil {
		return fmt.Errorf("mark collected: %w", err)
	}
	return nil
}
