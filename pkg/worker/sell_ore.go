package worker

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// sellableOreRarity is the catalog rarity of ores sell_ore turns into cash.
// Everything rarer (rare, exotic, legendary) is kept for deposit_all to bank.
var sellableOreRarity = map[string]bool{"common": true, "uncommon": true}

// SellOre sells, at the docked station's market, every cargo line that the
// catalog lists as a common or uncommon ore. It exists so a miner that banks
// its hold earns enough to pay for fuel: a miner that deposited everything had
// no income and went fuel-dead (the overmind agent, 2026-11-12).
//
// Items the catalog does not know are kept. A refused sale (no buyer here) is
// logged and skipped; the cargo stays for deposit_all. It returns how many
// item types were sold.
func SellOre(ctx context.Context, client game.GameClient, kb knowledge.Base, out io.Writer) (int, error) {
	if err := client.GetCargo(ctx); err != nil {
		fmt.Fprintf(out, "sell_ore: cargo refresh failed: %v\n", err) //nolint:errcheck
	}
	st := client.GetState()
	if st == nil || !st.Doc {
		return 0, errors.New("sell_ore: must be docked")
	}
	sold := 0
	for _, c := range st.Ship.Cargo {
		if c.Quantity <= 0 {
			continue
		}
		item, err := kb.GetItem(ctx, c.ItemID)
		if err != nil || item == nil || item.Category != "ore" || !sellableOreRarity[item.Rarity] {
			continue
		}
		if err := client.Sell(ctx, c.ItemID, c.Quantity); err != nil {
			fmt.Fprintf(out, "sell_ore: %s x%.0f not sold: %v\n", c.ItemID, c.Quantity, err) //nolint:errcheck
			continue
		}
		sold++
		if px, ok := actualUnitPrice(client.GetRawJSON("sell"), "total_earned", "quantity_sold"); ok {
			fmt.Fprintf(out, "sell_ore: sold %s x%.0f @%.0f\n", c.ItemID, c.Quantity, px) //nolint:errcheck
		} else {
			fmt.Fprintf(out, "sell_ore: sold %s x%.0f\n", c.ItemID, c.Quantity) //nolint:errcheck
		}
	}
	return sold, nil
}
