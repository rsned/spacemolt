package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// QuiesceFlag is the on-disk park request for one agent.
//
// It lives in a file rather than on the control channel for one reason: it has
// to survive a restart. The existing drain gate is in-memory, so a fleet cycle
// — which happens routinely here — puts a drained worker straight back to work.
// An operator parking an agent to take it away for other duty needs the park to
// still be there afterwards.
type QuiesceFlag struct {
	Quiesce bool   `json:"quiesce"`
	Reason  string `json:"reason,omitempty"`
	SetAt   string `json:"set_at,omitempty"` // RFC3339, operator bookkeeping only
}

// QuiesceFile is where an agent's park request lives, beside the schedule and
// checkpoint files it already owns.
func QuiesceFile(agentID string) string {
	return filepath.Join("data", "agents", agentID, "quiesce.json")
}

// ReadQuiesce reports whether the agent is flagged to park at its next safe
// point, and the operator's reason if one was given.
//
// FAILS OPEN, deliberately: a missing, unreadable, or malformed file reads as
// "not quiesced". This file is meant to be hand-edited, and the failure mode of
// failing closed — a stray character silently parking workers — is far worse
// than the failure mode of failing open, which is that a park does not take and
// the operator sees the agent still working.
func ReadQuiesce(path string) (bool, string) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is built from the agent id we were launched with
	if err != nil {
		return false, ""
	}
	var f QuiesceFlag
	if err := json.Unmarshal(raw, &f); err != nil {
		return false, ""
	}
	if !f.Quiesce {
		return false, ""
	}
	return true, f.Reason
}
