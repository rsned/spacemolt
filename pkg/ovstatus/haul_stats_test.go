package ovstatus

import (
	"strings"
	"testing"
	"time"
)

func TestPerJumpMetrics(t *testing.T) {
	gross, fuel, net := PerJumpMetrics(24000, 10, 9, 5) // gross 2400, fuel 45, net 2355
	if gross != 2400 || fuel != 45 || net != 2355 {
		t.Fatalf("got gross %v fuel %v net %v, want 2400/45/2355", gross, fuel, net)
	}
	g, f, n := PerJumpMetrics(0, 0, 9, 5)
	if g != 0 || f != 0 || n != 0 {
		t.Fatalf("zero jumps: got %v/%v/%v, want 0/0/0 (no divide)", g, f, n)
	}
}

func TestRenderLifetimeLine(t *testing.T) {
	var b strings.Builder
	renderLifetimeLine(&b, AgentLifetime{Hauls: 281, Jumps: 1405, AvgPerJump: 2391})
	out := b.String()
	for _, want := range []string{"281 hauls", "1,405 jumps", "— losses", "avg 2,391 cr/jump", `colspan="6"`} {
		if !strings.Contains(out, want) {
			t.Errorf("line missing %q; got %s", want, out)
		}
	}
}

func TestRenderEffPanel(t *testing.T) {
	var b strings.Builder
	renderEffPanel(&b, &EffPanel{
		WindowLabel: "48h", Hauls: 1204, GrossPerJump: 2391, FuelPerJump: 45, NetPerJump: 2346,
		Agents: []PanelAgent{{AgentID: "salvager-10", Hauls: 178, NetPerJump: 4050}},
	})
	out := b.String()
	for _, want := range []string{"Haul fleet efficiency", "48h", "NET 2,346 cr/jump", "1,204 hauls", "salvager-10 178h 4,050"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q; got %s", want, out)
		}
	}
}

func TestRenderEffPanelEmpty(t *testing.T) {
	var b strings.Builder
	renderEffPanel(&b, &EffPanel{WindowLabel: "48h", Hauls: 0})
	if !strings.Contains(b.String(), "No hauls in the last 48h") {
		t.Fatalf("empty panel should say no hauls; got %s", b.String())
	}
}

func TestRenderNilHaulStatsUnchanged(t *testing.T) {
	got := Render(nil, nil, 300, time.Now())
	if strings.Contains(got, "Haul fleet efficiency") {
		t.Fatal("nil HaulStats must not render the efficiency panel")
	}
	if strings.Contains(got, "effline") {
		t.Fatal("nil HaulStats must not render per-worker lines")
	}
}
