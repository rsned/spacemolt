package supervisor

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WorkerSpec is one roster entry the supervisor will spawn.
type WorkerSpec struct {
	AgentID string `yaml:"agent_id"`
	Role    string `yaml:"role"`
	Station string `yaml:"station"`
	// MissionCategories, when set, is forwarded to the worker as
	// --mission-categories (comma-joined) so a mission-runner fleet can run
	// beyond the delivery default — e.g. the learning pool's
	// [delivery, exploration]. Empty leaves the worker's delivery-only default.
	MissionCategories []string `yaml:"mission_categories,omitempty"`
}

type fleetFile struct {
	Workers []WorkerSpec `yaml:"workers"`
}

// LoadFleet parses the fleet roster YAML at path.
func LoadFleet(path string) ([]WorkerSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("supervisor: read fleet: %w", err)
	}
	var ff fleetFile
	if err := yaml.Unmarshal(raw, &ff); err != nil {
		return nil, fmt.Errorf("supervisor: parse fleet: %w", err)
	}
	for i, w := range ff.Workers {
		if w.AgentID == "" {
			return nil, fmt.Errorf("supervisor: fleet entry %d missing agent_id", i)
		}
	}
	return ff.Workers, nil
}
