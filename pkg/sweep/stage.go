package sweep

import (
	"fmt"
	"strings"
)

// Stage is the pre-lap staging route (sweep_stage "refuel,launch,target"):
// top up at the station in Refuel, fly by lanes to Launch, then drift to
// the frontier system Target, which is explored before the lap starts.
type Stage struct {
	Refuel string
	Launch string
	Target string
}

// Set reports whether a stage is configured.
func (s Stage) Set() bool { return s.Target != "" }

// ParseStage decodes "refuel,launch,target"; "" is no stage.
func ParseStage(v string) (Stage, error) {
	if strings.TrimSpace(v) == "" {
		return Stage{}, nil
	}
	parts := strings.Split(v, ",")
	if len(parts) != 3 {
		return Stage{}, fmt.Errorf("sweep_stage %q: want refuel,launch,target", v)
	}
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
		if parts[i] == "" {
			return Stage{}, fmt.Errorf("sweep_stage %q: empty system id", v)
		}
	}
	return Stage{Refuel: parts[0], Launch: parts[1], Target: parts[2]}, nil
}
