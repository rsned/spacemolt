package plans

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// SaveRun atomically writes pr to <dir>/<plan-id>.state.json, guarded by an
// exclusive flock on a sidecar .lock file (mirrors pkg/rescue/queue.go's
// withLock mechanics: the lock file is never renamed, so the lock identity
// stays stable across writers).
func SaveRun(dir string, pr *PlanRun) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("plans: mkdir: %w", err)
	}
	path := statePath(dir, pr.Manifest.PlanID)

	lock, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("plans: open lock: %w", err)
	}
	defer lock.Close() //nolint:errcheck
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("plans: flock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	data, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return fmt.Errorf("plans: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("plans: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("plans: rename: %w", err)
	}
	return nil
}

// LoadRun reads and parses one plan-run state file, under the same flock
// discipline as SaveRun.
func LoadRun(path string) (*PlanRun, error) {
	lock, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("plans: open lock: %w", err)
	}
	defer lock.Close() //nolint:errcheck
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("plans: flock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plans: read %s: %w", path, err)
	}
	var pr PlanRun
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, fmt.Errorf("plans: parse %s: %w", path, err)
	}
	return &pr, nil
}

// LoadAllRuns globs <dir>/*.state.json and loads each. Corrupt files are
// never silently skipped: they are collected as errors alongside the runs
// that did parse, so the caller can log them without losing the good runs.
func LoadAllRuns(dir string) ([]*PlanRun, []error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.state.json"))
	if err != nil {
		return nil, []error{fmt.Errorf("plans: glob %s: %w", dir, err)}
	}

	var runs []*PlanRun
	var errs []error
	for _, path := range matches {
		pr, err := LoadRun(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		runs = append(runs, pr)
	}
	return runs, errs
}

// statePath returns the on-disk path for a plan-run's state file.
func statePath(dir, planID string) string {
	return filepath.Join(dir, planID+".state.json")
}
