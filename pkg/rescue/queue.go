// Package rescue is the cross-overmind stranded-worker rescue channel: a
// flock-guarded JSON queue file. Fleet overminds enqueue quarantined workers;
// the assist overmind's workers claim and complete rescues; fleet overminds
// relaunch workers whose record is done. Operators edit the same file for
// manual rescues. Spec: docs/superpowers/specs/2026-07-03-stranded-worker-quarantine-design.md
package rescue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"
)

// Status is a rescue record's lifecycle state.
type Status string

// Lifecycle: pending → claimed → done | failed. done triggers rejoin; failed
// waits for the operator.
const (
	StatusPending Status = "pending"
	StatusClaimed Status = "claimed"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Record is one stranded worker awaiting (or through) rescue.
type Record struct {
	AgentID        string  `json:"agent_id"`
	TargetUsername string  `json:"target_username"`
	Fleet          string  `json:"fleet"`
	System         string  `json:"system"`
	SystemID       string  `json:"system_id"`
	POI            string  `json:"poi"`
	Fuel           float64 `json:"fuel"`
	MaxFuel        float64 `json:"max_fuel"`
	RescueFuel     int     `json:"rescue_fuel"`
	Reason         string  `json:"reason"`
	Status         Status  `json:"status"`
	ClaimedBy      string  `json:"claimed_by"`
	Error          string  `json:"error,omitempty"`
	RequestedAt    string  `json:"requested_at"`
	UpdatedAt      string  `json:"updated_at"`

	// PendingSince is when the record last entered pending — stamped by
	// Enqueue and by any transition back into pending. The claim election
	// widens by how long a rescue has gone UNCLAIMED, which is not how long
	// ago it was filed: reading RequestedAt made a days-old record pass every
	// assister's gate at every rank, disabling nearest-home ranking entirely
	// (live 2026-07-29: a Nexus Prime rescue went to the Krynn assister, 20
	// jumps out, while the Nexus Prime one sat idle at the strand system).
	// Empty on records written before this field existed; readers fall back to
	// RequestedAt.
	PendingSince string `json:"pending_since,omitempty"`
	// Attempts counts rescues that got as far as a claim and then failed. A
	// failure re-queues rather than terminating (see FailedBy), so this is the
	// bound that stops an unrescuable record cycling forever.
	Attempts int `json:"attempts,omitempty"`
	// FailedBy lists assisters that have already failed this record, so the
	// re-queue goes to a DIFFERENT one instead of the same agent immediately
	// re-winning the election it just lost.
	FailedBy []string `json:"failed_by,omitempty"`
}

// HasFailed reports whether agentID has already failed this record.
func (r Record) HasFailed(agentID string) bool {
	return slices.Contains(r.FailedBy, agentID)
}

// Queue is a handle on the shared queue file. Safe for concurrent use across
// processes: every operation takes an exclusive flock on a sidecar lock file
// (never renamed, so the lock identity is stable), then atomically rewrites
// the queue via temp+rename.
type Queue struct {
	path string
	now  func() time.Time
}

// NewQueue returns a queue handle on path. The file need not exist yet.
func NewQueue(path string) *Queue {
	return &Queue{path: path, now: time.Now}
}

// List returns all records. A missing file is an empty queue; a corrupt file
// is an error (callers log and skip the tick rather than clobbering it).
func (q *Queue) List() ([]Record, error) {
	var out []Record
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		out = recs
		return nil, false, nil
	})
	return out, err
}

// Enqueue appends rec (status pending, timestamps stamped) unless the agent
// already has a record of any status — one record per agent, so re-detection
// while a rescue is in flight is a no-op.
func (q *Queue) Enqueue(rec Record) (bool, error) {
	inserted := false
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for _, r := range recs {
			if r.AgentID == rec.AgentID {
				return nil, false, nil
			}
		}
		ts := q.now().UTC().Format(time.RFC3339)
		rec.Status = StatusPending
		rec.RequestedAt, rec.UpdatedAt, rec.PendingSince = ts, ts, ts
		inserted = true
		return append(recs, rec), true, nil
	})
	return inserted, err
}

// Transition moves the agent's record from → to (compare-and-set; false when
// the record is absent or not in from), applying mutate to the record first.
//
// Entering pending re-stamps PendingSince, so a released or re-queued rescue
// restarts the election's takeover clock instead of inheriting the age it had
// accumulated before someone claimed it. Stamping here rather than at each
// call site is deliberate: every route back into pending gets it, including
// ones added later.
func (q *Queue) Transition(agentID string, from, to Status, mutate func(*Record)) (bool, error) {
	moved := false
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i := range recs {
			if recs[i].AgentID != agentID || recs[i].Status != from {
				continue
			}
			if mutate != nil {
				mutate(&recs[i])
			}
			ts := q.now().UTC().Format(time.RFC3339)
			recs[i].Status = to
			recs[i].UpdatedAt = ts
			if to == StatusPending {
				recs[i].PendingSince = ts
			}
			moved = true
			return recs, true, nil
		}
		return nil, false, nil
	})
	return moved, err
}

// Remove deletes the agent's record and returns it (nil if absent).
func (q *Queue) Remove(agentID string) (*Record, error) {
	var removed *Record
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i := range recs {
			if recs[i].AgentID == agentID {
				r := recs[i]
				removed = &r
				return append(recs[:i], recs[i+1:]...), true, nil
			}
		}
		return nil, false, nil
	})
	return removed, err
}

// withLock runs fn over the current records under an exclusive flock. fn
// returns (newRecords, write, err); when write is true the queue file is
// atomically replaced. The lock lives on a sidecar .lock file so the rename
// never invalidates a held lock.
func (q *Queue) withLock(fn func([]Record) ([]Record, bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return fmt.Errorf("rescue queue: mkdir: %w", err)
	}
	lock, err := os.OpenFile(q.path+".lock", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("rescue queue: open lock: %w", err)
	}
	defer lock.Close() //nolint:errcheck
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("rescue queue: flock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	var recs []Record
	raw, err := os.ReadFile(q.path)
	switch {
	case os.IsNotExist(err):
		// empty queue
	case err != nil:
		return fmt.Errorf("rescue queue: read: %w", err)
	case len(raw) > 0:
		if err := json.Unmarshal(raw, &recs); err != nil {
			return fmt.Errorf("rescue queue: parse %s: %w", q.path, err)
		}
	}

	next, write, err := fn(recs)
	if err != nil || !write {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("rescue queue: marshal: %w", err)
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("rescue queue: write tmp: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("rescue queue: rename: %w", err)
	}
	return nil
}
