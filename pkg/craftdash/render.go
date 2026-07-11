// Package craftdash renders the operator's live monitor for craft-plan
// execution: each plans.PlanRun's DAG as a mermaid flowchart (done nodes
// faded grey, dispatched nodes bold/gold, parked nodes red) plus a per-item
// progress table. It is the primary way an operator watches Executor B work
// a plan tick by tick.
package craftdash

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/overmind/plans"
)

// MermaidForPlan renders the plan DAG as mermaid flowchart source.
//
// State -> class: done -> "done" (faded grey), dispatched -> "active" (bold
// gold), parked -> "parked" (red); waiting nodes get no class (default
// mermaid styling). KindBlocked nodes always render parked-red even if, for
// some reason, their NodeRun.State was never flipped to NodeParked — a
// blocked node is never a healthy default-styled node.
//
// Node label is "<kind> <item> x<qty>", with "<br/>@<agent>" appended on
// active nodes that have a pinned agent, or "<br/>PARKED (<reason>): <detail>"
// on parked nodes. Synthetic haul (xfer) nodes render with kind "xfer".
//
// Edges are drawn consumer --> producer (DependsOn direction); an edge whose
// consumer (the arrow's source) is dispatched/active uses a thick link (==>)
// so the diagram highlights exactly what's being worked right now.
//
// Node ids are sanitized for mermaid: '/' and any rune outside
// [a-zA-Z0-9_-] become '_'. Label content pulled from plan data (item id,
// agent id, park detail) is stripped of characters that could break out of
// a mermaid quoted label or inject markup (", [, ], <, >).
func MermaidForPlan(pr *plans.PlanRun) string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	b.WriteString("  classDef done fill:#eee,color:#999,stroke:#ccc\n")
	b.WriteString("  classDef active fill:#fffbe6,stroke:#e6a700,stroke-width:3px\n")
	b.WriteString("  classDef parked fill:#ffe6e6,stroke:#cc0000,stroke-width:2px\n")

	for _, n := range pr.Nodes {
		id := sanitizeID(n.Node.ID)
		cls := classFor(n)
		if cls == "" {
			fmt.Fprintf(&b, "  %s[\"%s\"]\n", id, nodeLabel(n))
		} else {
			fmt.Fprintf(&b, "  %s[\"%s\"]:::%s\n", id, nodeLabel(n), cls)
		}
	}

	for _, n := range pr.Nodes {
		arrow := "-->"
		if n.State == plans.NodeDispatched {
			arrow = "==>"
		}
		for _, dep := range n.Node.DependsOn {
			fmt.Fprintf(&b, "  %s %s %s\n", sanitizeID(n.Node.ID), arrow, sanitizeID(dep))
		}
	}

	return b.String()
}

// classFor returns the mermaid classDef name for a node, or "" for the
// default (waiting) style.
func classFor(n *plans.NodeRun) string {
	if n.State == plans.NodeParked || n.Node.Kind == craftbrain.KindBlocked {
		return "parked"
	}
	switch n.State {
	case plans.NodeDone:
		return "done"
	case plans.NodeDispatched:
		return "active"
	default:
		return ""
	}
}

// kindLabel is the node's kind word shown in its label; synthetic hauls
// (cross-station transfers inserted by the accept transform) render as
// "xfer" rather than "haul" so the operator can tell them apart from a
// plan-authored haul leaf at a glance.
func kindLabel(n *plans.NodeRun) string {
	if n.Synthetic && n.Node.Kind == craftbrain.KindHaul {
		return "xfer"
	}
	return string(n.Node.Kind)
}

// nodeLabel builds the mermaid node label text (pre-escaping, escaping is
// applied to each dynamic field individually so the literal "<br/>" markup
// this function inserts is never itself stripped).
func nodeLabel(n *plans.NodeRun) string {
	label := fmt.Sprintf("%s %s x%d", kindLabel(n), escapeLabel(n.Node.ItemID), n.Node.Qty)

	switch {
	case classFor(n) == "parked":
		label += "<br/>PARKED (" + escapeLabel(parkReason(n)) + ")"
		if detail := escapeLabel(parkDetail(n)); detail != "" {
			label += ": " + detail
		}
	case n.State == plans.NodeDispatched && n.Agent != "":
		label += "<br/>@" + escapeLabel(n.Agent)
	}
	return label
}

// parkReason returns the NodeRun's park reason, falling back to the
// craftbrain-level blocked status when a KindBlocked node's Park field was
// never populated by the accept transform.
func parkReason(n *plans.NodeRun) string {
	if n.Park != "" {
		return string(n.Park)
	}
	if n.Node.Kind == craftbrain.KindBlocked {
		return string(craftbrain.StatusBlocked)
	}
	return "parked"
}

// parkDetail mirrors parkReason's fallback for the human-readable detail.
func parkDetail(n *plans.NodeRun) string {
	if n.ParkDetail != "" {
		return n.ParkDetail
	}
	if n.Node.Kind == craftbrain.KindBlocked {
		return n.Node.Reason
	}
	return ""
}

