package main

import (
	"fmt"
	"slices"
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

// stationPayload builds a typed station payload from `station --action <a>
// [--param value...]` (run stationParts first). Booleans go out as JSON bools,
// fee/price as numbers, names and ids as strings even when all digits, and
// contraband_items as an array -- empty ("[]") stays an empty array because
// the server reads it as "allow all", unlike null.
func stationPayload(parts []string) (map[string]any, error) {
	keys := slices.Concat(stationTextParams, stationBoolParams, stationIntParams, []string{"contraband_items"})
	payload, err := parseFlagArgs(parts[1:], keys...)
	if err != nil {
		return nil, err
	}
	for _, k := range stationTextParams {
		if v, ok := payload[k]; ok {
			payload[k] = fmt.Sprint(v)
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
	if _, ok := payload["action"]; !ok {
		payload["action"] = "info"
	}
	return payload, nil
}
