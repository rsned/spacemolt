package huddash

import (
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

func TestParsePeriod(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"hour", time.Hour, true},
		{"half_day", 12 * time.Hour, true},
		{"day", 24 * time.Hour, true},
		{"week", 0, false},
	} {
		p, err := ParsePeriod(tc.in)
		if tc.ok && (err != nil || p.Dur != tc.want) {
			t.Errorf("ParsePeriod(%q) = %v,%v; want dur %v", tc.in, p, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("ParsePeriod(%q) want error", tc.in)
		}
	}
}

func TestPhaseTicksClampsNegative(t *testing.T) {
	// claimed=100, arrived_src=130, bought=135, arrived_dst=0 (missing), sold=200.
	// dst segment (0-135) and sell segment (200-0) must not go negative/huge-positive wrongly:
	// dst clamps to 0; sell = 200-0 = 200 (still >=0).
	h := market.HaulResult{ClaimedTick: 100, ArrivedSrcTick: 130, BoughtTick: 135, ArrivedDstTick: 0, SoldTick: 200}
	got := phaseTicks(h)
	if got[0] != 30 || got[1] != 5 {
		t.Errorf("travel_src/buy = %v, want 30/5", got[:2])
	}
	if got[2] != 0 {
		t.Errorf("travel_dst = %v, want 0 (clamped)", got[2])
	}
}

func TestGroupHaulsByPeriod(t *testing.T) {
	p, _ := ParsePeriod("hour")
	hauls := []market.HaulResult{
		{SoldAt: "2026-06-27T10:05:00Z"},
		{SoldAt: "2026-06-27T10:55:00Z"}, // same hour bucket
		{SoldAt: "2026-06-27T12:01:00Z"}, // next-next hour
	}
	bk := groupHaulsByPeriod(hauls, p)
	if len(bk) != 2 {
		t.Fatalf("buckets = %d, want 2", len(bk))
	}
	if len(bk[0].Hauls) != 2 || len(bk[1].Hauls) != 1 {
		t.Fatalf("bucket sizes = %d,%d want 2,1", len(bk[0].Hauls), len(bk[1].Hauls))
	}
	if !bk[0].Start.Before(bk[1].Start) {
		t.Errorf("buckets not chronological")
	}
}

// TestRenderStructure is the golden-structure test: render from fixture rows and
// assert the document carries the expected per-hauler sections, SVG charts, and
// real realized profit (no live DB).
func TestRenderStructure(t *testing.T) {
	p, _ := ParsePeriod("hour")
	in := Input{
		GeneratedAt: time.Date(2026, 6, 27, 6, 0, 0, 0, time.UTC),
		Period:      p,
		Window:      48 * time.Hour,
		Agents: []AgentData{
			{
				AgentID: "salvager-1",
				HasStat: true,
				Status: balances.LiveRecord{
					AgentID: "salvager-1", Role: "hauler", System: "sol", POI: "sol_central",
					Credits: 12345, Fuel: 80, MaxFuel: 100, CargoUsed: 40, CargoCapacity: 200,
				},
				Hauls: []market.HaulResult{{
					AgentID: "salvager-1", ItemID: "iron_ore", Qty: 100,
					RealizedProfit: 5000, JumpsTraveled: 10, SoldAt: "2026-06-27T05:30:00Z",
					ClaimedTick: 1000, ArrivedSrcTick: 1020, BoughtTick: 1022,
					ArrivedDstTick: 1060, SoldTick: 1065,
				}},
				Series: []market.FleetSnapshot{
					{AgentID: "salvager-1", TS: "2026-06-27T04:00:00Z", Credits: 9000},
					{AgentID: "salvager-1", TS: "2026-06-27T05:00:00Z", Credits: 12345},
				},
			},
			{AgentID: "salvager-2", HasStat: false}, // no data -> empty charts
		},
	}
	out := Render(in)

	for _, want := range []string{
		`id="salvager-1"`, ">salvager-1<", `id="salvager-2"`,
		"<table class=\"summary\"", "credit balance", "hauls / hour",
		"credits / jump", "response ticks", "80/100", "40/200", "no data yet",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}
	// Four charts per agent × 2 agents = 8 <svg> elements.
	if n := strings.Count(out, "<svg"); n != 8 {
		t.Errorf("<svg> count = %d, want 8", n)
	}
	// Realized profit (5,000) must appear in salvager-1's summary row.
	if !strings.Contains(out, "5,000") {
		t.Errorf("summary missing realized profit 5,000")
	}
	// salvager-2 has no hauls -> 0 in its row.
	if !strings.Contains(out, `<a href="#salvager-2">`) {
		t.Errorf("missing salvager-2 summary link")
	}
}
