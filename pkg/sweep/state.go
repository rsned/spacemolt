package sweep

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Phase is where a ship is in its sweep.
type Phase string

const (
	PhaseStage   Phase = "stage"   // pre-lap: refuel, lanes to the launch point, drift to the target
	PhaseLap     Phase = "lap"     // following its ring
	PhaseExplore Phase = "explore" // a drift landed it in a system: explore the cluster
	PhaseHome    Phase = "home"    // lap over or fuel low: heading for an empire station
	PhaseDone    Phase = "done"    // docked at a station; the sweep is over
)

// Gap is an arc of the ring the ship did not trace (a storm deflected it).
type Gap struct {
	FromDeg float64 `json:"from_deg"`
	ToDeg   float64 `json:"to_deg"`
	Reason  string  `json:"reason"`
}

// State is one ship's sweep, saved after every change so a restart, explore
// run or GSA tow resumes at the saved ring angle.
type State struct {
	Ring     float64 `json:"ring"`
	StartDeg float64 `json:"start_deg"`
	// SpanDeg ends the lap after this many degrees swept (a partial pass); 0 is a full 360° lap.
	SpanDeg     float64 `json:"span_deg,omitempty"`
	ProgressDeg float64 `json:"progress_deg"` // unwrapped; only decreases
	SweptDeg    float64 `json:"swept_deg"`
	Phase       Phase   `json:"phase"`

	TargetX        float64 `json:"target_x"`
	TargetY        float64 `json:"target_y"`
	TargetDeg      float64 `json:"target_deg"`
	PlannedBearing float64 `json:"planned_bearing"`

	LaunchCost         float64 `json:"launch_cost"`
	LaunchCostMeasured bool    `json:"launch_cost_measured"`
	Launches           int     `json:"launches"`

	// LastX/LastY is the latest drift reading; Drifted marks a drift in
	// progress, the cue that the next in-system pass is an arrival.
	LastX   float64 `json:"last_x"`
	LastY   float64 `json:"last_y"`
	Drifted bool    `json:"drifted"`

	// ReadSinceLaunch is set once a drift reading arrives after the latest
	// launch: only then is LastX/LastY a measurement of where the drift
	// ended rather than where it began.
	ReadSinceLaunch bool `json:"read_since_launch"`
	// LastReadTick is the game tick of LastX/LastY. A landing measures only
	// from a reading a tick or two old: a worker stopped mid-drift and
	// restarted after the landing holds a reading from far back along the
	// drift (explorer-11 wrote Ash Pearl 160 LY short on 2026-11-13).
	LastReadTick int64 `json:"last_read_tick,omitempty"`
	// RefuelTriedTick is when a docked, lap-done ship last asked the desk for
	// fuel; a dry desk is retried hourly, not every pass.
	RefuelTriedTick int64 `json:"refuel_tried_tick,omitempty"`
	// Landed marks a lap drift that stopped in a system: the rejoin then
	// aims past the landing, so the ship does not land there again.
	Landed bool `json:"landed"`
	// PendingMeasure* hold an arrival measurement for a system the KB has
	// no row for yet (the tour creates it); it is written once it can be.
	PendingMeasureSystem string  `json:"pending_measure_system,omitempty"`
	PendingMeasureName   string  `json:"pending_measure_name,omitempty"`
	PendingX             float64 `json:"pending_x,omitempty"`
	PendingY             float64 `json:"pending_y,omitempty"`

	// StageRefueled marks the staging refuel stop as done.
	StageRefueled bool `json:"stage_refueled,omitempty"`

	ArrivalToured bool `json:"arrival_toured"`
	ExploreJumps  int  `json:"explore_jumps"`

	Gaps       []Gap    `json:"gaps,omitempty"`
	Unfinished []string `json:"unfinished,omitempty"` // systems left with unexplored connections
	HomeSystem string   `json:"home_system,omitempty"`

	// Interior void sweep (Mode ModeVoid): the claimed zone, its square
	// wave, the index of the waypoint being flown to, and the hidden
	// systems found in it. Ring fields are unused in this mode.
	Mode      string   `json:"mode,omitempty"`
	ZoneID    string   `json:"zone_id,omitempty"`
	Waypoints []Point  `json:"waypoints,omitempty"`
	Waypoint  int      `json:"waypoint,omitempty"`
	ZoneFound []string `json:"zone_found,omitempty"`
	UpdatedUTC string   `json:"updated_utc,omitempty"`
}

// NewState starts a lap on the given ring at startDeg, with an estimated
// launch cost that the first launch replaces with a measurement.
func NewState(ring, startDeg, launchCostEstimate float64) *State {
	return &State{Ring: ring, StartDeg: startDeg, ProgressDeg: startDeg, Phase: PhaseLap, LaunchCost: launchCostEstimate}
}

// ModeVoid marks a State flying the interior void sweep.
const ModeVoid = "void"

// NewVoidState starts an interior void sweep with no zone yet.
func NewVoidState(launchCostEstimate float64) *State {
	return &State{Mode: ModeVoid, Phase: PhaseLap, LaunchCost: launchCostEstimate}
}

// AddUnfinished records a system left with unvisited connections, once.
func (s *State) AddUnfinished(systemID string) {
	if systemID != "" && !slices.Contains(s.Unfinished, systemID) {
		s.Unfinished = append(s.Unfinished, systemID)
	}
}

// StatePath is <agentsDir>/<agentID>/sweep_state.json.
func StatePath(agentsDir, agentID string) string {
	return filepath.Join(agentsDir, agentID, "sweep_state.json")
}

// Load reads a saved state; a missing file is (nil, false, nil).
func Load(path string) (*State, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("sweep state read: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false, fmt.Errorf("sweep state decode %s: %w", path, err)
	}
	return &s, true, nil
}

// Save writes the state atomically (temp file + rename) and stamps UpdatedUTC.
func Save(path string, s *State) error {
	s.UpdatedUTC = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("sweep state marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sweep state mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("sweep state write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("sweep state replace: %w", err)
	}
	return nil
}
