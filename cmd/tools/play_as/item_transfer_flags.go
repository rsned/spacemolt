package main

import (
	"fmt"
	"strings"
)

// itemTransferFlagKeys are the options deposit_items, withdraw_items,
// faction_deposit_items and faction_withdraw_items forward to the server.
// bucket names the faction storage extension items come from; dest_bucket the
// one they go to, where an empty value is the faction's main store. Station
// maintenance draws only from the main store, so a reserve kept in a bucket
// reaches upkeep only by a --bucket=<b> --dest_bucket= move.
var itemTransferFlagKeys = []string{"source", "target", "bucket", "dest_bucket"}

// itemTransferFlagsUsage is appended to the four commands' usage lines.
const itemTransferFlagsUsage = "[--source=<scope>] [--target=<scope>] [--bucket=<bucket>] [--dest_bucket=<bucket>|--dest_bucket=]"

// parseItemTransferFlags parses the --flags of an item transfer command.
//
// Unlike parseFlagArgs it rejects an unknown flag instead of dropping it: these
// commands move items, and a silently ignored --buckt turns a bucket withdraw
// into a main-store withdraw. A bare --dest_bucket (no value) means the main
// store, the same as --dest_bucket=, rather than parseFlagArgs's "true".
func parseItemTransferFlags(args []string) (map[string]any, error) {
	allowed := make(map[string]bool, len(itemTransferFlagKeys))
	for _, k := range itemTransferFlagKeys {
		allowed[k] = true
	}
	out := map[string]any{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		trimmed, ok := strings.CutPrefix(arg, "--")
		if !ok {
			return nil, fmt.Errorf("unexpected argument %q (options take the form --key=value)", arg)
		}
		key, value, hasValue := strings.Cut(trimmed, "=")
		if !allowed[key] {
			return nil, fmt.Errorf("unknown option --%s (allowed: --%s)", key, strings.Join(itemTransferFlagKeys, ", --"))
		}
		if !hasValue {
			switch {
			case key == "dest_bucket" && (i+1 >= len(args) || strings.HasPrefix(args[i+1], "--")):
				value = "" // bare --dest_bucket: the main store
			case i+1 < len(args) && !strings.HasPrefix(args[i+1], "--"):
				i++
				value = args[i]
			default:
				return nil, fmt.Errorf("option --%s needs a value", key)
			}
		}
		// Every value is sent as a string: scopes are words, and a bucket is
		// addressed by name or id, never by number.
		out[key] = value
	}
	return out, nil
}
