package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
)

// FactionWithdraw moves up to want units of item from faction storage into the
// worker's personal storage at the docked station, one hold-load per trip
// (faction_withdraw_items lands in cargo, so the hold bounds each lot). It is
// the one-shot counterpart of FactionPull: personal storage is what send_gift
// --source=storage and the handoff queue read, so this is how faction stock is
// staged for a gift. Faction storage is read once; moved stops at that stock.
// A missing faction-storage reading or item moves nothing and is not an error.
func FactionWithdraw(ctx context.Context, client game.GameClient, item string, want int, out io.Writer) (moved int, err error) {
	if want <= 0 {
		return 0, nil
	}
	if err := client.ViewFactionStorage(ctx); err != nil {
		return 0, fmt.Errorf("faction_withdraw: view faction storage: %w", err)
	}
	held, ok := consignHeld(client.GetRawJSON("faction_storage"))
	if !ok {
		_, _ = fmt.Fprintln(out, "faction_withdraw: no faction storage reading; nothing moved")

		return 0, nil
	}
	stock := held[item]
	for moved < want && stock > 0 {
		if cerr := ctx.Err(); cerr != nil {
			return moved, cerr
		}
		st := client.GetState()
		if st == nil {
			return moved, fmt.Errorf("faction_withdraw: no ship state")
		}
		free := int(st.Ship.CargoCapacity - st.Ship.CargoUsed)
		if free <= 0 {
			return moved, fmt.Errorf("faction_withdraw: hold full, moved %d of %d %s", moved, want, item)
		}
		n := min(want-moved, stock, free)
		if werr := client.FactionWithdrawItems(ctx, item, n); werr != nil {
			return moved, fmt.Errorf("faction withdraw %d %s: %w", n, item, werr)
		}
		// Tick-deferred withdraw: wait, refresh cargo, deposit what landed.
		settle(ctx, game.SleepTick)
		if cerr := client.GetCargo(ctx); cerr != nil {
			return moved, fmt.Errorf("refresh cargo after faction withdraw %s: %w", item, cerr)
		}
		aboard := cargoCount(client.GetState(), item)
		if aboard <= 0 {
			return moved, fmt.Errorf("faction_withdraw: %d %s not in cargo after withdraw", n, item)
		}
		if derr := client.DepositItems(ctx, item, float64(aboard)); derr != nil {
			return moved, fmt.Errorf("deposit %d %s (left in cargo): %w", aboard, item, derr)
		}
		moved += aboard
		stock -= n
		_, _ = fmt.Fprintf(out, "faction_withdraw: moved %d %s faction->personal (%d/%d)\n", aboard, item, moved, want)
	}

	return moved, nil
}
