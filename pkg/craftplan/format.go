package craftplan

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
)

// FormatCraftableOpts controls FormatCraftableCompact rendering.
type FormatCraftableOpts struct {
	StationID string
	Reachable bool
	// Total is the pre-truncation count of matching recipes. If > len(rows)
	// the footer prints "showing N / TOTAL" so the operator knows to widen
	// --max. If 0, treated as equal to len(rows).
	Total int
	// SortBy is surfaced in the footer so the operator can see which sort
	// produced the row order (the engine applies the sort; the formatter
	// just labels it).
	SortBy SortMode
}

// FormatCraftableCompact renders the compact table view of craftable rows.
// Matches the layout in the design spec.
func FormatCraftableCompact(rows []CraftableRow, opts FormatCraftableOpts) string {
	var b bytes.Buffer

	station := opts.StationID
	if station == "" {
		station = "(not docked)"
	}

	if opts.Reachable {
		direct := 0
		for _, r := range rows {
			if r.Depth <= 1 {
				direct++
			}
		}
		fmt.Fprintf(&b, "%d recipes reachable at %s (%d direct, %d via 1+ intermediate craft)\n\n",
			len(rows), station, direct, len(rows)-direct)
	} else {
		fmt.Fprintf(&b, "%d recipes buildable at %s (cargo + storage; legal)\n\n",
			len(rows), station)
	}

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	// TIME column dropped: one craft call advances one tick (~10s) regardless
	// of recipe.crafting_time, and the per-call output already scales with the
	// agent's crafting skill. The recipe time is still on detail view.
	if opts.Reachable {
		_, _ = fmt.Fprintln(tw, "RECIPE\tOUTPUT\tCATEGORY\tCAN_MAKE\tVIA")
	} else {
		_, _ = fmt.Fprintln(tw, "RECIPE\tOUTPUT\tCATEGORY\tCAN_MAKE")
	}
	for _, r := range rows {
		output := fmt.Sprintf("%s x%d", r.OutputItemID, r.OutputQuantity)
		cm := canMakeStr(r.CanMake)
		if opts.Reachable {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				r.Recipe.ID, output, r.Recipe.Category, cm, depthStr(r.Depth))
		} else {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				r.Recipe.ID, output, r.Recipe.Category, cm)
		}
	}
	_ = tw.Flush()

	total := max(opts.Total, len(rows))
	fmt.Fprintf(&b, "\n(showing %d / %d; sort: %s. Pass --max N to widen, --sort=name|category|can_make_asc|id to reorder, --detail to drill in.)\n",
		len(rows), total, opts.SortBy)
	return b.String()
}

func canMakeStr(n int) string {
	if n == math.MaxInt {
		return "∞"
	}
	return fmt.Sprintf("%d", n)
}

func depthStr(d int) string {
	switch {
	case d <= 1:
		return "direct"
	default:
		return fmt.Sprintf("+%d crafts", d-1)
	}
}

