package worker

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

// consignGiftPolls caps how many SleepLong waits a hauler standing at the
// source spends on a gift that has not landed yet (~5 minutes). The collector
// gifts on its next idle pass, which normally lands while the hauler is still
// in transit; outlasting this means the collector is stuck, and the claim goes
// back to the pool.
const consignGiftPolls = 15

var (
	// errConsignSourceGone: the collector could not gift (its storage holds
	// none of the item), so every route on this book is dead.
	errConsignSourceGone = errors.New("consign: collector has nothing to gift")
	// errConsignGiftPending: the gift has not landed within consignGiftPolls.
	errConsignGiftPending = errors.New("consign: gift not yet landed")
)

func consignHandoffID(oppID int, agentID string) string {
	return fmt.Sprintf("consign/%d/%s", oppID, agentID)
}

// itemUnitSize is itemID's per-unit cargo footprint from the catalog, 1 when
// the catalog is unavailable or does not know the item.
func itemUnitSize(ctx context.Context, kb knowledge.Base, itemID string) float64 {
	if kb == nil {
		return 1
	}
	if it, err := kb.GetItem(ctx, itemID); err == nil && it != nil && it.Size > 0 {
		return float64(it.Size)
	}
	return 1
}

// requestConsignGift asks the route's collector, through the handoff queue, to
// gift this hauler one hold's worth of the consigned goods. Called as soon as
// the route is claimed, so the gift lands while the hauler is in transit. The
// record ID is per (route, hauler), so a resumed run re-requesting is a no-op.
func requestConsignGift(ctx context.Context, deps HaulDeps, opp market.ArbitrageOpportunity, cargoCap float64) error {
	if deps.Handoff == nil {
		return errors.New("consign: no handoff queue configured")
	}
	holder, base, ok := market.ConsignSource(opp)
	if !ok {
		return fmt.Errorf("consign: opp %d carries no consign source", opp.ID)
	}
	qty := int(min(opp.Quantity, math.Floor(cargoCap/itemUnitSize(ctx, deps.KB, opp.ItemID))))
	if qty < 1 {
		return fmt.Errorf("consign: hold of %.0f fits no %s", cargoCap, opp.ItemID)
	}
	if _, err := deps.Handoff.Enqueue(handoff.Record{
		ID:        consignHandoffID(opp.ID, deps.AgentID),
		Holder:    holder,
		Station:   base,
		ItemID:    opp.ItemID,
		Qty:       qty,
		Recipient: deps.AgentID,
	}); err != nil {
		return fmt.Errorf("consign: request gift: %w", err)
	}
	return nil
}

// awaitConsignGift waits, at the source station, for this hauler's gift record
// to finish, and returns how many units it moved into the hauler's storage.
func awaitConsignGift(ctx context.Context, deps HaulDeps, opp market.ArbitrageOpportunity) (int, error) {
	id := consignHandoffID(opp.ID, deps.AgentID)
	for poll := 0; ; poll++ {
		recs, err := deps.Handoff.List()
		if err != nil {
			return 0, fmt.Errorf("consign: read handoff queue: %w", err)
		}
		var rec *handoff.Record
		for i := range recs {
			if recs[i].ID == id {
				rec = &recs[i]
				break
			}
		}
		switch {
		case rec == nil:
			return 0, fmt.Errorf("consign: gift record %s missing", id)
		case rec.Status == handoff.StatusDone:
			return rec.MovedQty, nil
		case rec.Status == handoff.StatusFailed:
			return 0, fmt.Errorf("%w: %s", errConsignSourceGone, rec.Error)
		case poll >= consignGiftPolls:
			return 0, errConsignGiftPending
		}
		settle(ctx, game.SleepLong)
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
	}
}
