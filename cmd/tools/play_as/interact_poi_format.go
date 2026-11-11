package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatInteractPOI renders an interact_poi reply: the message, whether data
// was copied, items recovered, and whether a mission was started or completed.
// "" when the reply does not parse, so the caller falls back to raw JSON.
func formatInteractPOI(raw []byte) string {
	var r serverapi.InteractPOIResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &r); err != nil || (r.Message == "" && r.POIID == "") {
		return ""
	}
	var b strings.Builder
	if r.POIID != "" {
		fmt.Fprintf(&b, "Interaction at %s\n", r.POIID)
	}
	if r.Message != "" {
		fmt.Fprintf(&b, "%s\n", r.Message)
	}
	if r.DataCopied {
		b.WriteString("📀 Data copied.\n")
	}
	if len(r.ItemsRecovered) > 0 {
		b.WriteString("Recovered:\n")
		for _, id := range slices.Sorted(maps.Keys(r.ItemsRecovered)) {
			fmt.Fprintf(&b, "  %-28s x%d\n", id, r.ItemsRecovered[id])
		}
	}
	if len(r.Mission) > 0 && string(r.Mission) != "null" {
		fmt.Fprintf(&b, "📜 Mission: %s\n", compactJSON(r.Mission))
	}
	if r.Completion != nil {
		b.WriteString("✅ Mission completed.\n")
	}
	if len(r.PassengersBoarded) > 0 && string(r.PassengersBoarded) != "null" {
		fmt.Fprintf(&b, "🧳 Passengers boarded: %s\n", compactJSON(r.PassengersBoarded))
	}
	return b.String()
}

// compactJSON is raw on one line, for fields whose shape is not pinned yet.
func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}
