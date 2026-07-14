package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatEspionage renders an espionage action_result. The command's whole
// output is a narrative account of the operation, so the story is the payload
// worth showing; the outcome (and intel type, when the spy actually turned
// something up) is surfaced as a header above it.
//
// Returns "" if the payload can't be parsed (caller falls back to JSON).
// Handles both the flat OK payload and an action_result-wrapped frame.
func formatEspionage(raw []byte) string {
	var resp serverapi.EspionageResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}
	if resp.Outcome == "" && resp.Story == "" {
		return ""
	}

	var b strings.Builder
	if resp.IntelType != "" {
		fmt.Fprintf(&b, "Espionage: %s (%s)\n", resp.Outcome, resp.IntelType)
	} else {
		fmt.Fprintf(&b, "Espionage: %s\n", resp.Outcome)
	}
	if resp.Story != "" {
		fmt.Fprintf(&b, "\n%s\n", resp.Story)
	}
	return b.String()
}
