package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// huntRoamFile is the per-agent record of how far a hunter has had to look for
// creature work. It sits beside hunt-chain.json and follows its persistence
// idiom (atomic tmp+rename, a missing file is not an error).
const huntRoamFile = "hunt_roam.json"

// huntRoam is one hunter's search state across passes and restarts.
//
// DryPasses counts consecutive Station Need searches that found no qualifying
// ground; it drives the adaptive radius (huntNeedRadius) and resets to 0 when
// a pass finds a job. Visited is reserved for the roaming move (empire
// capitals already tried) and is not read here.
type huntRoam struct {
	DryPasses  int      `json:"dry_passes"`
	Visited    []string `json:"visited,omitempty"`
	UpdatedUTC string   `json:"updated_utc,omitempty"`
}

// huntRoamPath is <agentsDir>/<agentID>/hunt_roam.json, agentsDir defaulting
// to DefaultAgentsDir.
func huntRoamPath(agentsDir, agentID string) string {
	if agentsDir == "" {
		agentsDir = DefaultAgentsDir
	}
	return filepath.Join(agentsDir, agentID, huntRoamFile)
}

// loadHuntRoam reads the roam record. A missing file is a fresh hunter and
// returns the zero record with no error.
func loadHuntRoam(path string) (huntRoam, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return huntRoam{}, nil
		}
		return huntRoam{}, fmt.Errorf("hunt-roam read: %w", err)
	}
	var r huntRoam
	if err := json.Unmarshal(raw, &r); err != nil {
		return huntRoam{}, fmt.Errorf("hunt-roam decode: %w", err)
	}
	return r, nil
}

// saveHuntRoam writes the roam record atomically, creating the agent
// directory if needed.
func saveHuntRoam(path string, r huntRoam) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("hunt-roam marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("hunt-roam mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("hunt-roam write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("hunt-roam replace: %w", err)
	}
	return nil
}

// huntLoadRoam reads this agent's roam record, logging and returning the zero
// record on any failure. With no AgentID there is no file.
func huntLoadRoam(deps HuntDeps, out io.Writer) huntRoam {
	if deps.AgentID == "" {
		return huntRoam{}
	}
	r, err := loadHuntRoam(huntRoamPath(deps.AgentsDir, deps.AgentID))
	if err != nil {
		fmt.Fprintf(out, "hunt: %v; starting the search radius over\n", err) //nolint:errcheck
		return huntRoam{}
	}
	return r
}

// huntSetDryPasses records the dry-pass count, writing only when it changed.
func huntSetDryPasses(deps HuntDeps, out io.Writer, n int) {
	if deps.AgentID == "" {
		return
	}
	path := huntRoamPath(deps.AgentsDir, deps.AgentID)
	r, err := loadHuntRoam(path)
	if err != nil {
		r = huntRoam{} // a corrupt record is replaced, not preserved
	}
	if err == nil && r.DryPasses == n {
		return
	}
	r.DryPasses = n
	r.UpdatedUTC = rfc(huntNow(deps))
	if err := saveHuntRoam(path, r); err != nil {
		fmt.Fprintf(out, "hunt: %v\n", err) //nolint:errcheck
	}
}
