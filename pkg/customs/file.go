package customs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultMapPath is where the derived map lives; autopilot re-reads it on
// every route, so a rebuild takes effect without restarting any worker.
const DefaultMapPath = "data/overmind/customs-monitored.json"

// DefaultOverridesPath holds operator knowledge that beats the evidence.
const DefaultOverridesPath = "data/overmind/customs-overrides.json"

// Overrides force a class on a system: {"monitored": {id: why}, "clear": {id: why}}.
type Overrides struct {
	Monitored map[string]string `json:"monitored"`
	Clear     map[string]string `json:"clear"`
}

// Map is the file autopilot reads.
type Map struct {
	GeneratedAt time.Time             `json:"generated_at"`
	WindowDays  int                   `json:"window_days"`
	Systems     map[string]SystemStat `json:"systems"`
}

// LoadOverrides reads the overrides file. A missing file is no overrides.
func LoadOverrides(path string) (Overrides, error) {
	var o Overrides
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return o, fmt.Errorf("customs: read overrides: %w", err)
	}
	if err := json.Unmarshal(data, &o); err != nil {
		return o, fmt.Errorf("customs: parse overrides %s: %w", path, err)
	}
	return o, nil
}

// Apply forces each overridden system's class, keeping its evidence, and
// adds overridden systems that have no evidence at all.
func Apply(stats map[string]SystemStat, o Overrides) {
	force := func(ids map[string]string, c Class) {
		for id, why := range ids {
			id = strings.ToLower(id)
			s := stats[id]
			s.Class, s.Reason = c, why
			stats[id] = s
		}
	}
	force(o.Monitored, Monitored)
	force(o.Clear, Clear)
}

// Write replaces the map file atomically (temp file + rename), so a reader
// never sees a half-written map.
func Write(path string, m Map) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("customs: encode map: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".customs-map-*")
	if err != nil {
		return fmt.Errorf("customs: write map: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("customs: write map: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("customs: write map: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("customs: write map: %w", err)
	}
	return nil
}

// Load reads the map file. A missing file returns an error wrapping
// fs.ErrNotExist.
func Load(path string) (Map, error) {
	var m Map
	data, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("customs: read map: %w", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("customs: parse map %s: %w", path, err)
	}
	return m, nil
}

// Monitored returns the monitored systems keyed by lowercased id, each with
// a short reason for avoid-list log lines.
func (m Map) Monitored() map[string]string {
	out := map[string]string{}
	for id, s := range m.Systems {
		if s.Class != Monitored {
			continue
		}
		why := fmt.Sprintf("customs: %d inspection(s)", s.Inspections)
		if !s.LastInspection.IsZero() {
			why += ", last " + s.LastInspection.Format("2006-01-02")
		}
		if s.Reason != "" {
			why = "customs: " + s.Reason
		}
		out[strings.ToLower(id)] = why
	}
	return out
}
