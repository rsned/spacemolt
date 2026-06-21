package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatCommissionStatus renders a commission_status response: one block per
// active/ready commission with the ship, shipyard, price paid, and either the
// built-ship id (when ready) or the build progress. Materials-provided
// commissions also show the required-vs-gathered tally. Returns "" if the
// payload can't be parsed (caller falls back to JSON). Handles both the flat
// OK payload and an action_result-wrapped frame.
func formatCommissionStatus(raw []byte) string {
	var resp serverapi.CommissionStatusResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "=== Commissions (%d) ===\n", len(resp.Commissions))
	if len(resp.Commissions) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}

	for i, c := range resp.Commissions {
		name := c.ShipName
		if name == "" {
			name = c.ShipClassID
		}
		// Header carries the built ship id (the thing you switch_ship to) right
		// after the name + class, once the build is ready and an id exists.
		header := fmt.Sprintf("%d. %s (%s)", i+1, name, c.ShipClassID)
		if c.BuiltShipID != "" {
			header += " " + c.BuiltShipID
		}
		fmt.Fprintf(&b, "\n%s — %s\n", header, strings.ToUpper(c.Status))

		if c.BaseName != "" || c.BaseID != "" {
			base := c.BaseName
			if base == "" {
				base = c.BaseID
			}
			fmt.Fprintf(&b, "   Shipyard:    %s\n", base)
		}
		fmt.Fprintf(&b, "   Credits paid: %d cr\n", c.CreditsPaid)
		if c.EarmarkedCredits > 0 {
			fmt.Fprintf(&b, "   Earmarked:   %d cr\n", c.EarmarkedCredits)
		}

		ready := c.Status == "ready" && c.BuiltShipID != ""
		if !ready && c.TicksRemaining > 0 {
			hours := float64(c.TicksRemaining) * 10 / 3600
			fmt.Fprintf(&b, "   Remaining:   %d ticks (~%.1f h)\n", c.TicksRemaining, hours)
		}

		if c.MaterialsProvided && len(c.RequiredMaterials) > 0 {
			fmt.Fprintln(&b, "   Materials:")
			ids := make([]string, 0, len(c.RequiredMaterials))
			for id := range c.RequiredMaterials {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				fmt.Fprintf(&b, "     %-24s %d/%d\n", id, c.MaterialsGathered[id], c.RequiredMaterials[id])
			}
		}

		// Bottom line. Finished commissions auto-deliver the ship into the
		// fleet (docked at the build station) — there is no claim step; use
		// switch_ship to fly it. In-progress ones show the bare id (for
		// cancel_commission / supply_commission).
		if ready {
			fmt.Fprintf(&b, "   Ready — ship delivered to fleet (switch_ship to fly)  %s\n", c.CommissionID)
		} else {
			fmt.Fprintf(&b, "   Commission:  %s\n", c.CommissionID)
		}
	}

	return b.String()
}
