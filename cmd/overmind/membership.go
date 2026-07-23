package main

import (
	"log"
	"reflect"
	"sync"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// rosterState holds the latest yaml + overrides view shared between the
// SIGHUP handler, the admin hook, and the status writer.
type rosterState struct {
	mu        sync.Mutex
	yamlSpecs []supervisor.WorkerSpec
	overrides supervisor.Overrides
}

// reload re-reads the fleet yaml and overrides sidecar and returns the
// effective roster. ok=false means the yaml failed to parse — the caller MUST
// keep the current roster (loud log already emitted). A corrupt overrides
// file degrades to empty with a loud log (spec: never block a fleet on a bad
// sidecar).
func (rs *rosterState) reload(fleetPath, overridesPath string, logger *log.Logger) ([]supervisor.WorkerSpec, bool) {
	specs, err := supervisor.LoadFleet(fleetPath)
	if err != nil {
		logger.Printf("SIGHUP: FLEET YAML UNREADABLE (%v) — keeping current roster", err)
		return nil, false
	}
	ov, err := supervisor.LoadOverrides(overridesPath)
	if err != nil {
		logger.Printf("SIGHUP: overrides unreadable (%v) — treating as empty", err)
		ov = supervisor.Overrides{}
	}
	rs.mu.Lock()
	rs.yamlSpecs = specs
	rs.overrides = ov
	rs.mu.Unlock()
	return supervisor.SubtractOverrides(specs, ov), true
}

// removedList returns a copy of the current override-removed ids.
func (rs *rosterState) removedList() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]string, len(rs.overrides.Removed))
	copy(out, rs.overrides.Removed)
	return out
}

// yamlSpec looks up an agent's spec in the latest yaml copy.
func (rs *rosterState) yamlSpec(agentID string) (supervisor.WorkerSpec, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, sp := range rs.yamlSpecs {
		if sp.AgentID == agentID {
			return sp, true
		}
	}
	return supervisor.WorkerSpec{}, false
}

// diffSpecs computes the membership requests that turn current into desired.
func diffSpecs(current, desired []supervisor.WorkerSpec) []supervisor.MembershipRequest {
	curBy := make(map[string]supervisor.WorkerSpec, len(current))
	for _, sp := range current {
		curBy[sp.AgentID] = sp
	}
	var reqs []supervisor.MembershipRequest
	seen := make(map[string]bool, len(desired))
	for _, want := range desired {
		seen[want.AgentID] = true
		have, ok := curBy[want.AgentID]
		switch {
		case !ok:
			reqs = append(reqs, supervisor.MembershipRequest{Op: supervisor.MembershipAdd, Spec: want})
		case !reflect.DeepEqual(have, want):
			reqs = append(reqs, supervisor.MembershipRequest{Op: supervisor.MembershipUpdate, Spec: want})
		}
	}
	for _, have := range current {
		if !seen[have.AgentID] {
			reqs = append(reqs, supervisor.MembershipRequest{Op: supervisor.MembershipRemove, Spec: supervisor.WorkerSpec{AgentID: have.AgentID}})
		}
	}
	return reqs
}

// safeDiff computes the SIGHUP membership requests but refuses a wholesale
// wipe. An effective roster that is empty — or that would remove more than
// half the live fleet — is far more likely a truncated / fat-fingered / caught
// mid-write yaml than a real intent, and the spec's guarantee is "never drop a
// fleet on a bad edit". In that case it returns (nil, false) so the SIGHUP
// caller keeps the current roster (a loud log is emitted here).
//
// This guard is SIGHUP-only: an empty live roster imposes no guard, so an
// operator legitimately starting or growing an empty fleet is unaffected
// (boot loads specs directly, never through this path). Legitimate large
// scale-downs remain possible via the dashboard per-agent removes or by
// staging the yaml edit across more than one SIGHUP.
func safeDiff(live, effective []supervisor.WorkerSpec, logger *log.Logger) ([]supervisor.MembershipRequest, bool) {
	reqs := diffSpecs(live, effective)
	if len(live) == 0 {
		return reqs, true // nothing running to protect
	}
	if len(effective) == 0 {
		logger.Printf("SIGHUP: REFUSING reload — effective roster is EMPTY while %d worker(s) are live (truncated/empty yaml?); keeping current roster", len(live))
		return nil, false
	}
	removes := 0
	for _, r := range reqs {
		if r.Op == supervisor.MembershipRemove {
			removes++
		}
	}
	if removes*2 > len(live) {
		logger.Printf("SIGHUP: REFUSING reload — would remove %d of %d live worker(s) (>half; likely a bad edit); keeping current roster", removes, len(live))
		return nil, false
	}
	return reqs, true
}

// makeAdminHook builds the server admin callback: remove records the override
// is NOT done here (the dashboard owns the sidecar); this hook only enqueues
// the live change and acks. readd resolves the spec from the latest yaml copy.
func makeAdminHook(rs *rosterState, sup *supervisor.Supervisor, overridesPath string, logger *log.Logger) func(control.Type, string) control.AdminAck {
	return func(op control.Type, agentID string) control.AdminAck {
		switch op {
		case control.TypeAdminRemove:
			if _, ok := rs.yamlSpec(agentID); !ok {
				// Not in yaml — still allow removing a live roster member
				// (e.g. added earlier via a now-edited yaml) if present.
				found := false
				for _, sp := range sup.Roster() {
					if sp.AgentID == agentID {
						found = true
						break
					}
				}
				if !found {
					return control.AdminAck{AgentID: agentID, Status: control.AckUnknownAgent}
				}
			}
			// Track locally so the status writer's removed list is fresh even
			// before the dashboard's sidecar write is re-read.
			rs.mu.Lock()
			rs.overrides.Add(agentID)
			rs.mu.Unlock()
			sup.EnqueueMembership(supervisor.MembershipRequest{Op: supervisor.MembershipRemove, Spec: supervisor.WorkerSpec{AgentID: agentID}})
			logger.Printf("admin: remove %q enqueued", agentID)
			return control.AdminAck{AgentID: agentID, Status: control.AckAccepted}
		case control.TypeAdminReadd:
			spec, ok := rs.yamlSpec(agentID)
			if !ok {
				return control.AdminAck{AgentID: agentID, Status: control.AckUnknownAgent, Detail: "agent not in fleet yaml"}
			}
			rs.mu.Lock()
			rs.overrides.Delete(agentID)
			rs.mu.Unlock()
			sup.EnqueueMembership(supervisor.MembershipRequest{Op: supervisor.MembershipAdd, Spec: spec})
			logger.Printf("admin: readd %q enqueued", agentID)
			return control.AdminAck{AgentID: agentID, Status: control.AckAccepted}
		default:
			return control.AdminAck{AgentID: agentID, Status: control.AckUnknownAgent, Detail: "unsupported op"}
		}
	}
}
