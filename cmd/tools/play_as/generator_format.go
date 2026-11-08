package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatGenerator renders a generator action_result: whether the docked ship's
// field generator is feeding the station grid, its output and fuel cost, and,
// once unlinked, what the link delivered. Returns "" if the payload can't be
// parsed (caller falls back to JSON).
func formatGenerator(raw []byte) string {
	var resp serverapi.GeneratorResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}

	var b strings.Builder
	if resp.Connected {
		fmt.Fprintf(&b, "Generator: CONNECTED to %s, output %d, %d fuel/tick (reserve %d), until tick %d\n",
			resp.BaseID, resp.Output, resp.FuelPerTick, resp.FuelReserve, resp.EndTick)
	} else {
		fmt.Fprintf(&b, "Generator: DISCONNECTED from %s\n", resp.BaseID)
	}
	if resp.PowerDelivered > 0 || resp.FuelBurned > 0 {
		fmt.Fprintf(&b, "Link so far: %d power delivered, %d fuel burned\n", resp.PowerDelivered, resp.FuelBurned)
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "%s\n", resp.Message)
	}
	return b.String()
}
