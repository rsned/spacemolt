package main

import "strings"

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
