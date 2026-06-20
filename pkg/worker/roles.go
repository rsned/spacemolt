package worker

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ScheduleEntry is one recurring command in a role's standing behavior.
type ScheduleEntry struct {
	Every   string `yaml:"every"`   // hourly | daily | weekly
	Command string `yaml:"command"` // a command line; may contain $TOKEN$s
}

// Role is a worker's default standing behavior: recurring scheduled commands
// plus an idle script run on idle cycles.
type Role struct {
	Schedule   []ScheduleEntry   `yaml:"schedule"`
	Idle       string            `yaml:"idle"`        // bare script name (data/scripts)
	IdleParams map[string]string `yaml:"idle_params"` // substituted into the idle script
}

type rolesFile struct {
	Roles map[string]Role `yaml:"roles"`
}

// LoadRoles parses the roles config at path. Every schedule entry must name a
// valid frequency.
func LoadRoles(path string) (map[string]Role, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("worker: read roles: %w", err)
	}
	var rf rolesFile
	if err := yaml.Unmarshal(raw, &rf); err != nil {
		return nil, fmt.Errorf("worker: parse roles: %w", err)
	}
	for name, r := range rf.Roles {
		for i, se := range r.Schedule {
			if !ValidFrequencies[se.Every] {
				return nil, fmt.Errorf("worker: role %q schedule[%d]: invalid frequency %q", name, i, se.Every)
			}
			if se.Command == "" {
				return nil, fmt.Errorf("worker: role %q schedule[%d]: empty command", name, i)
			}
		}
	}
	return rf.Roles, nil
}