// mermaidLabelReplacer strips/replaces characters that would break out of a
// mermaid quoted node label or inject markup: double quotes end the quoted
// label early, [ ] are mermaid node-shape delimiters, and < > could open
// arbitrary HTML inside the label (mermaid labels otherwise allow raw HTML
// such as the <br/> this package inserts deliberately).
var mermaidLabelReplacer = strings.NewReplacer(
	`"`, "'",
	"[", "(",
	"]", ")",
	"<", "",
	">", "",
)

func escapeLabel(s string) string {
	return mermaidLabelReplacer.Replace(s)
}

// idReplacer maps any rune outside [a-zA-Z0-9_-] to '_' for mermaid node ids.
func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Page renders the full HTML for all runs: per-plan header (id, status,
// spent/cap), the mermaid <pre class="mermaid">, and the progress table.
func Page(runs []*plans.PlanRun, refreshSec int) []byte {
	return PageWithErrors(runs, nil, refreshSec)
}

// PageWithErrors is Page plus a visible warning banner listing any
// LoadAllRuns errors, so a corrupt state file never silently disappears from
// the operator's view.
func PageWithErrors(runs []*plans.PlanRun, loadErrs []error, refreshSec int) []byte {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	if refreshSec > 0 {
		fmt.Fprintf(&b, "<meta http-equiv=\"refresh\" content=\"%d\">\n", refreshSec)
	}
	b.WriteString("<title>Craft Plan Dashboard</title>\n")
	b.WriteString(`<script src="/assets/mermaid.min.js"></script>` + "\n")
	b.WriteString(styleBlock)
	b.WriteString("</head>\n<body>\n")

	b.WriteString("<header><h1>Craft Plan Dashboard</h1></header>\n")

	if len(loadErrs) > 0 {
		b.WriteString(`<div class="warn-banner"><strong>Failed to load ` +
			fmt.Sprintf("%d plan state file(s):</strong><ul>\n", len(loadErrs)))
		for _, e := range loadErrs {
			fmt.Fprintf(&b, "<li>%s</li>\n", html.EscapeString(e.Error()))
		}
		b.WriteString("</ul></div>\n")
	}

	if len(runs) == 0 {
		b.WriteString(`<p class="subtle">no active plans</p>` + "\n")
	} else {
		for _, pr := range runs {
			renderPlanSection(&b, pr)
		}
	}

	b.WriteString("<script>mermaid.initialize({startOnLoad: true});</script>\n")
	b.WriteString("</body>\n</html>\n")
	return []byte(b.String())
}

func renderPlanSection(b *strings.Builder, pr *plans.PlanRun) {
	capText := "unbounded"
	if pr.Manifest.BudgetCap > 0 {
		capText = comma(pr.Manifest.BudgetCap)
	}
	fmt.Fprintf(b, "<section>\n<h2>%s <span class=\"status\">%s</span></h2>\n",
		html.EscapeString(pr.Manifest.PlanID), html.EscapeString(pr.Status))
	fmt.Fprintf(b, "<p class=\"subtle\">spent %s / cap %s</p>\n", comma(pr.Spent), capText)

	fmt.Fprintf(b, "<pre class=\"mermaid\">\n%s</pre>\n", html.EscapeString(MermaidForPlan(pr)))

	renderProgressTable(b, pr)
	b.WriteString("</section>\n")
}

func renderProgressTable(b *strings.Builder, pr *plans.PlanRun) {
	progress := pr.ItemProgress()
	if len(progress) == 0 {
		return
	}
	items := make([]string, 0, len(progress))
	for item := range progress {
		items = append(items, item)
	}
	sort.Strings(items)

	b.WriteString("<table>\n<thead><tr><th>Item</th><th class=\"num\">Progress</th></tr></thead>\n<tbody>\n")
	for _, item := range items {
		p := progress[item]
		fmt.Fprintf(b, "<tr><td>%s</td><td class=\"num\">%s / %s</td></tr>\n",
			html.EscapeString(item), comma(p.Done), comma(p.Total))
	}
	b.WriteString("</tbody>\n</table>\n")
}

// comma formats an integer with thousands separators (e.g. 17511 -> "17,511").
func comma(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

const styleBlock = `<style>
:root { color-scheme: light dark; }
body { font-family: system-ui, sans-serif; margin: 1.5rem; line-height: 1.4; }
h1 { margin: 0 0 0.25rem; font-size: 1.4rem; }
h2 { margin: 1.5rem 0 0.25rem; font-size: 1.15rem; }
.status { font-weight: normal; opacity: 0.75; font-size: 0.9rem; }
.subtle { opacity: 0.7; font-size: 0.9rem; margin: 0.1rem 0; }
.warn-banner { background: #ffe6e6; border: 1px solid #cc0000; border-radius: 4px; padding: 0.5rem 1rem; margin: 0.5rem 0 1rem; }
pre.mermaid { background: transparent; }
table { border-collapse: collapse; margin: 0.5rem 0 1rem; }
th, td { border: 1px solid rgba(128,128,128,0.35); padding: 0.25rem 0.6rem; text-align: left; }
td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
section { margin-bottom: 2rem; }
</style>
`
