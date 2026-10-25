package linedash

import (
	"strings"
	"testing"
	"time"
)

// moonshineReport evaluates the fixture line with the four miners offline and
// lays it out.
func moonshineReport(t *testing.T) LineReport {
	t.Helper()
	line := fixtureLine(t)
	in := baseInputs(line)
	for _, a := range []string{"miningbot_0000", "miningbot_0001", "miningbot_0002", "miningbot_0003"} {
		delete(in.Live, a)
	}
	r := Evaluate(line, in)
	Layout(&r)

	return r
}

func mustContain(t *testing.T, page, want string) {
	t.Helper()
	if !strings.Contains(page, want) {
		t.Errorf("page does not contain %q", want)
	}
}

func TestRenderLineContainsNodesEdgesStatsAndSliders(t *testing.T) {
	r := moonshineReport(t)
	for i := range r.Edges {
		if r.Edges[i].From == "ice_mine" {
			r.Edges[i].LastMovedAt = r.GeneratedAt.Add(-3 * time.Hour)
		}
	}
	for i := range r.Nodes {
		if r.Nodes[i].ID == "voss_output" {
			r.Nodes[i].Name = `<script>alert(1)</script>`
		}
	}
	b, err := RenderLine(r, 60)
	if err != nil {
		t.Fatalf("RenderLine: %v", err)
	}
	page := string(b)
	for _, want := range []string{
		"<title>Line Dashboard</title>",
		"Backwater Still", "Ice Mine", "Treasure Cache Shop",
		"haul consigned",
		"haul consigned · trader-9 · 3h",
		"Output Rate", "Avg Factory Eff", "Active Alerts",
		"Ice Mine 0/4 bots", "Culture Purchase 1/1 bots",
		"Optimal", "Starved", "Choked / Backed up", "Unknown / stale",
		`id="pause"`,
		"<svg", `<g class="node starved"`,
		`type="range" disabled`,
		"miningbot_0000",
		`data-refresh="60"`,
		"&lt;script&gt;alert(1)&lt;/script&gt;",
	} {
		mustContain(t, page, want)
	}
	if strings.Contains(page, "<script>alert(1)") {
		t.Error("node name was not escaped")
	}
	if n := strings.Count(page, `<g class="node `); n != len(r.Nodes) {
		t.Errorf("node groups = %d, want %d", n, len(r.Nodes))
	}
	if n := strings.Count(page, `class="edge `); n != len(r.Edges) {
		t.Errorf("edge paths = %d, want %d", n, len(r.Edges))
	}
	if n := strings.Count(page, `type="range" disabled`); n != 2 {
		t.Errorf("sliders = %d, want 2 (one mine, one purchase)", n)
	}
	// Offline miners are greyed.
	mustContain(t, page, `class="agent off"`)
}

func TestRenderLineViewBoxFitsAllNodes(t *testing.T) {
	r := moonshineReport(t)
	v := lineView(r, 60)
	for _, n := range r.Nodes {
		if n.X+nodeWidth > v.Width || n.Y+nodeHeight(n) > v.Height {
			t.Errorf("%s at %d,%d (h %d) outside viewBox %dx%d", n.ID, n.X, n.Y, nodeHeight(n), v.Width, v.Height)
		}
	}
}

func TestRenderBoardWithMissingSources(t *testing.T) {
	r := LineReport{ID: "moonshine", Name: "Pirate Moonshine", State: StateUnknown, StaleSources: []string{"assets.db"}}
	b, err := RenderBoard([]LineReport{r}, 60)
	if err != nil {
		t.Fatalf("RenderBoard: %v", err)
	}
	page := string(b)
	mustContain(t, page, "Pirate Moonshine")
	mustContain(t, page, "data stale")
	mustContain(t, page, "assets.db")
	mustContain(t, page, "—") // no alerts
}

func TestRenderBoardRowLinksToLine(t *testing.T) {
	r := LineReport{
		ID: "moonshine", Name: "Pirate Moonshine", State: StateStarved,
		ProducedPerDay: 1234, SoldPerDay: 56,
		Alerts: []Alert{AlertNoBotsLive, AlertHaulerStarved},
	}
	b, err := RenderBoard([]LineReport{r}, 0)
	if err != nil {
		t.Fatalf("RenderBoard: %v", err)
	}
	page := string(b)
	mustContain(t, page, `<a href="/line/moonshine">Pirate Moonshine</a>`)
	mustContain(t, page, "1234 /day produced")
	mustContain(t, page, "56 /day sold")
	mustContain(t, page, "no bots on node · hauler starved")
	mustContain(t, page, `class="dot starved"`)
	if strings.Contains(page, "data stale") {
		t.Error("fresh line shows a data stale badge")
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		t    time.Time
		want string
	}{
		{time.Time{}, "never"},
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-12 * time.Minute), "12m"},
		{now.Add(-3*time.Hour - 20*time.Minute), "3h"},
		{now.Add(-50 * time.Hour), "2d"},
		{now.Add(time.Minute), "now"},
	} {
		if got := ago(now, tc.t); got != tc.want {
			t.Errorf("ago(%v) = %q, want %q", now.Sub(tc.t), got, tc.want)
		}
	}
}

// TestRenderHasManualRefreshLink pins the operator's manual refresh: both
// pages carry a Refresh link to ?refresh=1, and the auto-reload drops the
// query so a forced rebuild is not repeated on every tick.
func TestRenderHasManualRefreshLink(t *testing.T) {
	board, err := RenderBoard(nil, 60)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="?refresh=1"`, `location.replace(location.pathname)`} {
		if !strings.Contains(string(board), want) {
			t.Errorf("board page missing %q", want)
		}
	}
}
