package main

import (
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/fitting"
)

// formatCheck renders a MaxFit result for one ship + module.
func formatCheck(shipName, moduleName string, r fitting.FitResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s: max %d\n", moduleName, shipName, r.MaxCount)
	fmt.Fprintf(&b, "  %s %d/%d   CPU %d/%d   power %d/%d\n",
		r.SlotType, r.SlotsUsed, r.SlotsAvail, r.CPUUsed, r.CPUAvail, r.PowerUsed, r.PowerAvail)
	if r.BindingConstraint != "" {
		fmt.Fprintf(&b, "  limited by: %s\n", r.BindingConstraint)
	}
	for _, w := range r.SkillWarnings {
		fmt.Fprintf(&b, "  note: %s\n", w)
	}
	return b.String()
}

// formatShips renders the reverse-query result list.
func formatShips(moduleName string, count int, fits []fitting.ShipFit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Ships that fit >= %d x %s:\n", count, moduleName)
	if len(fits) == 0 {
		b.WriteString("  No ships qualify.\n")
		return b.String()
	}
	for _, sf := range fits {
		fmt.Fprintf(&b, "  [t%d] %-24s max %d\n", sf.Ship.Tier, sf.Ship.Name, sf.Result.MaxCount)
	}
	return b.String()
}
