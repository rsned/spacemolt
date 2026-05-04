package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// sellableOptions mirrors the flags accepted on the `sellable` REPL command.
// Filled in over later tasks; the v1 surface is small.
type sellableOptions struct {
	detail      bool  //nolint:unused // populated in a later task
	minProceeds int64 //nolint:unused // populated in a later task
}

// runSellable is the REPL entry point for the `sellable` command. It is the
// only function `executeCommand` calls — every other piece of this file is
// either pure (testable without a network) or a renderer.
func runSellable(client game.GameClient, ctx context.Context, opts sellableOptions, format outputFormat) error {
	state := client.GetState()
	if state == nil || !state.Doc {
		return fmt.Errorf("sellable: must be docked at a station with a market service")
	}
	// Subsequent tasks fill in: fetch (market+cargo+storage), build plan, render.
	_ = opts
	_ = format
	return fmt.Errorf("sellable: not implemented yet")
}

// sellableFill records one match of cargo against a single buy order.
type sellableFill struct {
	Price    float64 `json:"price"`
	Qty      float64 `json:"qty"`
	Proceeds float64 `json:"proceeds"`
}

// fillItem walks a sorted-by-price-desc copy of orders, taking
// min(remaining_cargo, order.quantity) from each, until cargo is exhausted
// or orders run out. Pure: no I/O, no globals. Tests live in
// sellable_test.go::TestFillItem.
//
// Returns: total filled quantity, total proceeds (price*qty summed), the
// proceeds-weighted average price (0 when nothing filled), and the per-fill
// breakdown in fill order.
//
// Defensive: skips orders whose PriceEach <= 0 or Quantity <= 0 so bad data
// can't manufacture phantom credits or burn cargo on no-op fills.
func fillItem(cargo float64, orders []serverapi.MarketOrder) (qty, proceeds, avg float64, fills []sellableFill) {
	if cargo <= 0 || len(orders) == 0 {
		return 0, 0, 0, nil
	}
	sorted := slices.Clone(orders)
	slices.SortStableFunc(sorted, func(a, b serverapi.MarketOrder) int {
		// Higher price first; stable so server order breaks ties.
		switch {
		case a.PriceEach > b.PriceEach:
			return -1
		case a.PriceEach < b.PriceEach:
			return 1
		default:
			return 0
		}
	})
	remaining := cargo
	for _, o := range sorted {
		if remaining <= 0 {
			break
		}
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		take := o.Quantity
		if take > remaining {
			take = remaining
		}
		fills = append(fills, sellableFill{
			Price:    o.PriceEach,
			Qty:      take,
			Proceeds: take * o.PriceEach,
		})
		qty += take
		proceeds += take * o.PriceEach
		remaining -= take
	}
	if qty > 0 {
		avg = proceeds / qty
	}
	return qty, proceeds, avg, fills
}
