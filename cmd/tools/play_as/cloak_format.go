package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatCloak renders a cloak action_result: the on/off state, cloak strength,
// and the server message. Returns "" if the payload can't be parsed (caller
// falls back to JSON). Handles both the flat OK payload and an
// action_result-wrapped frame.
func formatCloak(raw []byte) string {
	var resp serverapi.CloakResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}

	var b strings.Builder
	if resp.Enabled {
		fmt.Fprintf(&b, "Cloak: ENGAGED (strength %d)\n", resp.CloakStrength)
	} else {
		b.WriteString("Cloak: DISENGAGED\n")
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "%s\n", resp.Message)
	}
	return b.String()
}
