package linedash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

const statusSuffix = "-status.json"

// Live is one agent's current position and condition, from a fleet status file.
type Live struct {
	AgentID, Fleet, System, POI  string
	Docked, Healthy, Quarantined bool
	Credits, Fuel, MaxFuel       float64
	CargoUsed, CargoCap          float64
	LastSeen, CapturedAt         time.Time
}

// LoadLive reads every *-status.json in dir. The same agent in two files keeps
// the freshest captured_at. A missing dir yields an empty map.
func LoadLive(dir string) (map[string]Live, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*"+statusSuffix))
	if err != nil {
		return nil, fmt.Errorf("glob status files: %w", err)
	}
	out := map[string]Live{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read status %s: %w", f, err)
		}
		var sf balances.StatusFile
		if err := json.Unmarshal(raw, &sf); err != nil {
			return nil, fmt.Errorf("parse status %s: %w", f, err)
		}
		fleet := strings.TrimSuffix(filepath.Base(f), statusSuffix)
		captured := parseTime(sf.CapturedAt)
		for _, w := range sf.Workers {
			if old, ok := out[w.AgentID]; ok && !captured.After(old.CapturedAt) {
				continue
			}
			out[w.AgentID] = Live{
				AgentID:     w.AgentID,
				Fleet:       fleet,
				System:      w.System,
				POI:         w.POI,
				Docked:      w.Docked,
				Healthy:     w.Healthy,
				Quarantined: w.Quarantined,
				Credits:     w.Credits,
				Fuel:        w.Fuel,
				MaxFuel:     w.MaxFuel,
				CargoUsed:   w.CargoUsed,
				CargoCap:    w.CargoCapacity,
				LastSeen:    parseTime(w.LastSeen),
				CapturedAt:  captured,
			}
		}
	}
	return out, nil
}

// parseTime parses RFC3339 (fractional seconds accepted); garbage is the zero time.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
