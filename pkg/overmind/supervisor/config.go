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
