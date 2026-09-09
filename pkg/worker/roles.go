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
	// IdleTicks multiplies the idle-loop period, counted in GAME TICKS
	// (0 or 1 -> one tick, the default every role had). Expressed in ticks
	// rather than a duration because the loop's natural unit is the tick: the
	// game advances once per tick and a mutation is capped at one per tick per
	// agent, so any sub-tick period can only emit redundant calls.
	//
	// It exists to trade idle responsiveness for shared-IP headroom. Command
	// volume scales with worker count over this period, and the fleet's per-IP
	// budget is fixed, so a role doing low-value work can be slowed instead of
	// being shut off: two ticks roughly halves that role's contribution while
	// keeping every agent alive and progressing. Raised for the pool roles after
	// the 2026-09-09 block, where 170 concurrently-active workers crossed a line
	// that ~144 had not.
	IdleTicks int `yaml:"idle_ticks"`
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
		// A negative multiplier would yield a non-positive sleep and spin the
		// idle loop with no delay — the exact opposite of what this field is
		// for, and a very expensive typo on a shared IP. Refuse it at load.
		if r.IdleTicks < 0 {
			return nil, fmt.Errorf("worker: role %q: idle_ticks must be >= 0, got %d", name, r.IdleTicks)
		}
	}
	return rf.Roles, nil
}
