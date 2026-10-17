package main

import (
	"fmt"
	"os"
	"strings"
)

// facilityArrayParams are the facility parameters openapi declares as
// `type: array`. Every other flag is a scalar string.
//
// buildFacilityPayload puts flag values into the payload verbatim, which made
// all three unusable: the server answered
//
//	invalid_payload: Parameter 'package_ids' must be an array, but received a string.
//
// Found live 2026-10-17 rebuilding the Crystal Infusion Still from its 42
// recovered material packages. The list is derived from the spec's
// /facility request body rather than guessed, so it is exactly items,
// job_ids and package_ids.
var facilityArrayParams = map[string]bool{
	"items":       true,
	"job_ids":     true,
	"package_ids": true,
}

// parseFacilityList turns a flag value into the string array the server wants.
//
// It accepts the comma-separated form and, because that is what anyone pastes
// from a document or a generated list, bracketed JSON (["a","b"]) and the
// brace/single-quote shapes people actually type. Quotes and brackets are
// stripped per element rather than by parsing JSON, so a half-quoted paste
// still works instead of failing on syntax.
//
// A value of "@<path>" reads the list from that file instead. Forty-two
// package ids is a 1,400-character command line, which is how a copy-paste
// doubling slipped in live; a file keeps the list reviewable and reusable. The
// file may hold a JSON array, one id per line, or a comma-separated line --
// whichever a person or a tool produced.
//
// It does NOT touch a "package:" prefix: the server accepts the raw id and the
// prefixed form equally, so rewriting either way would be a guess.
func parseFacilityList(key, raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)

	if path, ok := strings.CutPrefix(trimmed, "@"); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("--%s: read %s: %w", key, path, err)
		}
		// Newlines are a separator alongside commas, so a pasted column of ids
		// works as well as a JSON array.
		trimmed = strings.ReplaceAll(string(data), "\n", ",")
	}

	trimmed = strings.Trim(strings.TrimSpace(trimmed), "[]{}")

	var out []string
	seen := map[string]int{}
	for field := range strings.SplitSeq(trimmed, ",") {
		// Strip quotes and brackets PER ELEMENT, not just at the ends of the
		// whole value: a pretty-printed JSON array puts its closing "]" on its
		// own line, which becomes a field of its own once newlines are
		// separators.
		id := strings.Trim(strings.TrimSpace(field), "\"'[]{} \t\r")
		if id == "" {
			continue
		}
		if first, dup := seen[id]; dup {
			// A duplicate is a guaranteed server-side rejection, not a
			// harmless redundancy: pooled package contents must equal the
			// recipe inputs EXACTLY, and "any shortage or overage is rejected
			// before anything is touched". A copy-paste doubling sent 44 ids
			// for a 42-package set, which would have failed after the tick.
			return nil, fmt.Errorf("--%s: duplicate id %q (positions %d and %d); "+
				"pooled package contents must match the recipe exactly, so a repeat is rejected",
				key, id, first, len(out)+1)
		}
		seen[id] = len(out) + 1
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--%s was empty", key)
	}

	return out, nil
}