// FormatPlan renders a PlanResult to a string matching the design spec.
func FormatPlan(res *PlanResult) string {
	var b bytes.Buffer

	fmt.Fprintf(&b, "plan: %s x%d units → %d run(s)  (%s, %gs/batch, %d/run)\n\n",
		res.Recipe.ID, res.Quantity, res.Runs, res.Recipe.Category, res.Recipe.CraftingTime, outputPerRun(res.Recipe))

	if len(res.BlockedSkill) > 0 {
		keys := make([]string, 0, len(res.BlockedSkill))
		for k := range res.BlockedSkill {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintln(&b, "blocked by skill:")
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: need +%d levels\n", k, res.BlockedSkill[k])
		}
		fmt.Fprintln(&b)
	}
	if res.BlockedIllegal {
		fmt.Fprintf(&b, "blocked: recipe is illegal at this station (%s)\n\n", res.StationID)
	}
	if res.BlockedPassive {
		fmt.Fprintln(&b, "blocked: Ship Passive recipe — runs automatically on ships that have this capability built in; it cannot be crafted manually.")
		fmt.Fprintln(&b)
	}
	if res.FacilityOnlyNoAlt {
		fmt.Fprintln(&b, "note: facility-only recipe — must be built at a facility; no hand-craftable alternative recipe exists for this output.")
		fmt.Fprintln(&b)
	}

	// Inputs table.
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	hasFaction := false
	for _, row := range res.Inputs {
		if row.HaveFaction > 0 {
			hasFaction = true
			break
		}
	}
	if hasFaction {
		_, _ = fmt.Fprintln(tw, "ITEM\tNEED\tCARGO\tSTORAGE\tFACTION\tSHORT")
	} else {
		_, _ = fmt.Fprintln(tw, "ITEM\tNEED\tCARGO\tSTORAGE\tSHORT")
	}
	short := 0
	for _, row := range res.Inputs {
		shortStr := "–"
		if row.Short > 0 {
			shortStr = fmt.Sprintf("%d ✗", row.Short)
			short++
		}
		if hasFaction {
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\n",
				row.ItemID, row.Need, row.HaveCargo, row.HaveStorage, row.HaveFaction, shortStr)
		} else {
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\n",
				row.ItemID, row.Need, row.HaveCargo, row.HaveStorage, shortStr)
		}
	}
	_ = tw.Flush()

	// Intermediates (reachable mode only).
	if len(res.Intermediates) > 0 {
		fmt.Fprintln(&b, "\nintermediate crafts needed (in order):")
		for i, ic := range res.Intermediates {
			fmt.Fprintf(&b, "  %d. %s\tx%d\t→ %s\n", i+1, ic.RecipeID, ic.BatchesNeeded, ic.OutputItemID)
		}
	}

	// Footer.
	fmt.Fprintln(&b)
	switch {
	case res.Ready:
		fmt.Fprintln(&b, "summary: ✓ ready to craft")
		fmt.Fprintf(&b, "→ craft %s %d\n", res.Recipe.ID, res.Quantity)
	case short > 0:
		var tail []string
		for _, row := range res.Inputs {
			if row.Short > 0 {
				tail = append(tail, fmt.Sprintf("%d %s", row.Short, row.ItemID))
				if len(tail) == 3 {
					break
				}
			}
		}
		fmt.Fprintf(&b, "summary: ✗ %d input(s) short (need %s)\n", short, strings.Join(tail, ", "))
	default:
		fmt.Fprintln(&b, "summary: ✗ blocked (see notes above)")
	}
	return b.String()
}

// FormatCraftableDetail renders each row as its own per-recipe block instead
// of one table. Use when the operator wants depth over breadth.
func FormatCraftableDetail(rows []CraftableRow, opts FormatCraftableOpts) string {
	var b bytes.Buffer
	station := opts.StationID
	if station == "" {
		station = "(not docked)"
	}
	fmt.Fprintf(&b, "%d recipe(s) at %s\n", len(rows), station)
	for i, r := range rows {
		if i > 0 {
			fmt.Fprintln(&b, "─────────────────────────────────────────────")
		}
		fmt.Fprintf(&b, "\n%s — %s (%s, %gs/batch, can_make=%s)\n",
			r.Recipe.ID, r.Recipe.Name, r.Recipe.Category, r.Recipe.CraftingTime, canMakeStr(r.CanMake))
		if len(r.Recipe.Inputs) > 0 {
			fmt.Fprintln(&b, "  inputs:")
			for _, in := range r.Recipe.Inputs {
				fmt.Fprintf(&b, "    %s x%d\n", in.ItemID, in.Quantity)
			}
		}
		if len(r.Recipe.Outputs) > 0 {
			fmt.Fprintln(&b, "  outputs:")
			for _, out := range r.Recipe.Outputs {
				fmt.Fprintf(&b, "    %s x%d\n", out.ItemID, out.Quantity)
			}
		}
	}
	return b.String()
}
