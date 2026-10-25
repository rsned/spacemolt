package linedash

import (
	"fmt"
	"slices"
)

// Layout grid, in SVG pixels.
const (
	layoutMargin  = 40  // outer margin of the canvas
	layoutGutter  = 60  // gap between rows
	colGutter     = 120 // gap between columns: wide enough for a two-line edge label
	nodeWidth     = 260 // every node box is this wide
	rowMinPitch   = 140 // a row is at least this tall before the gutter
	nodeMinHeight = 100 // a node box is at least this tall
	nodeHeaderH   = 28  // status dot + name band at the top of a box
	nodeLineH     = 16  // one body text line
	nodeFooterH   = 18  // padding above and including the 6 px state bar
)

// Line classes of a node's body text.
const (
	lineTank   = "tank"
	lineAgent  = "agent"
	lineAgentX = "agent off" // greyed: not live, removed or seconded away
	lineDetail = "detail"
	lineAlert  = "alert"
)

// bodyLine is one text line shown under a node's name.
type bodyLine struct {
	Text, Class string
}

// nodeBody lists the node's text lines in display order: one per tank item
// ("<item> <units>"), then one per attached agent, then detail lines, then
// the node's alerts. Tank items follow their first appearance in Tanks, then
// any remaining Units keys sorted.
func nodeBody(n NodeReport) []bodyLine {
	var out []bodyLine
	var items []string
	for _, t := range n.Tanks {
		if _, ok := n.Units[t.Item]; ok && !slices.Contains(items, t.Item) {
			items = append(items, t.Item)
		}
	}
	var rest []string
	for it := range n.Units {
		if !slices.Contains(items, it) {
			rest = append(rest, it)
		}
	}
	slices.Sort(rest)
	for _, it := range append(items, rest...) {
		out = append(out, bodyLine{Text: fmt.Sprintf("%s %.0f", it, n.Units[it]), Class: lineTank})
	}
	for _, a := range n.Agents {
		cls := lineAgent
		if !a.Live || a.Removed || a.AwayFleet != "" {
			cls = lineAgentX
		}
		out = append(out, bodyLine{Text: a.AgentID, Class: cls})
	}
	for _, d := range n.Detail {
		out = append(out, bodyLine{Text: d, Class: lineDetail})
	}
	for _, a := range n.Alerts {
		out = append(out, bodyLine{Text: string(a), Class: lineAlert})
	}

	return out
}

// nodeHeight is the box height that fits the node's text.
func nodeHeight(n NodeReport) int {
	return max(nodeMinHeight, nodeHeaderH+nodeLineH*len(nodeBody(n))+nodeFooterH)
}

// Layout assigns X,Y to every node. Column = longest path from any source
// node (no incoming edge), edges taken as directed; row = order of
// declaration within the column. Columns are nodeWidth+colGutter apart;
// each row is as tall as its tallest box (at least rowMinPitch) plus the
// gutter. Deterministic for a given report. Cycles cannot loop forever: the
// relaxation runs at most len(Nodes) rounds.
func Layout(r *LineReport) {
	index := make(map[string]int, len(r.Nodes))
	for i, n := range r.Nodes {
		index[n.ID] = i
	}
	col := make([]int, len(r.Nodes))
	for range len(r.Nodes) {
		changed := false
		for _, e := range r.Edges {
			f, okF := index[e.From]
			t, okT := index[e.To]
			if okF && okT && col[t] < col[f]+1 && col[f]+1 < len(r.Nodes) {
				col[t] = col[f] + 1
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	row := make([]int, len(r.Nodes))
	perCol := map[int]int{}
	rows := 0
	for i := range r.Nodes {
		row[i] = perCol[col[i]]
		perCol[col[i]]++
		rows = max(rows, row[i]+1)
	}
	pitch := make([]int, rows)
	for i, n := range r.Nodes {
		pitch[row[i]] = max(pitch[row[i]], rowMinPitch, nodeHeight(n))
	}
	rowY := make([]int, rows)
	y := layoutMargin
	for i := range rows {
		rowY[i] = y
		y += pitch[i] + layoutGutter
	}
	for i := range r.Nodes {
		r.Nodes[i].X = layoutMargin + col[i]*(nodeWidth+colGutter)
		r.Nodes[i].Y = rowY[row[i]]
	}
}
