package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// fillCargoOptions mirrors the flags accepted by the `fill_cargo` REPL command.
type fillCargoOptions struct {
	category string // catalog category to pull from storage (case-insensitive)
	dryRun   bool   // print the plan without withdrawing
}

// fillStep is one withdraw_items call in a fill_cargo plan.
type fillStep struct {
	ItemID   string
	Quantity int
	Space    int // cargo space the withdrawal uses (Quantity × unit size)
}

// parseFillCargoArgs reads `fill_cargo --category=<cat> [--dry-run]`; the
// category may also be given as a bare positional argument.
func parseFillCargoArgs(args []string) (fillCargoOptions, error) {
	var opts fillCargoOptions
	for _, arg := range args {
		switch {
		case arg == "--dry-run":
			opts.dryRun = true
		case strings.HasPrefix(arg, "--category="):
			opts.category = strings.TrimPrefix(arg, "--category=")
		case !strings.HasPrefix(arg, "--") && opts.category == "":
			opts.category = arg
		default:
			return opts, fmt.Errorf("fill_cargo: unknown argument %q", arg)
		}
	}
	if opts.category == "" {
		return opts, fmt.Errorf("usage: fill_cargo --category=<category> [--dry-run]  (e.g. consumable, ore, refined, component, ammo, misc, unknown)")
	}
	return opts, nil
}

// buildFillPlan picks storage items of the given category in item_id order and
// withdraws as many of each as fit in the remaining cargo space. An item too big
// for what is left is skipped, so smaller items later in the order can still
// top the hold off. A zero unit size is treated as 1.
func buildFillPlan(items []storageItem, available int, category string, categoryOf func(string) string) []fillStep {
	matches := make([]storageItem, 0, len(items))
	for _, it := range items {
		if it.Quantity >= 1 && strings.EqualFold(categoryOf(it.ItemID), category) {
			matches = append(matches, it)
		}
	}
	slices.SortFunc(matches, func(a, b storageItem) int { return strings.Compare(a.ItemID, b.ItemID) })

	var plan []fillStep
	remaining := available
	for _, it := range matches {
		if remaining <= 0 {
			break
		}
		size := max(it.Size, 1)
		qty := min(int(it.Quantity), remaining/size)
		if qty <= 0 {
			continue
		}
		plan = append(plan, fillStep{ItemID: it.ItemID, Quantity: qty, Space: qty * size})
		remaining -= qty * size
	}
	return plan
}

// runFillCargo reads free cargo space and station storage, then withdraws items
// of one category (alphabetically) until the hold is full. Must be docked.
func runFillCargo(ctx context.Context, client game.GameClient, opts fillCargoOptions) error {
	if err := client.GetCargo(ctx); err != nil {
		return fmt.Errorf("fill_cargo: get_cargo: %w", err)
	}
	var cargo struct {
		Available int `json:"available"`
		Capacity  int `json:"capacity"`
	}
	if raw := client.GetRawJSON("cargo"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &cargo); err != nil {
			return fmt.Errorf("fill_cargo: parse cargo: %w", err)
		}
	}
	if cargo.Available <= 0 {
		fmt.Printf("Cargo is full (capacity %d); nothing to do.\n", cargo.Capacity)
		return nil
	}

	if err := client.ViewStorage(ctx); err != nil {
		return fmt.Errorf("fill_cargo: view_storage (are you docked?): %w", err)
	}
	var storage struct {
		Items []storageItem `json:"items"`
	}
	if raw := client.GetRawJSON("storage"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &storage); err != nil {
			return fmt.Errorf("fill_cargo: parse storage: %w", err)
		}
	}

	plan := buildFillPlan(storage.Items, cargo.Available, opts.category, itemCategory)
	if len(plan) == 0 {
		fmt.Printf("No %q items in storage fit the %d free cargo space.\n", opts.category, cargo.Available)
		return nil
	}

	total := 0
	for _, s := range plan {
		total += s.Space
	}
	verb := "Withdrawing"
	if opts.dryRun {
		verb = "Would withdraw"
	}
	fmt.Printf("%s %d stack(s) of %q, %d of %d free cargo space:\n", verb, len(plan), opts.category, total, cargo.Available)

	moved := 0
	for _, s := range plan {
		if opts.dryRun {
			fmt.Printf("  %-32s %6d  (space %d)\n", s.ItemID, s.Quantity, s.Space)
			continue
		}
		if err := client.WithdrawItems(ctx, s.ItemID, float64(s.Quantity)); err != nil {
			fmt.Printf("  ❌ %-30s %6d  %v\n", s.ItemID, s.Quantity, err)
			continue
		}
		moved += s.Space
		fmt.Printf("  ✓ %-30s %6d  (space %d)\n", s.ItemID, s.Quantity, s.Space)
	}
	if !opts.dryRun {
		fmt.Printf("Moved %d cargo space into the hold.\n", moved)
	}
	return nil
}
