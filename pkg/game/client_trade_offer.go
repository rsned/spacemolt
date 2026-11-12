package game

import (
	"fmt"
	"strings"
)

// formatTradeOffer renders a trade_offer_received push as one log line. The
// first live one (2026-11-12) was a stranded player offering 5,000 cr for a
// single fuel cell; it expired unseen because the push had no handler. Fields
// are read from the raw payload (offerer_name, note, offer_credits,
// offer_items, request_items, request_credits, expires_at, trade_id), so a
// missing one degrades to a placeholder rather than dropping the line.
func formatTradeOffer(payload map[string]any) string {
	from, _ := payload["offerer_name"].(string)
	if from == "" {
		from = "an unknown player"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "💱 TRADE OFFER from %s", from)
	if note, _ := payload["note"].(string); note != "" {
		fmt.Fprintf(&b, ": %q", note)
	}
	fmt.Fprintf(&b, " — offers %s; wants %s",
		tradeSide(payload["offer_credits"], payload["offer_items"]),
		tradeSide(payload["request_credits"], payload["request_items"]))
	if exp, _ := payload["expires_at"].(string); exp != "" {
		fmt.Fprintf(&b, "; expires %s", exp)
	}
	if id, _ := payload["trade_id"].(string); id != "" {
		fmt.Fprintf(&b, " [trade %s]", id)
	}
	return b.String()
}

// tradeSide describes one side of an offer: credits plus "N x item" lines.
func tradeSide(credits, items any) string {
	var parts []string
	if c, ok := credits.(float64); ok && c > 0 {
		parts = append(parts, fmt.Sprintf("%.0f cr", c))
	}
	if list, ok := items.([]any); ok {
		for _, it := range list {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			id, _ := m["item_id"].(string)
			qty, _ := m["quantity"].(float64)
			parts = append(parts, fmt.Sprintf("%.0f x %s", qty, id))
		}
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, " + ")
}
