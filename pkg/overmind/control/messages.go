// Package control defines the overmind<->worker control-channel wire format.
package control

import (
	"encoding/json"
	"fmt"
)

// Type identifies a control message kind.
type Type string

const (
	TypeHello      Type = "hello"
	TypeStatus     Type = "status"
	TypeEvent      Type = "event"
	TypeAbort      Type = "abort"
	TypePause      Type = "pause"
	TypeResume     Type = "resume"
	TypeAssign     Type = "assign"
	TypeDrain      Type = "drain"
	TypeAdminRemove Type = "admin_remove"
	TypeAdminReadd  Type = "admin_readd"
	TypeAdminAck    Type = "admin_ack"
)

// Envelope is the framed wire unit; one Envelope is one NDJSON line.
type Envelope struct {
	Type    Type            `json:"type"`
	AgentID string          `json:"agent_id"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Hello is the first message a worker sends after connecting.
type Hello struct {
	AgentID string `json:"agent_id"`
	Role    string `json:"role"`
	Station string `json:"station"`
	PID     int    `json:"pid"`
}

// Status is a worker heartbeat snapshot.
type Status struct {
	System           string  `json:"system"`
	POI              string  `json:"poi"`
	Docked           bool    `json:"docked"`
	Hull             float64 `json:"hull"`
	MaxHull          float64 `json:"max_hull"`
	Fuel             float64 `json:"fuel"`
	MaxFuel          float64 `json:"max_fuel"`
	Credits          float64 `json:"credits"`
	CargoUsed        float64 `json:"cargo_used"`
	CargoCapacity    float64 `json:"cargo_capacity"`
	StandingBehavior string  `json:"standing_behavior"`
	ActiveTaskID     string  `json:"active_task_id"`
	// Activity is a short human-readable description of the unit of work the
	// standing role is currently doing (e.g. "Mission Steel Plate Order",
	// "Rescuing haul-3"). Empty when idle. Produced by the role goroutine and
	// read by the heartbeat goroutine via a shared atomic on WorkerDispatch.
	Activity   string `json:"activity,omitempty"`
	FactionID  string `json:"faction_id,omitempty"`
	FactionTag string `json:"faction_tag,omitempty"`
	Drained    bool   `json:"drained,omitempty"`
	// Disconnected reports that the worker's game-server connection is currently
	// down (it is reconnecting via the fleet-wide reconnect gate). It keeps
	// heartbeating to the overmind over the control socket throughout. The
	// supervisor uses this to avoid restarting a worker that is already
	// self-healing: a restart forces a fresh login that cannot succeed during a
	// per-IP rate-limit block and deepens it. Omitted (false) means connected,
	// so an older worker binary that never sets it is treated as connected.
	Disconnected bool   `json:"disconnected,omitempty"`
	Timestamp    string `json:"timestamp"`
}

// Event is a notable worker-side occurrence (action result, danger signal).
type Event struct {
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	Timestamp string `json:"timestamp"`
}

// Abort tells a worker to stop now; Flee requests undock/flee first.
type Abort struct {
	Reason string `json:"reason"`
	Flee   bool   `json:"flee"`
}

// Assign tells a worker to run a one-shot task: resolve Script via the worker's
// script search path, substitute Params ($KEY$) into it, then run it once in
// place of the idle behavior. Completion is reported back via an Event
// (Kind "task_done" / "task_failed").
type Assign struct {
	TaskID string            `json:"task_id"`
	Script string            `json:"script"`
	Params map[string]string `json:"params,omitempty"`
}

// AdminRequest asks the overmind to change fleet membership for one agent.
// Sent by an admin client (dashboard backend) instead of a Hello; the
// connection receives one AdminAck and closes.
type AdminRequest struct {
	AgentID string `json:"agent_id"`
}

// AdminAck statuses.
const (
	AckAccepted       = "accepted"
	AckUnknownAgent   = "unknown_agent"
	AckAlreadyPending = "already_pending"
)

// AdminAck is the overmind's reply to an AdminRequest.
type AdminAck struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
}

// NewEnvelope marshals payload and wraps it with the given type and agent id.
func NewEnvelope(t Type, agentID string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("control: marshal payload: %w", err)
	}
	return Envelope{Type: t, AgentID: agentID, Payload: raw}, nil
}

// Into unmarshals the envelope payload into v.
func (e Envelope) Into(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("control: decode payload: %w", err)
	}
	return nil
}
