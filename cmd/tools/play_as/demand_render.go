package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func renderDemandJSON(rep demandReport) string { //nolint:unused // used by demand_cmd.go (Task 9)
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":%q}\n", err.Error())
	}
	return string(b) + "\n"
}

func renderDemandStyled(rep demandReport) string { //nolint:unused // used by demand_cmd.go (Task 9)
	var sb strings.Builder
	if len(rep.Rows) == 0 {
		sb.WriteString("No captured demand matches. Visit stations and run view_market/sellable to fill the ledger, or `demand scan` for full depth.\n")
		return sb.String()
	}
	fmt.Fprintf(&sb, "Demand ledger — %d rows, total fulfillable value %.0f\n", len(rep.Rows), rep.TotalFulfill)
	fmt.Fprintf(&sb, "%-7s %-16s %8s %8s %8s %8s %6s  %s\n",
		"CLASS", "ITEM", "PRICE", "DEMAND", "ONHAND", "FILLVAL", "CRAFT", "STATION")
	for _, r := range rep.Rows {
		station := r.StationID
		if r.AgeStale {
			station += " (STALE)"
		}
		craft := ""
		if r.CanCraft > 0 {
			craft = fmt.Sprintf("%d", r.CanCraft)
		}
		name := r.ItemName
		if name == "" {
			name = r.ItemID
		}
		fmt.Fprintf(&sb, "%-7s %-16s %8.0f %8.0f %8.0f %8.0f %6s  %s\n",
			r.Class, truncateName(name, 16), r.Price, r.Quantity, r.OnHand, r.FulfillValue, craft, station)
	}
	return sb.String()
}
