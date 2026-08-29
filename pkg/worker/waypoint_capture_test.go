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

// In lawless space every hop asks who else is in the system, so a hunter's
// movements can be rebuilt after the fact.
func TestKBWaypointCaptureAsksForSystemAgentsInLawlessSpace(t *testing.T) {
	client := waypointClient(0)
	if err := KBWaypointCapture(context.Background(), client, knowledge.NewMemoryKB()); err != nil {
		t.Fatalf("KBWaypointCapture: %v", err)
	}
	if !slices.Contains(client.calls, "get_system_agents") {
		t.Errorf("calls = %v, want get_system_agents", client.calls)
	}
	// The system and POI captures every hop already did must survive.
	if !slices.Contains(client.calls, "get_system") || !slices.Contains(client.calls, "get_poi") {
		t.Errorf("calls = %v, want get_system and get_poi too", client.calls)
	}
}

// Policed space is not where hunters work, and every extra call comes out of
// the same rate-limit budget whose exhaustion got ships killed.
func TestKBWaypointCaptureSkipsSystemAgentsInPolicedSpace(t *testing.T) {
	client := waypointClient(3)
	if err := KBWaypointCapture(context.Background(), client, knowledge.NewMemoryKB()); err != nil {
		t.Fatalf("KBWaypointCapture: %v", err)
	}
	if slices.Contains(client.calls, "get_system_agents") {
		t.Errorf("calls = %v, must not include get_system_agents", client.calls)
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

// Residents schedule these two by name; the dispatcher must know them.
func TestDispatchRunsSightingCommandsByName(t *testing.T) {
	for _, cmd := range []string{"get_system_agents", "get_nearby"} {
		fc := &fakeClient{state: waypointState(3)}
		d := NewWorkerDispatch(fc, knowledge.NewMemoryKB(), nil, io.Discard)
		if err := d.Run(context.Background(), []string{cmd}); err != nil {
			t.Fatalf("Run(%s): %v", cmd, err)
		}
		if !slices.Contains(fc.calls, cmd) {
			t.Errorf("Run(%s) issued %v", cmd, fc.calls)
		}
	}
}
