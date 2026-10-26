package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// stationParts rewrites `station <action> [flags...]` to
// `station --action <action> [flags...]` so the generic passthrough sends
// the sub-action under the key the server reads; a bare positional would go
// out as arg1 and be refused with invalid_payload. Already-flagged and bare
// `station` invocations pass through unchanged.
func stationParts(parts []string) []string {
	if len(parts) < 2 || strings.HasPrefix(parts[1], "--") {
		return parts
	}
	out := make([]string, 0, len(parts)+1)
	out = append(out, parts[0], "--action", parts[1])
	return append(out, parts[2:]...)
}

// Parameter types for the station command, from the server's get_commands
// notes (category "base").
var (
	stationTextParams = []string{"action", "name", "description", "service", "access", "player", "faction"}
	stationBoolParams = []string{"public", "allow_outsiders", "auto_buy_fuel"}
	stationIntParams  = []string{"fee_percent", "price"}
)

// stationActionParams lists, per action, the parameters it needs, in the
// order bare positional values fill them (`station set_public true`). The
// last parameter of set_name/set_description takes the rest of the line.
// Every listed parameter is required: the server reads a missing one as its
// zero value (false / 0) rather than refusing, so `station set_public` with
// no value would silently make the station private.
var stationActionParams = map[string][]string{
	"set_name":           {"name"},
	"set_description":    {"description"},
	"set_public":         {"public"},
	"set_build_policy":   {"allow_outsiders"},
	"set_service_access": {"service", "access"},
	"set_market_fee":     {"fee_percent"},
	"set_refuel_price":   {"price"},
	"set_repair_price":   {"price"},
	"set_auto_buy_fuel":  {"auto_buy_fuel"},
	"set_contraband":     {"contraband_items"},
	"allow_player":       {"player"},
	"remove_player":      {"player"},
	"ban":                {"player"},
	"unban":              {"player"},
	"allow_faction":      {"faction"},
	"remove_faction":     {"faction"},
}

// stationPayload builds a typed station payload from `station --action <a>
// [value...] [--param value...]` (run stationParts first). Bare values fill
// the action's parameters in order; explicit --flags win. Booleans go out as
// JSON bools, fee/price as numbers, names and ids as strings even when all
// digits, and contraband_items as an array -- empty ("[]") stays an empty
// array because the server reads it as "allow all", unlike null.
func stationPayload(parts []string) (map[string]any, error) {
	keys := slices.Concat(stationTextParams, stationBoolParams, stationIntParams, []string{"contraband_items"})
	payload, err := parseFlagArgs(parts[1:], keys...)
	if err != nil {
		return nil, err
	}
	if _, ok := payload["action"]; !ok {
		payload["action"] = "info"
	}
	action := fmt.Sprint(payload["action"])
	params := stationActionParams[action]

	positional, _ := partitionFlags(parts[1:])
	for i, k := range params {
		if i >= len(positional) {
			break
		}
		v := positional[i]
		if i == len(params)-1 && (k == "name" || k == "description") {
			v = strings.Join(positional[i:], " ")
		}
		if _, set := payload[k]; !set {
			payload[k] = v
		}
	}
	for _, k := range params {
		if _, ok := payload[k]; !ok {
			return nil, fmt.Errorf("station %s: missing %s (e.g. station %s <%s> or --%s <value>)",
				action, k, action, strings.Join(params, "> <"), k)
		}
	}

	for _, k := range stationTextParams {
		if v, ok := payload[k]; ok {
			payload[k] = fmt.Sprint(v)
		}
	}
	for _, k := range stationIntParams {
		if v, ok := payload[k]; ok {
			n, err := strconv.Atoi(fmt.Sprint(v))
			if err != nil {
				return nil, fmt.Errorf("station %s: --%s must be a whole number, got %q", action, k, fmt.Sprint(v))
			}
			payload[k] = n
		}
	}
	if err := coerceBoolFlags(payload, stationBoolParams...); err != nil {
		return nil, fmt.Errorf("station: %w", err)
	}
	if v, ok := payload["contraband_items"]; ok {
		raw := fmt.Sprint(v)
		if strings.Trim(raw, "[]{} \t\"'") == "" {
			payload["contraband_items"] = []string{} // explicit: allow all
		} else {
			items, err := parseFacilityList("contraband_items", raw)
			if err != nil {
				return nil, err
			}
			payload["contraband_items"] = items
		}
	}
	return payload, nil
}
