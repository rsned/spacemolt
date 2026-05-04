package main

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
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
