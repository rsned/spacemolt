package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Phases of the frontier full-explore role.
const (
	frontierVerify = "verify" // check the fit, pick the first target
	frontierStage  = "stage"  // lanes to the system nearest the target
	frontierLaunch = "launch" // pathfinder launch at the target
	frontierWork   = "work"   // tour the claimed cluster, system by system
	frontierRefuel = "refuel" // detour to a station for fuel, hull and kits
	frontierNext   = "next"   // pick the next target
	frontierHome   = "home"   // nothing left: back to Conjunction Junction
	frontierDone   = "done"
)

// frontierState is one ship's place in the frontier run, saved after every
// pass so a restart resumes where it stopped. The shared ledger
// (sweep.FrontierRun) is the record of what is finished; this is only where
// the ship is going.
type frontierState struct {
	Phase   string  `json:"phase"`
	Target  string  `json:"target,omitempty"` // system being flown to or worked
	TargetX float64 `json:"target_x,omitempty"`
	TargetY float64 `json:"target_y,omitempty"`
	// Cluster is the claimed cluster, breadth first; Done its finished part.
	Cluster []string `json:"cluster,omitempty"`
	Done    []string `json:"done,omitempty"`
	// Launches counts pathfinder launches at Target; two that do not arrive
	// mark it unreachable.
	Launches int `json:"launches,omitempty"`
	// LaunchDist is how far from Target the last launch at it started: a
	// launch from well inside that is progress and starts the count again.
	LaunchDist float64 `json:"launch_dist,omitempty"`
	// ResumeAt is the system to return to after a refuel or repair stop.
	ResumeAt string `json:"resume_at,omitempty"`

	LaunchCost         float64 `json:"launch_cost"`
	LaunchCostMeasured bool    `json:"launch_cost_measured,omitempty"`

	// The drift in progress: where it is aimed, the bearing plotted, and the
	// latest reading (see sweep.State for why the reading carries a tick).
	AimX            float64 `json:"aim_x,omitempty"`
	AimY            float64 `json:"aim_y,omitempty"`
	PlannedBearing  float64 `json:"planned_bearing,omitempty"`
	Drifted         bool    `json:"drifted,omitempty"`
	ReadSinceLaunch bool    `json:"read_since_launch,omitempty"`
	LastX           float64 `json:"last_x,omitempty"`
	LastY           float64 `json:"last_y,omitempty"`
	LastReadTick    int64   `json:"last_read_tick,omitempty"`

	// StopSystem and Stops count the station detours a system's tour has
	// forced (hull at the floor, tank empty): the second ends it incomplete.
	StopSystem string `json:"stop_system,omitempty"`
	Stops      int    `json:"stops,omitempty"`
	// TrySystem and Tries count a system's failed tours: the third ends it
	// incomplete.
	TrySystem string `json:"try_system,omitempty"`
	Tries     int    `json:"tries,omitempty"`

	// Distress is set once a tow has been asked for, cleared at a station.
	Distress   bool      `json:"distress,omitempty"`
	UpdatedUTC time.Time `json:"updated_utc"`
}

// frontierStatePath is <agentsDir>/<agentID>/frontier_state.json.
func frontierStatePath(agentsDir, agentID string) string {
	return filepath.Join(agentsDir, agentID, "frontier_state.json")
}

// loadFrontierState reads the state; a missing file is a ship that has not
// started (phase verify).
func loadFrontierState(path string) (*frontierState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &frontierState{Phase: frontierVerify}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("frontier state: %w", err)
	}
	var s frontierState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("frontier state %s: %w", path, err)
	}
	if s.Phase == "" {
		s.Phase = frontierVerify
	}
	return &s, nil
}

// saveFrontierState writes the state atomically.
func saveFrontierState(path string, s *frontierState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("frontier state: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("frontier state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("frontier state: %w", err)
	}
	return os.Rename(tmp, path)
}
