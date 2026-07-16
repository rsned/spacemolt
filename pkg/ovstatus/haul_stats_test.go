package ovstatus

import (
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
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
	if strings.Contains(got, `colspan="6"`) {
		t.Fatal("nil HaulStats must not render per-worker lines")
	}
}

func TestRenderRowLifetimeGuard(t *testing.T) {
	now := time.Now()
	w := balances.LiveRecord{AgentID: "trader-1", Healthy: true, LastSeen: now.UTC().Format(time.RFC3339)}

	// nil hs -> only the worker's own row, no per-worker line.
	var b1 strings.Builder
	renderRow(&b1, w, nil, now)
	if strings.Contains(b1.String(), `colspan="6"`) {
		t.Errorf("nil hs must not emit a per-worker line; got %s", b1.String())
	}

	// hs containing this agent -> the per-worker line appears.
	var b2 strings.Builder
	hs := &HaulStats{Lifetime: map[string]AgentLifetime{"trader-1": {Hauls: 281, Jumps: 1405, AvgPerJump: 2391}}}
	renderRow(&b2, w, hs, now)
	if !strings.Contains(b2.String(), `colspan="6"`) || !strings.Contains(b2.String(), "281 hauls") {
		t.Errorf("hs with this agent must emit the per-worker line; got %s", b2.String())
	}

	// hs present but this agent absent from Lifetime -> no per-worker line.
	var b3 strings.Builder
	hsOther := &HaulStats{Lifetime: map[string]AgentLifetime{"someone-else": {Hauls: 1, Jumps: 1, AvgPerJump: 1}}}
	renderRow(&b3, w, hsOther, now)
	if strings.Contains(b3.String(), `colspan="6"`) {
		t.Errorf("agent absent from Lifetime must not emit a per-worker line; got %s", b3.String())
	}
}
