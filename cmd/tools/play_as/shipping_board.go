package main

import (
	"context"
	"fmt"
	"strings"
)

// boardCommand handles the two freight board reads: `shipping list` (the
// station you are docked at) and `shipping list_all` (v0.724.0: every station
// you have charted, read from any dock with a mission service). Both take an
// optional leading sort and --include_ineligible, which also shows the runs
// you cannot accept yet, each with its ineligible_reason; --filter_origin
// narrows list_all to one origin station.
//
// A run seen on list_all is accepted at its ORIGIN: fly there first.
//
// parts is the whole command line, so parts[0] is "shipping" and parts[1] is
// the action.
func boardCommand(ctx context.Context, c shippingSender, parts []string) error {
	if len(parts) < 2 {
		return fmt.Errorf("usage: shipping <list|list_all> [sort] [--include_ineligible true]")
	}
	action := strings.ToLower(parts[1])
	args := parts[2:]
	payload := map[string]any{}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		payload["sort"] = args[0]
		args = args[1:]
	}
	keys := []string{"include_ineligible", "page", "per_page"}
	if action == "list_all" {
		keys = append(keys, "filter_origin")
	} else if hasFlag(args, "filter_origin") {
		return fmt.Errorf("--filter_origin belongs to `shipping list_all`; `shipping list` reads only this station")
	}
	flags, err := shipperFlags(args, keys...)
	if err != nil {
		return err
	}
	for k, v := range flags {
		if k == "include_ineligible" {
			v = flagBool(v)
		}
		payload[k] = v
	}

	return c.Shipping(ctx, action, payload)
}

// hasFlag reports whether args carries --name, in either the `--name value`
// or the `--name=value` form.
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == "--"+name || strings.HasPrefix(a, "--"+name+"=") {
			return true
		}
	}

	return false
}
