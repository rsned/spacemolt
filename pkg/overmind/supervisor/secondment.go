package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Secondment is one agent's round trip out of its home fleet and back.
//
// A hauler that finishes a delivery in nebula space is already standing next to
// the unlock chain's giver (treasure_cache_trading_post, system treasure_cache),
// so running the chain from there costs almost nothing — while sending an idle
// hauler there on purpose costs a 20+ jump deadhead. The trip is therefore taken
// opportunistically: the worker NOMINATES itself when it happens to be in the
// right place, and a reconciler moves it.
//
// It is a loan, not a transfer: the agent returns to haul once it holds the
// unlock, and the haul fleet ends up fully stronghold-capable at the same size
// it started. See PhaseNominated -> PhaseSeconded -> PhaseReturning -> PhaseHome.
type Secondment struct {
	AgentID string `json:"agent_id"`
	// HomeFleet is the fleet the agent belongs to and returns to.
	HomeFleet string `json:"home_fleet"`
	// AwayFleet is the fleet it is loaned to.
	AwayFleet string `json:"away_fleet"`
	// Reason is free text for the operator: why this agent, why now.
	Reason string `json:"reason,omitempty"`
	// NominatedAt/StationID record where the worker was when it qualified,
	// which is the whole justification for the trip being cheap.
	NominatedAt string `json:"nominated_at"`
	StationID   string `json:"station_id,omitempty"`
	SystemID    string `json:"system_id,omitempty"`

	Phase     string `json:"phase"`
	UpdatedAt string `json:"updated_at,omitempty"`
	// Note carries the last thing that happened, especially a failure. A
	// secondment that cannot proceed must say why rather than sit silent.
	Note string `json:"note,omitempty"`
}

// Secondment phases. The order matters: a worker must be confirmed STOPPED in
// its home fleet before it is started in the away fleet, because running the
// same agent twice takes the game session down with status 4001
// (session_replaced) and both copies lose their connection.
const (
	// PhaseNominated: the worker asked. Nothing has moved yet.
	PhaseNominated = "nominated"
	// PhaseSeconded: removed from home, running in the away fleet.
	PhaseSeconded = "seconded"
	// PhaseReturning: the away work is done; move it back.
	PhaseReturning = "returning"
	// PhaseHome: back in its home fleet. Terminal, kept as a record.
	PhaseHome = "home"
	// PhaseFailed: the reconciler could not complete a step. Terminal until an
	// operator intervenes; never retried automatically, because a half-applied
	// membership change is exactly the state that double-runs an agent.
	PhaseFailed = "failed"
)

// Secondments is the on-disk ledger, one file shared by both fleets' reconcilers.
type Secondments struct {
	Entries []Secondment `json:"entries"`
}

// Active returns the entry for agentID that is still in flight, if any.
//
// PhaseFailed counts as active on purpose. A failure means a membership change
// was left half-applied, and the one state that must never be entered blindly
// from there is "start this agent somewhere" — that is how the same agent ends
// up running twice. So a failed trip pins the agent until an operator clears the
// entry, rather than letting the next pass re-nominate it into the same hole.
func (s Secondments) Active(agentID string) (Secondment, bool) {
	for _, e := range s.Entries {
		if e.AgentID != agentID {
			continue
		}
		if e.Phase != PhaseHome {
			return e, true
		}
	}
	return Secondment{}, false
}

// InFlight counts trips that are open — asked for but not yet finished. Use it
// to answer "is anything happening", not to size fleet coverage: a nomination
// costs the home fleet nothing until it is acted on.
func (s Secondments) InFlight() int {
	n := 0
	for _, e := range s.Entries {
		if e.Phase == PhaseNominated || e.Phase == PhaseSeconded || e.Phase == PhaseReturning {
			n++
		}
	}
	return n
}

// Away counts agents actually absent from their home fleet right now. This is
// the number the reconciler's cap governs — a queue of nominations may be any
// length without costing the home fleet a single hull, and counting those would
// stall the queue forever at a cap of one.
func (s Secondments) Away() int {
	n := 0
	for _, e := range s.Entries {
		if e.Phase == PhaseSeconded || e.Phase == PhaseReturning {
			n++
		}
	}
	return n
}

// Nominate records a new nomination. It is a no-op when the agent already has
// one in flight — a worker re-nominating every pass must not queue up trips.
func (s *Secondments) Nominate(e Secondment) bool {
	if _, ok := s.Active(e.AgentID); ok {
		return false
	}
	e.Phase = PhaseNominated
	e.UpdatedAt = e.NominatedAt
	s.Entries = append(s.Entries, e)
	return true
}

// SetPhase advances agentID's active entry, stamping when and why.
func (s *Secondments) SetPhase(agentID, phase, note, now string) bool {
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.AgentID != agentID {
			continue
		}
		if e.Phase == PhaseHome || e.Phase == PhaseFailed {
			continue
		}
		e.Phase, e.Note, e.UpdatedAt = phase, note, now
		return true
	}
	return false
}

// Prune drops terminal entries older than keep, so the ledger stays readable
// without losing the recent history an operator would want after a bad night.
func (s *Secondments) Prune(now time.Time, keep time.Duration) {
	s.Entries = slices.DeleteFunc(s.Entries, func(e Secondment) bool {
		if e.Phase != PhaseHome && e.Phase != PhaseFailed {
			return false
		}
		ts, err := time.Parse(time.RFC3339, e.UpdatedAt)
		if err != nil {
			return false // unparseable stamp: keep it, it is evidence
		}
		return now.Sub(ts) > keep
	})
}

// LoadSecondments reads the ledger. A missing file is an empty ledger; a corrupt
// one returns empty plus the error, so a caller can log loudly and carry on
// rather than block a fleet on a bad sidecar (same contract as LoadOverrides).
func LoadSecondments(path string) (Secondments, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Secondments{}, nil
	}
	if err != nil {
		return Secondments{}, fmt.Errorf("supervisor: read secondments: %w", err)
	}
	var s Secondments
	if err := json.Unmarshal(raw, &s); err != nil {
		return Secondments{}, fmt.Errorf("supervisor: parse secondments %s: %w", path, err)
	}
	return s, nil
}

// SaveSecondments writes the ledger atomically (temp file + rename), so a reader
// racing a writer sees either the old file or the new one, never a partial.
func SaveSecondments(path string, s Secondments) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("supervisor: marshal secondments: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("supervisor: mkdir for secondments: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("supervisor: write secondments: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("supervisor: rename secondments: %w", err)
	}
	return nil
}
