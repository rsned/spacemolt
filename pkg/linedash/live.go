package linedash

import (
	"encoding/json"
	"errors"
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
// the freshest captured_at. A missing dir yields an empty map and a nil error.
//
// A non-nil error together with a non-nil map means "partial": the named
// unreadable or unparsable files were skipped. The caller should log the error
// and render the map.
func LoadLive(dir string) (map[string]Live, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*"+statusSuffix))
	if err != nil {
		return nil, fmt.Errorf("glob status files: %w", err)
	}
	var errs []error
	out := map[string]Live{}
	for _, f := range files {
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			errs = append(errs, fmt.Errorf("read status %s: %w", filepath.Base(f), rerr))
			continue
		}
		var sf balances.StatusFile
		if jerr := json.Unmarshal(raw, &sf); jerr != nil {
			errs = append(errs, fmt.Errorf("parse status %s: %w", filepath.Base(f), jerr))
			continue
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
	return out, errors.Join(errs...)
}

// parseTime parses RFC3339 (fractional seconds accepted); garbage is the zero time.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
