package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Overrides is the dashboard-written membership sidecar: agents in Removed are
// subtracted from the yaml roster at boot and on SIGHUP. The fleet yaml itself
// is never machine-rewritten; this file is the only machine-written state.
type Overrides struct {
	Removed   []string `json:"removed"`
	UpdatedAt string   `json:"updated_at,omitempty"`
	By        string   `json:"by,omitempty"`
}

// IsRemoved reports whether agentID is override-removed.
func (o Overrides) IsRemoved(agentID string) bool {
	return slices.Contains(o.Removed, agentID)
}

// Add records agentID as removed (idempotent).
func (o *Overrides) Add(agentID string) {
	if !o.IsRemoved(agentID) {
		o.Removed = append(o.Removed, agentID)
	}
}

// Delete clears agentID from the removed set (idempotent).
func (o *Overrides) Delete(agentID string) {
	o.Removed = slices.DeleteFunc(o.Removed, func(id string) bool { return id == agentID })
}

// LoadOverrides reads the sidecar. A missing file is an empty override set; a
// corrupt file returns empty plus the error so the caller can log loudly and
// proceed (never block a fleet on a bad sidecar).
func LoadOverrides(path string) (Overrides, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Overrides{}, nil
	}
	if err != nil {
		return Overrides{}, fmt.Errorf("supervisor: read overrides: %w", err)
	}
	var o Overrides
	if err := json.Unmarshal(raw, &o); err != nil {
		return Overrides{}, fmt.Errorf("supervisor: parse overrides %s: %w", path, err)
	}
	return o, nil
}

// SaveOverrides writes the sidecar atomically (temp file + rename).
func SaveOverrides(path string, o Overrides) error {
	raw, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return fmt.Errorf("supervisor: marshal overrides: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".overrides-*.tmp")
	if err != nil {
		return fmt.Errorf("supervisor: temp overrides: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("supervisor: write overrides: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("supervisor: close overrides: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("supervisor: rename overrides: %w", err)
	}
	return nil
}

// SubtractOverrides returns specs minus the override-removed agents.
func SubtractOverrides(specs []WorkerSpec, o Overrides) []WorkerSpec {
	out := make([]WorkerSpec, 0, len(specs))
	for _, sp := range specs {
		if !o.IsRemoved(sp.AgentID) {
			out = append(out, sp)
		}
	}
	return out
}
