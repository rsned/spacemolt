package worker

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/market"
)

// ConsignOffer advertises an item this collector will gift to whichever hauler
// claims a consigned route for it. Keep units stay in storage regardless.
//
// It is the claim-driven counterpart of ConsignRule: a rule names a fixed
// recipient up front, while an offer lets the hauler fleet decide who goes,
// and the gift follows the claim through the handoff queue. Explicit
// allow-list only, for the same reason as ConsignRule — gifting is
// irreversible, and reserved goods share this storage.
type ConsignOffer struct {
	Item string
	Keep int
}

// ConsignStockStore is the slice of *market.Collector the report writes to.
type ConsignStockStore interface {
	UpsertConsignStock(ctx context.Context, s market.ConsignStock) error
}

var _ ConsignStockStore = (*market.Collector)(nil)

// ParseConsignOffers decodes the flag form the supervisor forwards:
// item[:keep], comma-separated. A malformed offer is rejected rather than
// skipped, so a typo cannot leave a collector's stock silently unadvertised.
func ParseConsignOffers(spec string) ([]ConsignOffer, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	var out []ConsignOffer
	for raw := range strings.SplitSeq(spec, ",") {
		parts := strings.Split(strings.TrimSpace(raw), ":")
		if len(parts) > 2 || parts[0] == "" {
			return nil, fmt.Errorf("consign offer %q: want item[:keep]", raw)
		}
		o := ConsignOffer{Item: parts[0]}
		if len(parts) == 2 {
			keep, err := strconv.Atoi(parts[1])
			if err != nil || keep < 0 {
				return nil, fmt.Errorf("consign offer %q: keep must be a non-negative integer", raw)
			}
			o.Keep = keep
		}
		out = append(out, o)
	}
	return out, nil
}

// HandoffOwed sums, per item, what holder's still-pending handoff records at
// base have yet to move. Those units are spoken for even though they still sit
// in storage, so the offer must not advertise them again.
func HandoffOwed(q *handoff.Queue, holder, base string) (map[string]int, error) {
	recs, err := q.List()
	if err != nil {
		return nil, err
	}
	owed := map[string]int{}
	for _, r := range recs {
		if r.Status == handoff.StatusPending && r.Holder == holder && r.Station == base {
			if left := r.Qty - r.MovedQty; left > 0 {
				owed[r.ItemID] += left
			}
		}
	}
	return owed, nil
}

// ReportConsignStock reads this station's storage and records, for each offer,
// how many units a hauler may claim: held, less Keep, less owed (pending
// handoffs not yet gifted). It runs after HandoffPass in the idle pass, so
// gifts already made this pass are out of storage by the time it reads.
//
// Without a storage reading it reports nothing. A zero would withdraw a real
// offer on a read glitch; silence lets the last report age out instead
// (market.ConsignStockMaxAge).
func ReportConsignStock(ctx context.Context, client game.GameClient, store ConsignStockStore,
	holder string, offers []ConsignOffer, owed map[string]int,
) error {
	if client == nil || store == nil || len(offers) == 0 {
		return nil
	}
	state := client.GetState()
	if state == nil || !state.Doc || state.Player.DockedAtBase == "" || state.CurrentPOI == "" {
		return nil
	}
	if err := client.ViewStorage(ctx); err != nil {
		return nil //nolint:nilerr // no reading is not an error worth failing the idle pass
	}
	held, ok := consignHeld(client.GetRawJSON("storage"))
	if !ok {
		return nil
	}

	now := time.Now().UTC()
	for _, o := range offers {
		units := max(held[o.Item]-o.Keep-owed[o.Item], 0)
		if err := store.UpsertConsignStock(ctx, market.ConsignStock{
			Holder:    holder,
			StationID: state.CurrentPOI,
			BaseID:    state.Player.DockedAtBase,
			ItemID:    o.Item,
			Units:     float64(units),
			UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("report consign stock %s: %w", o.Item, err)
		}
	}
	return nil
}
