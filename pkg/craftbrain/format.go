package craftbrain

import (
	"fmt"
	"sort"
	"strings"
)

// Format renders a Plan for human review. The JSON DAG is the contract for
// Executor B; this is for the operator who has to approve it.
func Format(p *Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Build plan: %s x%d\n\n", p.Target, p.Quantity)

	if len(p.Nodes) == 0 {
		b.WriteString("  (nothing to do — already in stock)\n")
	}
	for _, n := range p.Nodes {
		switch n.Kind {
		case KindMine:
			fmt.Fprintf(&b, "  [%s] mine   %-24s x%d\n", n.ID, n.ItemID, n.Qty)
		case KindBuy:
			fmt.Fprintf(&b, "  [%s] buy    %-24s x%d  @ %s\n", n.ID, n.ItemID, n.Qty, orDash(n.StationID))
		case KindHaul:
			if n.Status == StatusUnknownRoute {
				fmt.Fprintf(&b, "  [%s] haul   %-24s x%d  %s -> %s (distance unknown, holder %s)%s\n",
					n.ID, n.ItemID, n.Qty, n.FromBase, n.ToBase, orDash(n.Holder), tag(n.Status))
			} else {
				fmt.Fprintf(&b, "  [%s] haul   %-24s x%d  %s -> %s (%d jumps, holder %s)%s\n",
					n.ID, n.ItemID, n.Qty, n.FromBase, n.ToBase, n.Jumps, orDash(n.Holder), tag(n.Status))
			}
		case KindCraft:
			site := n.StationID
			if n.FacilityID != "" {
				site = fmt.Sprintf("%s/%s", n.StationID, n.FacilityID)
			}
			fmt.Fprintf(&b, "  [%s] craft  %-24s x%d  %d runs of %s @ %s  fee %d, %.1f ticks%s\n",
				n.ID, n.ItemID, n.Qty, n.Runs, n.RecipeID, site, n.FeeTotal, n.TicksEst, tag(n.Status))
		case KindBlocked:
			fmt.Fprintf(&b, "  [%s] BLOCKED %-23s x%d  %s\n", n.ID, n.ItemID, n.Qty, n.Reason)
		}
	}

	if len(p.Surplus) > 0 {
		items := make([]string, 0, len(p.Surplus))
		for k := range p.Surplus {
			items = append(items, k)
		}
		sort.Strings(items)
		b.WriteString("\nLeftover surplus:\n")
		for _, k := range items {
			fmt.Fprintf(&b, "  %-24s x%d\n", k, p.Surplus[k])
		}
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "Total fee: %d credits\n", p.TotalFee)
	fmt.Fprintf(&b, "Total time: %.1f ticks (makespan estimate)\n", p.TotalTicks)
	fmt.Fprintf(&b, "Total haul: %d jumps\n", p.TotalHaulJumps)
	fmt.Fprintf(&b, "Catalog coverage: %d stations, %d/%d facility_only recipes known\n",
		p.Coverage.Stations, p.Coverage.FacilityOnlyCovered, p.Coverage.FacilityOnlyTotal)
	if p.Coverage.FacilityOnlyTotal > 0 && p.Coverage.FacilityOnlyCovered < p.Coverage.FacilityOnlyTotal {
		b.WriteString("  (a BLOCKED node may mean 'not swept yet', not 'impossible')\n")
	}
	for _, d := range p.Diagnostics {
		fmt.Fprintf(&b, "note: %s\n", d)
	}
	return b.String()
}

func tag(s Status) string {
	if s == StatusOK || s == "" {
		return ""
	}
	return "  [" + strings.ToUpper(string(s)) + "]"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
