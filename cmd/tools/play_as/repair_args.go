package main

import (
	"fmt"
	"strings"
)

// repairArgs builds `repair`'s payload from either the positional form
//
//	repair <target>
//
// or the flag form
//
//	repair --target=<target> [--item_id=<id>] [--quantity=<n>]
//
// mirroring what `scan` already does for its target.
//
// repair accepted ONLY the flag form, and parseFlagArgs drops a token that does
// not start with "--" via a bare `continue`. So `repair fleet` produced an EMPTY
// payload and went out as a plain hull repair — which at a station SPENDS
// CREDITS. The operator asked for fleet hull status and was billed for a repair
// instead, with nothing in the output to say so. Silence was the whole bug:
// parseFlagArgs already rejects a single-dash flag loudly, so `-target` gave a
// helpful error while `fleet` gave a wrong action.
//
// target is NOT lowercased, unlike item_id. It carries a player id or username,
// which is case-sensitive: lowercasing it would make `repair ThomasEdison`
// unrepairable. "fleet" is the one reserved value and is already lowercase.
//
// A nil payload means "no arguments" — the caller uses the plain Repair path.
//
// parts is the whole command line, so parts[0] is "repair".
func repairArgs(parts []string) (map[string]any, error) {
	const usage = "usage: repair [<target>]\n" +
		"   or: repair --target=<player-id|fleet> [--item_id=<id>] [--quantity=<n>]\n" +
		"       target 'fleet' reports fleet hull status; a player id needs a Repair Arm module"

	args := parts[1:]
	if len(args) == 0 {
		return nil, nil
	}

	payload := make(map[string]any)

	// A leading bare token is the target. Reject a single-dash long flag here
	// rather than reading it as one: `repair -target fleet` would otherwise
	// become target="-target", which is the same silent-wrong-action class of
	// bug this function exists to close.
	if !strings.HasPrefix(args[0], "--") {
		if len(args[0]) > 1 && args[0][0] == '-' {
			if c := args[0][1]; (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				return nil, fmt.Errorf("flag %q must use two dashes: --%s\n%s",
					args[0], strings.TrimPrefix(args[0], "-"), usage)
			}
		}
		payload["target"] = args[0]
		args = args[1:]
	}

	// Name an unknown flag instead of dropping it. parseFlagArgs discards keys it
	// was not told about, which is right for callers parsing a subset and wrong
	// here: a misspelled --targt would vanish and the repair would run untargeted
	// — exactly the failure being fixed.
	known := map[string]bool{"target": true, "item_id": true, "quantity": true}
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			continue
		}
		key, _, _ := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !known[key] {
			return nil, fmt.Errorf("unknown flag --%s\n%s", key, usage)
		}
	}

	flags, err := parseFlagArgs(args, "target", "item_id", "quantity")
	if err != nil {
		return nil, err
	}
	for k, v := range flags {
		payload[k] = v
	}

	if v, ok := payload["quantity"]; ok {
		if n, ok := flagInt(v); ok {
			payload["quantity"] = n
		}
	}
	if v, ok := flagString(payload["item_id"]); ok {
		payload["item_id"] = strings.ToLower(v)
	}

	if len(payload) == 0 {
		return nil, nil
	}
	return payload, nil
}
