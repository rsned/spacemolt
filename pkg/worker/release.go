package worker

import (
	"context"
	"fmt"
	"strconv"
)

// Release hands this agent's stock of itemID, above keep, from its personal
// storage at station straight to recipient (send_gift source=storage: no
// cargo trip). It exists for the route fleet (2026-11-20): only an owner can
// move its own storage, and only while docked there, so owners release on
// their own schedule and the route's trucker hauls the stock to Conjunction
// Junction with deliver.
//
// Anywhere but docked at station (base id or POI id: stations are
// dual-named) it is a no-op that makes no calls, so owners can run it often.
func (d *WorkerDispatch) Release(ctx context.Context, itemID, station, recipient string, keep int) error {
	st := d.Client.GetState()
	if st == nil || !st.IsDocked() || (st.Player.DockedAtBase != station && st.CurrentPOI != station) {
		return nil
	}
	username, err := resolveRecipientUsername(d, recipient)
	if err != nil {
		return fmt.Errorf("release: %w", err)
	}
	if err := d.Client.ViewStorage(ctx); err != nil {
		return fmt.Errorf("release: view storage: %w", err)
	}
	qty := storageItemCount(d.Client.GetRawJSON("storage"), itemID) - keep
	if qty <= 0 {
		return nil
	}
	if err := d.Client.SendGift(ctx, map[string]any{
		"recipient": username,
		"item_id":   itemID,
		"quantity":  float64(qty),
		"source":    "storage",
	}); err != nil {
		return fmt.Errorf("release: gift %d %s to %s: %w", qty, itemID, recipient, err)
	}
	fmt.Fprintf(d.Out, "release: gifted %d %s to %s at %s\n", qty, itemID, recipient, station) //nolint:errcheck
	return nil
}

// parseReleaseArgs parses ITEM STATION RECIPIENT [KEEP].
func parseReleaseArgs(args []string) (item, station, recipient string, keep int, err error) {
	if len(args) < 3 {
		return "", "", "", 0, fmt.Errorf("release: want ITEM STATION RECIPIENT [KEEP], got %v", args)
	}
	if len(args) >= 4 {
		keep, err = strconv.Atoi(args[3])
		if err != nil || keep < 0 {
			return "", "", "", 0, fmt.Errorf("release: bad keep %q", args[3])
		}
	}
	return args[0], args[1], args[2], keep, nil
}
