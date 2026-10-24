package worker

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// FactionPull moves allow-listed finished product from the crafter's faction
// storage into its personal storage, so the consignment-offer and shopkeeper
// steps (which only read personal storage) can see it. A facility craft that
// drew its inputs from faction storage delivers its output there.
//
// Per item: withdraw min(faction quantity, free hold) into cargo, then deposit
// it to personal storage. The withdraw lands in cargo, so the hold bounds each
// trip. moved counts units. A missing faction-storage reading moves nothing
// and is not an error; only a context error or a failed withdraw/deposit is.
func FactionPull(ctx context.Context, client game.GameClient, items []string, out io.Writer) (moved int, err error) {
	if len(items) == 0 {
		return 0, nil
	}
	if err := client.ViewFactionStorage(ctx); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		_, _ = fmt.Fprintf(out, "faction_pull: view faction storage: %v; skipping\n", err)

		return 0, nil
	}
	held, ok := consignHeld(client.GetRawJSON("faction_storage"))
	if !ok {
		_, _ = fmt.Fprintln(out, "faction_pull: no faction storage reading; skipping")

		return 0, nil
	}
	for _, item := range items {
		if cerr := ctx.Err(); cerr != nil {
			return moved, cerr
		}
		st := client.GetState()
		if st == nil {
			return moved, nil
		}
		free := int(st.Ship.CargoCapacity - st.Ship.CargoUsed)
		if free <= 0 {
			return moved, nil
		}
		qty := min(held[item], free)
		if qty <= 0 {
			continue
		}
		if werr := client.FactionWithdrawItems(ctx, item, qty); werr != nil {
			return moved, fmt.Errorf("faction withdraw %d %s: %w", qty, item, werr)
		}
		if derr := client.DepositItems(ctx, item, float64(qty)); derr != nil {
			return moved, fmt.Errorf("deposit %d %s (left in cargo): %w", qty, item, derr)
		}
		moved += qty
		_, _ = fmt.Fprintf(out, "faction_pull: moved %d %s faction->personal\n", qty, item)
	}

	return moved, nil
}

// ParseFactionPull decodes the --faction-pull flag: comma-separated explicit
// item ids. energy_crystal is refused because those crystals are reserved.
// A repeated id is dropped, first occurrence wins: FactionPull reads faction
// storage once per pass, so a second entry would withdraw units already moved.
func ParseFactionPull(s string) ([]string, error) {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if id == "energy_crystal" {
			return nil, fmt.Errorf("energy_crystal is reserved and may not be pulled")
		}
		if slices.Contains(out, id) {
			continue
		}
		out = append(out, id)
	}

	return out, nil
}
