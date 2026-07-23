package ovdash

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// AdminResult is the dashboard-facing outcome of a membership action.
type AdminResult struct {
	Status string `json:"status"` // accepted | unknown_agent | already_pending | recorded_offline
	Detail string `json:"detail,omitempty"`
}

// fleetByLabel finds a fleet registry entry by its UI label.
func fleetByLabel(label string) (FleetDef, bool) {
	for _, f := range Fleets {
		if f.Label == label {
			return f, true
		}
	}
	return FleetDef{}, false
}

// AdminRemove records agentID in the fleet's overrides sidecar, then asks the
// live overmind to remove it. A dead socket is the documented degraded mode:
// the override alone guarantees the removal applies at the next overmind boot.
func AdminRemove(dir, fleetLabel, agentID string) (AdminResult, error) {
	return adminOp(dir, fleetLabel, agentID, control.TypeAdminRemove)
}

// AdminReadd clears agentID from the overrides sidecar, then asks the live
// overmind to relaunch it from its yaml spec.
func AdminReadd(dir, fleetLabel, agentID string) (AdminResult, error) {
	return adminOp(dir, fleetLabel, agentID, control.TypeAdminReadd)
}

func adminOp(dir, fleetLabel, agentID string, op control.Type) (AdminResult, error) {
	def, ok := fleetByLabel(fleetLabel)
	if !ok {
		return AdminResult{}, fmt.Errorf("ovdash: unknown fleet %q", fleetLabel)
	}
	ovPath := filepath.Join(dir, strings.TrimSuffix(def.Socket, ".sock")+"-overrides.json")
	ov, err := supervisor.LoadOverrides(ovPath)
	if err != nil {
		// Corrupt sidecar: degrade to empty (matches overmind read semantics)
		// and let the save below rewrite it cleanly.
		ov = supervisor.Overrides{}
	}
	if op == control.TypeAdminRemove {
		ov.Add(agentID)
	} else {
		ov.Delete(agentID)
	}
	ov.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	ov.By = "dashboard"
	if err := supervisor.SaveOverrides(ovPath, ov); err != nil {
		return AdminResult{}, err //nolint:wrapcheck
	}

	conn, err := net.DialTimeout("unix", filepath.Join(dir, def.Socket), 3*time.Second)
	if err != nil {
		return AdminResult{Status: "recorded_offline", Detail: "overrides recorded; applies at next overmind start"}, nil
	}
	defer conn.Close() //nolint:errcheck
	env, err := control.NewEnvelope(op, agentID, control.AdminRequest{AgentID: agentID})
	if err != nil {
		return AdminResult{}, err //nolint:wrapcheck
	}
	if err := control.NewEncoder(conn).Encode(env); err != nil {
		return AdminResult{Status: "recorded_offline", Detail: "overrides recorded; send failed: " + err.Error()}, nil
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply, err := control.NewDecoder(conn).Decode()
	if err != nil {
		return AdminResult{Status: "recorded_offline", Detail: "overrides recorded; no ack: " + err.Error()}, nil
	}
	var ack control.AdminAck
	if err := reply.Into(&ack); err != nil {
		return AdminResult{Status: "recorded_offline", Detail: "overrides recorded; bad ack: " + err.Error()}, nil
	}
	return AdminResult{Status: ack.Status, Detail: ack.Detail}, nil
}
