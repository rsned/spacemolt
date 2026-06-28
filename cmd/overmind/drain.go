package main

import (
	"errors"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// errSendFail is used by tests to simulate a failed control send.
var errSendFail = errors.New("send failed")

// controlSender is the subset of *supervisor.Server the drain helpers need.
type controlSender interface {
	Send(agentID string, env control.Envelope) error
}

// broadcast sends a control message of type t (with payload) to every worker,
// returning the number successfully delivered. A failed send is logged and
// skipped (a worker may have exited) — never fatal to the fan-out.
func broadcast(s controlSender, workers []supervisor.WorkerInfo, t control.Type, payload any, log func(string)) int {
	sent := 0
	for _, w := range workers {
		env, err := control.NewEnvelope(t, w.AgentID, payload)
		if err != nil {
			log("build " + string(t) + " envelope for " + w.AgentID + ": " + err.Error())
			continue
		}
		if err := s.Send(w.AgentID, env); err != nil {
			log("send " + string(t) + " to " + w.AgentID + ": " + err.Error())
			continue
		}
		sent++
	}
	return sent
}

// drainComplete reports whether the fleet has reached drain quiescence: no
// healthy workers, or all healthy workers idle.
func drainComplete(idle, total int) bool {
	return total == 0 || idle >= total
}
