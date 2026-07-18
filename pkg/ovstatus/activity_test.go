package ovstatus

import (
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

func TestRenderActivityLine(t *testing.T) {
	var b strings.Builder
	renderActivityLine(&b, "Mission Steel Plate Order")
	out := b.String()
	for _, want := range []string{"Mission Steel Plate Order", `colspan="6"`, `class="subtle"`} {
		if !strings.Contains(out, want) {
			t.Errorf("activity line missing %q; got %s", want, out)
		}
	}
}

func TestRenderActivityLineEscaped(t *testing.T) {
	var b strings.Builder
	renderActivityLine(&b, `Rescuing <b>x</b>`)
	if strings.Contains(b.String(), "<b>") {
		t.Errorf("activity text not HTML-escaped: %s", b.String())
	}
}

// A worker with no activity gets no sub-row; one with activity gets exactly one,
// and a hauler shows the activity line ABOVE its lifetime line.
func TestRenderRowActivitySubline(t *testing.T) {
	now := time.Now()
	seen := now.Format(time.RFC3339)

	// No activity -> no eff-line sub-row from renderRow (no haul stats supplied).
	var idle strings.Builder
	renderRow(&idle, balances.LiveRecord{AgentID: "engineer-1", Seen: true, LastSeen: seen, Healthy: true}, nil, now)
	if strings.Contains(idle.String(), "eff-line") {
		t.Errorf("idle worker should have no sub-row; got %s", idle.String())
	}

	// With activity -> one activity sub-row.
	var active strings.Builder
	renderRow(&active, balances.LiveRecord{
		AgentID: "engineer-1", Seen: true, LastSeen: seen, Healthy: true,
		Activity: "Mission Steel Plate Order",
	}, nil, now)
	if got := strings.Count(active.String(), "eff-line"); got != 1 {
		t.Errorf("active worker want 1 sub-row, got %d: %s", got, active.String())
	}
	if !strings.Contains(active.String(), "Mission Steel Plate Order") {
		t.Errorf("activity text missing; got %s", active.String())
	}

	// Hauler with activity AND lifetime stats -> two sub-rows, activity first.
	var hauler strings.Builder
	hs := &HaulStats{Lifetime: map[string]AgentLifetime{"haul-3": {Hauls: 10, Jumps: 40, AvgPerJump: 2772}}}
	renderRow(&hauler, balances.LiveRecord{
		AgentID: "haul-3", Seen: true, LastSeen: seen, Healthy: true,
		Activity: "Opportunity #100042 24 power_cell from A to B",
	}, hs, now)
	out := hauler.String()
	if got := strings.Count(out, "eff-line"); got != 2 {
		t.Errorf("hauler want 2 sub-rows (activity + lifetime), got %d: %s", got, out)
	}
	if ai, li := strings.Index(out, "Opportunity #100042"), strings.Index(out, "hauls ·"); ai < 0 || li < 0 || ai > li {
		t.Errorf("activity line should precede lifetime line; activity@%d lifetime@%d: %s", ai, li, out)
	}
}
