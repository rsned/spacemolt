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
	// EnableFreight forwards --enable-freight, opting this worker into the
	// /shipping carrier path (evaluated co-equally with the mission board).
	// Default false = freight fully dormant. Canary one worker before any
	// pool rollout — see the Rollout section of
	// docs/superpowers/plans/2026-07-20-shipping-carrier-subproject-b.md.
	EnableFreight bool `yaml:"enable_freight,omitempty"`
	// FreightMaxPackages forwards --freight-max-packages, capping concurrent
	// freight contracts (sub-project C multi-package trips). 0/1 = the v1
	// single-contract behavior; canary fighter-4 runs 3. Layers UNDER the
	// server/cargo headroom gates.
	FreightMaxPackages int `yaml:"freight_max_packages,omitempty"`
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
