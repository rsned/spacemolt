package worker

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func (f *fakeClient) GetSystemAgents(ctx context.Context) error {
	f.calls = append(f.calls, "get_system_agents")
	return nil
}

func waypointState(policeLevel int) *game.State {
	st := &game.State{}
	st.System.ID = "nashira"
	st.System.Name = "Nashira"
	st.System.PoliceLevel = policeLevel
	return st
}

// waypointClient is a fake parked at a belt in the given security band, with
// enough of a get_location reply for the POI capture to succeed.
func waypointClient(policeLevel int) *fakeClient {
	return &fakeClient{
		state: waypointState(policeLevel),
		raw: map[string][]byte{"location": []byte(
			`{"location":{"poi_id":"nashira_belt","poi_name":"Nashira Belt","poi_type":"asteroid_belt","system_id":"nashira"}}`)},
	}
}

// In lawless space every hop asks who else is around, so a hunter's movements
// can be rebuilt after the fact. Server v0.618.0 made get_system_agents return
// an empty list (and promised its removal), so the hop must ask get_nearby --
// the only call that still reports anyone -- and must not spend budget on the
// dead one.
func TestKBWaypointCaptureAsksForNearbyInLawlessSpace(t *testing.T) {
	client := waypointClient(0)
	if err := KBWaypointCapture(context.Background(), client, knowledge.NewMemoryKB()); err != nil {
		t.Fatalf("KBWaypointCapture: %v", err)
	}
	if !slices.Contains(client.calls, "get_nearby") {
		t.Errorf("calls = %v, want get_nearby", client.calls)
	}
	if slices.Contains(client.calls, "get_system_agents") {
		t.Errorf("calls = %v, must not include the retired get_system_agents", client.calls)
	}
	// The system and POI captures every hop already did must survive.
	if !slices.Contains(client.calls, "get_system") || !slices.Contains(client.calls, "get_poi") {
		t.Errorf("calls = %v, want get_system and get_poi too", client.calls)
	}
}

// Policed space is not where hunters work, and every extra call comes out of
// the same rate-limit budget whose exhaustion got ships killed.
func TestKBWaypointCaptureSkipsSightingCallInPolicedSpace(t *testing.T) {
	client := waypointClient(3)
	if err := KBWaypointCapture(context.Background(), client, knowledge.NewMemoryKB()); err != nil {
		t.Fatalf("KBWaypointCapture: %v", err)
	}
	for _, dead := range []string{"get_system_agents", "get_nearby"} {
		if slices.Contains(client.calls, dead) {
			t.Errorf("calls = %v, must not include %s", client.calls, dead)
		}
	}
}

// No KB means nothing to record into; the hop must not spend any calls.
func TestKBWaypointCaptureNoKBIsANoop(t *testing.T) {
	client := &fakeClient{state: waypointState(0)}
	if err := KBWaypointCapture(context.Background(), client, nil); err != nil {
		t.Fatalf("KBWaypointCapture: %v", err)
	}
	if len(client.calls) != 0 {
		t.Errorf("calls = %v, want none", client.calls)
	}
}

func (f *fakeClient) GetNearby(ctx context.Context) error {
	f.calls = append(f.calls, "get_nearby")
	return nil
}

// Residents schedule both names; the dispatcher must know them BOTH -- but it
// must answer only one of them with a call.
//
// get_system_agents used to be aliased to get_nearby so a stale seed still
// returned data. That was wrong: every role scheduling it also scheduled
// get_nearby at the SAME ten_minutely cadence, so the alias made 65 agents
// fetch one set of data twice per cycle. It is now a recognised no-op, which
// keeps a stale seed from erroring forever without paying for it.
func TestDispatchRunsSightingCommandsByName(t *testing.T) {
	d0 := NewWorkerDispatch(&fakeClient{state: waypointState(3)}, knowledge.NewMemoryKB(), nil, io.Discard)
	for _, cmd := range []string{"get_system_agents", "get_nearby"} {
		if !d0.Supports(cmd) {
			t.Errorf("Supports(%s) = false; residents schedule it by name", cmd)
		}
	}

	// get_nearby is the live call.
	fc := &fakeClient{state: waypointState(3)}
	d := NewWorkerDispatch(fc, knowledge.NewMemoryKB(), nil, io.Discard)
	if err := d.Run(context.Background(), []string{"get_nearby"}); err != nil {
		t.Fatalf("Run(get_nearby): %v", err)
	}
	if !slices.Contains(fc.calls, "get_nearby") {
		t.Errorf("Run(get_nearby) issued %v, want get_nearby", fc.calls)
	}

	// The deprecated name spends nothing and still must not error.
	fd := &fakeClient{state: waypointState(3)}
	dd := NewWorkerDispatch(fd, knowledge.NewMemoryKB(), nil, io.Discard)
	if err := dd.Run(context.Background(), []string{"get_system_agents"}); err != nil {
		t.Fatalf("Run(get_system_agents) must not error, a stale seed fires forever: %v", err)
	}
	if len(fd.calls) != 0 {
		t.Errorf("Run(get_system_agents) issued %v, want no call at all", fd.calls)
	}
}

// Schedule seeding is additive only: a command already written into an agent's
// schedule.json fires forever, so deleting get_system_agents from the role
// config cannot stop the traffic. The dispatcher is the only chokepoint, and it
// must answer the retired name with the live call instead of forwarding a
// request the server now answers with an empty list.
func TestDispatchNeverForwardsRetiredSystemAgents(t *testing.T) {
	fc := &fakeClient{state: waypointState(3)}
	d := NewWorkerDispatch(fc, knowledge.NewMemoryKB(), nil, io.Discard)
	if err := d.Run(context.Background(), []string{"get_system_agents"}); err != nil {
		t.Fatalf("Run(get_system_agents): %v", err)
	}
	if slices.Contains(fc.calls, "get_system_agents") {
		t.Errorf("calls = %v, must not forward the retired get_system_agents", fc.calls)
	}
}
