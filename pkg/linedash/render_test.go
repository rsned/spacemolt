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
	b, err := RenderBoard([]LineReport{r}, nil, 60)
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
	b, err := RenderBoard([]LineReport{r}, nil, 0)
	if err != nil {
		t.Fatalf("RenderBoard: %v", err)
	}
	page := string(b)
	mustContain(t, page, `<a href="/line/moonshine">Pirate Moonshine</a>`)
	mustContain(t, page, "1234 /day produced")
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
	board, err := RenderBoard(nil, nil, 60)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="?refresh=1"`, `location.replace(location.pathname)`} {
		if !strings.Contains(string(board), want) {
			t.Errorf("board page missing %q", want)
		}
	}
}

func boardReports() []LineReport {
	return []LineReport{
		{ID: "v:moon", Name: "Pirate Moonshine", State: StateStarved, StationName: "Voss Redoubt Station", SystemName: "Alhena",
			StationCondition: "operational", StationPct: 81, Category: "Contraband", Kind: KindFacility, Purpose: PurposeForSale,
			Agents: 10, Why: "trader-10: buy_directed: price 70 exceeds ceiling 50 (replan)", ProducedPerDay: 2200},
		{ID: "c:steel", Name: "Steel Plate", State: StateOptimal, StationName: "Conjunction Junction", SystemName: "Cloverfield",
			StationCondition: "thriving", StationPct: 100, Category: "Refined", Kind: KindFacility, Purpose: PurposeFeeds,
			DependedOn: Deps{Production: 2, Upkeep: 1}, Agents: 2, ProducedPerDay: 208000},
		{ID: "c:sun", Name: "Sunspindle", State: StateStarved, StationName: "Conjunction Junction", SystemName: "Cloverfield",
			StationCondition: "thriving", StationPct: 100, Category: CategoryUpkeep, Kind: KindHand, Purpose: PurposeUpkeep, Agents: 1,
			Why: "liquid_nitrogen 0 (shared with 2 lines)"},
		{ID: "c:sell:void_dust", Name: "Void Dust", State: StateOptimal, StationName: "Conjunction Junction", SystemName: "Cloverfield",
			Category: CategorySales, Kind: KindSales, Purpose: PurposeForSale, Agents: 1, SoldPerDay: 12},
		{ID: "c:board", Name: "Circuit Board", State: StateOptimal, StationName: "Conjunction Junction", SystemName: "Cloverfield",
			Category: "Component", Kind: KindFacility, Purpose: PurposeFeeds, Agents: 1},
	}
}

func renderBoard(t *testing.T, warnings []string) string {
	t.Helper()
	b, err := RenderBoard(boardReports(), warnings, 60)
	if err != nil {
		t.Fatalf("RenderBoard: %v", err)
	}
	return string(b)
}

func before(t *testing.T, page, a, b string) {
	t.Helper()
	i, j := strings.Index(page, a), strings.Index(page, b)
	if i < 0 || j < 0 || i > j {
		t.Errorf("want %q before %q (at %d, %d)", a, b, i, j)
	}
}

func TestBoardGroupsByStationThenCategory(t *testing.T) {
	page := renderBoard(t, nil)
	before(t, page, "Conjunction Junction", "Voss Redoubt Station")
	mustContain(t, page, "Cloverfield")
	mustContain(t, page, "thriving 100%")
	before(t, page, ">Station upkeep<", ">Refined<")
	before(t, page, ">Refined<", ">Component<")
	before(t, page, ">Component<", ">Market/Sales<")
}

func TestBoardRowShowsKindTagsDepsAgentsWhy(t *testing.T) {
	page := renderBoard(t, nil)
	mustContain(t, page, `<a href="/line/c:steel">Steel Plate</a>`)
	mustContain(t, page, "feeds lines")
	mustContain(t, page, "2 production · 1 upkeep")
	mustContain(t, page, `<td class="num">10</td>`)
	mustContain(t, page, "price 70 exceeds ceiling 50")
	mustContain(t, page, "12 /day sold")
	mustContain(t, page, "for sale")
}

func TestBoardHandAndFacilityTagsDiffer(t *testing.T) {
	page := renderBoard(t, nil)
	mustContain(t, page, `class="tag tag-hand"`)
	mustContain(t, page, `class="tag tag-facility"`)
	mustContain(t, page, `class="tag tag-sales"`)
}

func TestBoardShowsWarnings(t *testing.T) {
	mustContain(t, renderBoard(t, []string{"override names unknown line x"}), "override names unknown line x")
}

func TestBoardHasLinesFleetSwitch(t *testing.T) {
	page := renderBoard(t, nil)
	mustContain(t, page, `class="tab on"`)
	mustContain(t, page, ">Fleet<")
}
