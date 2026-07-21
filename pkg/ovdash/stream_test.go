package ovdash

import (
	"bufio"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func agent(fleet, id, sys string, credits float64) AgentState {
	return AgentState{Fleet: fleet, AgentID: id, SystemID: sys, Credits: credits, Healthy: true, Seen: true}
}

func TestDiffClassifiesMovesUpdatesJoinsLeaves(t *testing.T) {
	prev := &Snapshot{Agents: []AgentState{
		agent("haul", "h1", "sol", 100),
		agent("haul", "h2", "sol", 100),
		agent("mission", "m1", "nova_terra", 50),
	}}
	cur := &Snapshot{Agents: []AgentState{
		agent("haul", "h1", "nova_terra", 100),  // moved
		agent("haul", "h2", "sol", 250),         // vitals changed
		agent("mission", "m2", "sol", 10),       // joined
	}, StaleFleets: []string{"craft"}}

	d := Diff(prev, cur)
	if len(d.Moved) != 1 || d.Moved[0].Agent.AgentID != "h1" || d.Moved[0].FromSystemID != "sol" {
		t.Fatalf("moved wrong: %+v", d.Moved)
	}
	if len(d.Updated) != 1 || d.Updated[0].AgentID != "h2" {
		t.Fatalf("updated wrong: %+v", d.Updated)
	}
	if len(d.Joined) != 1 || d.Joined[0].AgentID != "m2" {
		t.Fatalf("joined wrong: %+v", d.Joined)
	}
	if len(d.Left) != 1 || d.Left[0] != "m1" {
		t.Fatalf("left wrong: %+v", d.Left)
	}
	if len(d.StaleFleets) != 1 || d.StaleFleets[0] != "craft" {
		t.Fatalf("stale fleets must pass through: %+v", d.StaleFleets)
	}
}

func TestDiffNilPrevIsAllJoins(t *testing.T) {
	cur := &Snapshot{Agents: []AgentState{agent("haul", "h1", "sol", 1)}}
	d := Diff(nil, cur)
	if len(d.Joined) != 1 || len(d.Moved)+len(d.Updated)+len(d.Left) != 0 {
		t.Fatalf("nil prev must be all joins: %+v", d)
	}
}

func TestDiffUnchangedAgentEmitsNothing(t *testing.T) {
	a := agent("haul", "h1", "sol", 100)
	d := Diff(&Snapshot{Agents: []AgentState{a}}, &Snapshot{Agents: []AgentState{a}})
	if len(d.Moved)+len(d.Updated)+len(d.Joined)+len(d.Left) != 0 {
		t.Fatalf("identical snapshots must be an empty delta: %+v", d)
	}
}

func TestHubSendsKeyframeOnConnectAndRelaysBroadcasts(t *testing.T) {
	h := NewHub()
	h.SetKeyframe("snapshot", map[string]int{"n": 1})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/overmind/stream", nil)
	done := make(chan struct{})
	go func() { h.ServeHTTP(rec, req); close(done) }()

	// Give the handler a beat to register, then broadcast and close.
	time.Sleep(50 * time.Millisecond)
	h.Broadcast("delta", map[string]int{"n": 2})
	time.Sleep(50 * time.Millisecond)
	h.CloseAll()
	<-done

	body := rec.Body.String()
	r := bufio.NewScanner(strings.NewReader(body))
	var events []string
	for r.Scan() {
		if strings.HasPrefix(r.Text(), "event: ") {
			events = append(events, strings.TrimPrefix(r.Text(), "event: "))
		}
	}
	if len(events) < 2 || events[0] != "snapshot" || events[1] != "delta" {
		t.Fatalf("want keyframe then delta, got %v in %q", events, body)
	}
	if !strings.Contains(body, `data: {"n":1}`) || !strings.Contains(body, `data: {"n":2}`) {
		t.Fatalf("payloads missing: %q", body)
	}
}
