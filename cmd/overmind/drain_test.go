package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

type fakeSender struct {
	sent []string // agent ids
	fail map[string]bool
}

func (f *fakeSender) Send(agentID string, _ control.Envelope) error {
	if f.fail[agentID] {
		return errSendFail
	}
	f.sent = append(f.sent, agentID)
	return nil
}

func TestBroadcastFansOutToAllWorkers(t *testing.T) {
	s := &fakeSender{}
	workers := []supervisor.WorkerInfo{{AgentID: "a"}, {AgentID: "b"}, {AgentID: "c"}}
	n := broadcast(s, workers, control.TypeDrain, nil, func(string) {})
	if n != 3 {
		t.Fatalf("sent count = %d, want 3", n)
	}
	if len(s.sent) != 3 {
		t.Fatalf("delivered = %v, want 3", s.sent)
	}
}

func TestBroadcastCountsOnlySuccesses(t *testing.T) {
	s := &fakeSender{fail: map[string]bool{"b": true}}
	workers := []supervisor.WorkerInfo{{AgentID: "a"}, {AgentID: "b"}}
	n := broadcast(s, workers, control.TypeResume, nil, func(string) {})
	if n != 1 {
		t.Fatalf("sent count = %d, want 1 (b failed)", n)
	}
}

func TestDrainComplete(t *testing.T) {
	cases := []struct {
		idle, total int
		want        bool
	}{
		{0, 0, true},  // no workers -> trivially drained
		{3, 3, true},  // all idle
		{2, 3, false}, // one busy
		{0, 1, false},
	}
	for _, c := range cases {
		if got := drainComplete(c.idle, c.total); got != c.want {
			t.Fatalf("drainComplete(%d,%d) = %v, want %v", c.idle, c.total, got, c.want)
		}
	}
}
