package linedash

import (
	"bytes"
	_ "embed" // page.gohtml
	"fmt"
	"html/template"
	"strings"
	"time"
)

//go:embed page.gohtml
var pageSrc string

var pageTmpl = template.Must(template.New("page").Parse(pageSrc))

// Node box drawing, in SVG pixels.
const (
	nodeRadius = 10 // rect rx
	barHeight  = 6  // state bar at the bottom of a box
	textInset  = 14 // left padding of node text
	dotRadius  = 5  // status dot in the header
)

// stateWords are the human names of the states.
var stateWords = map[State]string{
	StateOptimal: "Optimal",
	StateStarved: "Starved",
	StateChoked:  "Choked",
	StateUnknown: "Unknown",
}

func stateWord(s State) string {
	if w, ok := stateWords[s]; ok {
		return w
	}

	return "Unknown"
}

// stateClass is the CSS class of a state; anything unexpected is "unknown".
func stateClass(s State) string {
	if _, ok := stateWords[s]; ok {
		return string(s)
	}

	return string(StateUnknown)
}

// ago renders the age of t at now: "never" for the zero time, "now" under a
// minute (or in the future), else whole minutes, hours or days.
func ago(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

func fmtStamp(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

// boardRow is one line on the top board.
type boardRow struct {
	ID, Name, State, StateWord string
	Produced, Sold             int
	Alerts                     string
	Stale                      []string
}

type boardData struct {
	Refresh   int
	Generated string
	Rows      []boardRow
}

// RenderBoard renders the top board ("/"): one row per line.
func RenderBoard(reports []LineReport, refreshSec int) ([]byte, error) {
	d := boardData{Refresh: refreshSec, Generated: "never"}
	if len(reports) > 0 {
		d.Generated = fmtStamp(reports[0].GeneratedAt)
	}
	for _, r := range reports {
		alerts := "—"
		if len(r.Alerts) > 0 {
			parts := make([]string, len(r.Alerts))
			for i, a := range r.Alerts {
				parts[i] = string(a)
			}
			alerts = strings.Join(parts, " · ")
		}
		d.Rows = append(d.Rows, boardRow{
			ID: r.ID, Name: r.Name, State: stateClass(r.State), StateWord: stateWord(r.State),
			Produced: r.ProducedPerDay, Sold: r.SoldPerDay, Alerts: alerts, Stale: r.StaleSources,
		})
	}

	return execute("board", d)
}

// textView is one positioned text line inside a node box.
type textView struct {
	Text, Class string
	X, Y        int
}

type nodeView struct {
	State, Name      string
	X, Y, W, H, BarY int
	DotX, DotY       int
	NameX, NameY     int
	Lines            []textView
}

type edgeView struct {
	State, D       string
	LX, LY1, LY2   int
	Label, Sub     string // shown on two lines above the curve midpoint
	FullLabel      string // "<Label> · <agents> · <ago>", the hover title
	From, To, Kind string
}

type sliderView struct {
	Name           string
	Live, Assigned int
}

type lineData struct {
	Refresh                 int
	ID, Name, State, Word   string
	Generated               string
	Width, Height           int
	Nodes                   []nodeView
	Edges                   []edgeView
	Output, Eff, AlertCount int
	Alerts                  []Alert
	Sliders                 []sliderView
	Stale                   []string
	RX, BarH, DotR, NodeW   int
}

// RenderLine renders one line's SCADA view ("/line/<id>"). The report must
// already be laid out (Layout).
func RenderLine(r LineReport, refreshSec int) ([]byte, error) {
	return execute("line", lineView(r, refreshSec))
}

// lineView computes every position the template draws. The SVG viewBox
// (Width x Height) fits every node box plus the margin.
func lineView(r LineReport, refreshSec int) lineData {
	d := lineData{
		Refresh: refreshSec, ID: r.ID, Name: r.Name, State: stateClass(r.State), Word: stateWord(r.State),
		Generated: fmtStamp(r.GeneratedAt),
		Output:    r.ProducedPerDay, Eff: r.EffPct, AlertCount: len(r.Alerts), Alerts: r.Alerts,
		Stale: r.StaleSources,
		RX:    nodeRadius, BarH: barHeight, DotR: dotRadius, NodeW: nodeWidth,
	}
	type box struct{ x, y, h int }
	boxes := make(map[string]box, len(r.Nodes))
	for _, n := range r.Nodes {
		h := nodeHeight(n)
		boxes[n.ID] = box{n.X, n.Y, h}
		d.Width = max(d.Width, n.X+nodeWidth+layoutMargin)
		d.Height = max(d.Height, n.Y+h+layoutMargin)
		nv := nodeView{
			State: stateClass(n.State), Name: n.Name,
			X: n.X, Y: n.Y, W: nodeWidth, H: h, BarY: n.Y + h - barHeight,
			DotX: n.X + textInset + dotRadius, DotY: n.Y + nodeHeaderH/2 + 2,
			NameX: n.X + textInset + 2*dotRadius + 8, NameY: n.Y + nodeHeaderH/2 + 7,
		}
		for i, bl := range nodeBody(n) {
			nv.Lines = append(nv.Lines, textView{
				Text: bl.Text, Class: bl.Class,
				X: n.X + textInset, Y: n.Y + nodeHeaderH + nodeLineH*i + 12,
			})
		}
		d.Nodes = append(d.Nodes, nv)
		if n.Type == NodeMine || n.Type == NodePurchase {
			d.Sliders = append(d.Sliders, sliderView{Name: n.Name, Live: n.LiveAgents, Assigned: n.AssignedAgents})
		}
	}
	for _, e := range r.Edges {
		f, okF := boxes[e.From]
		t, okT := boxes[e.To]
		if !okF || !okT {
			continue
		}
		x1, y1 := f.x+nodeWidth, f.y+f.h/2
		x2, y2 := t.x, t.y+t.h/2
		mx, my := (x1+x2)/2, (y1+y2)/2
		var sub []string
		if len(e.Agents) > 0 {
			sub = append(sub, strings.Join(e.Agents, ", "))
		}
		if e.Kind != EdgeCraft {
			sub = append(sub, ago(r.GeneratedAt, e.LastMovedAt))
		}
		full := append([]string{e.Label}, sub...)
		d.Edges = append(d.Edges, edgeView{
			State: stateClass(e.State),
			D:     fmt.Sprintf("M %d %d Q %d %d %d %d T %d %d", x1, y1, mx, y1, mx, my, x2, y2),
			LX:    mx, LY1: my - 20, LY2: my - 7,
			Label: e.Label, Sub: strings.Join(sub, " · "), FullLabel: strings.Join(full, " · "),
			From: e.From, To: e.To, Kind: string(e.Kind),
		})
	}
	d.Width = max(d.Width, 2*layoutMargin)
	d.Height = max(d.Height, 2*layoutMargin)

	return d
}

func execute(name string, data any) ([]byte, error) {
	var b bytes.Buffer
	if err := pageTmpl.ExecuteTemplate(&b, name, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", name, err)
	}

	return b.Bytes(), nil
}
