package worker

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// FactionPullRule is one --faction-pull entry. Keep units stay in faction
// storage: a pull otherwise takes everything, and the goods worth selling are
// often the ones facility upkeep and construction draw on.
type FactionPullRule struct {
	Item string
	Keep int
	// Size is the item's per-unit cargo footprint; 0 means 1. The withdraw
	// lands in cargo, so a 2-unit item fits half as many per trip.
	Size float64
}

// FactionPull moves allow-listed finished product from the crafter's faction
// storage into its personal storage, so the consignment-offer and shopkeeper
// steps (which only read personal storage) can see it. A facility craft that
// drew its inputs from faction storage delivers its output there.
//
// Per item: move the faction quantity above Keep straight into personal storage
// with deposit_items source=faction target=self (no cargo hop, so the hold does
// not bound it). Units an older cargo-routed pull left aboard are deposited
// first. moved counts units. A missing faction-storage reading moves nothing
// and is not an error; only a context error or a failed withdraw/deposit is.
func FactionPull(ctx context.Context, client game.GameClient, rules []FactionPullRule, out io.Writer) (moved int, err error) {
	if len(rules) == 0 {
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
	for _, rule := range rules {
		item := rule.Item
		if cerr := ctx.Err(); cerr != nil {
			return moved, cerr
		}
		// Drain anything of this item already aboard first — a withdraw that an
		// earlier pass could not deposit (live 2026-10-24: 60 units stranded
		// with faction storage at zero, so the withdraw branch never ran).
		if aboard := cargoCount(client.GetState(), item); aboard > 0 {
			if derr := client.DepositItems(ctx, item, float64(aboard)); derr != nil {
				return moved, fmt.Errorf("deposit %d stranded %s (left in cargo): %w", aboard, item, derr)
			}
			moved += aboard
			_, _ = fmt.Fprintf(out, "faction_pull: moved %d stranded %s cargo->personal\n", aboard, item)
		}
		qty := held[item] - rule.Keep
		if qty <= 0 {
			continue
		}
		// One storage-to-storage call: no cargo hop, so the hold size plays no
		// part (the cargo-routed pull crawled 175 per pass through a 175-space
		// hold, 2026-11-20).
		if terr := client.DepositItemsPayload(ctx, map[string]any{
			"item_id": item, "quantity": qty, "source": "faction", "target": "self",
		}); terr != nil {
			return moved, fmt.Errorf("faction->personal %d %s: %w", qty, item, terr)
		}
		moved += qty
		_, _ = fmt.Fprintf(out, "faction_pull: moved %d %s faction->personal\n", qty, item)
		// The transfer is tick-deferred; let it land so the next pass's faction
		// reading does not count these units again.
		settle(ctx, game.SleepTick)
	}

	return moved, nil
}

// ParseFactionPull decodes the --faction-pull flag: comma-separated item[:keep]
// entries. energy_crystal is refused because those crystals are reserved.
// A repeated id is dropped, first occurrence wins: FactionPull reads faction
// storage once per pass, so a second entry would withdraw units already moved.
func ParseFactionPull(s string) ([]FactionPullRule, error) {
	var out []FactionPullRule
	for part := range strings.SplitSeq(s, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		id, keepStr, hasKeep := strings.Cut(entry, ":")
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("faction pull %q: no item id", entry)
		}
		if id == "energy_crystal" {
			return nil, fmt.Errorf("energy_crystal is reserved and may not be pulled")
		}
		keep := 0
		if hasKeep {
			n, err := strconv.Atoi(strings.TrimSpace(keepStr))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("faction pull %q: keep must be a non-negative integer", entry)
			}
			keep = n
		}
		if slices.ContainsFunc(out, func(r FactionPullRule) bool { return r.Item == id }) {
			continue
		}
		out = append(out, FactionPullRule{Item: id, Keep: keep})
	}

	return out, nil
}

// SizeFactionPullRules returns rules with each Size filled from the catalog, so
// FactionPull bounds a withdraw by cargo space rather than by unit count.
func SizeFactionPullRules(ctx context.Context, kb knowledge.Base, rules []FactionPullRule) []FactionPullRule {
	out := slices.Clone(rules)
	for i := range out {
		out[i].Size = itemUnitSize(ctx, kb, out[i].Item)
	}
	return out
}
