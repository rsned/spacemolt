// Package handoff is the crafting-brain executor's stock-handoff channel: a
// flock-guarded JSON queue file. The executor enqueues "holder gifts stock to
// recipient" instructions as it walks a craft plan; workers claim and
// complete the handoffs via send_gift. Mirrors pkg/rescue/queue.go's
// withLock mechanics (flock LOCK_EX sidecar, read-mutate-atomic-rename).
package handoff

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Status is a handoff record's lifecycle state.
type Status string

const (
	StatusPending Status = "pending"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Record is one "holder gifts stock to recipient" instruction.
type Record struct {
	ID          string `json:"id"`      // "<plan-id>/<node-id>" — unique per node
	Holder      string `json:"holder"`  // agent_id that owns the stock
	Station     string `json:"station"` // base_id where the stock sits
	ItemID      string `json:"item_id"`
	Qty         int    `json:"qty"`
	Recipient   string `json:"recipient"` // agent_id to send_gift to
	Status      Status `json:"status"`
	MovedQty    int    `json:"moved_qty"` // actually transferred (may be < Qty)
	Error       string `json:"error,omitempty"`
	RequestedAt string `json:"requested_at"`
	UpdatedAt   string `json:"updated_at"`
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

// Enqueue appends rec (status pending, timestamps stamped) unless a record
// with the same ID already exists — one record per plan node, so re-planning
// while a handoff is in flight is a no-op.
func (q *Queue) Enqueue(rec Record) (bool, error) {
	inserted := false
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for _, r := range recs {
			if r.ID == rec.ID {
				return nil, false, nil
			}
		}
		ts := q.now().UTC().Format(time.RFC3339)
		rec.Status = StatusPending
		rec.RequestedAt, rec.UpdatedAt = ts, ts
		inserted = true
		return append(recs, rec), true, nil
	})
	return inserted, err
}

// Transition moves the record with the given ID from → to (compare-and-set;
// false when the record is absent or not in from), applying mutate to the
// record first.
func (q *Queue) Transition(id string, from, to Status, mutate func(*Record)) (bool, error) {
	moved := false
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i := range recs {
			if recs[i].ID != id || recs[i].Status != from {
				continue
			}
			if mutate != nil {
				mutate(&recs[i])
			}
			recs[i].Status = to
			recs[i].UpdatedAt = q.now().UTC().Format(time.RFC3339)
			moved = true
			return recs, true, nil
		}
		return nil, false, nil
	})
	return moved, err
}

// Remove deletes the record with the given ID and returns it (nil if absent).
func (q *Queue) Remove(id string) (*Record, error) {
	var removed *Record
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i := range recs {
			if recs[i].ID == id {
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
		return fmt.Errorf("handoff queue: mkdir: %w", err)
	}
	lock, err := os.OpenFile(q.path+".lock", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("handoff queue: open lock: %w", err)
	}
	defer lock.Close() //nolint:errcheck
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("handoff queue: flock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	var recs []Record
	raw, err := os.ReadFile(q.path)
	switch {
	case os.IsNotExist(err):
		// empty queue
	case err != nil:
		return fmt.Errorf("handoff queue: read: %w", err)
	case len(raw) > 0:
		if err := json.Unmarshal(raw, &recs); err != nil {
			return fmt.Errorf("handoff queue: parse %s: %w", q.path, err)
		}
	}

	next, write, err := fn(recs)
	if err != nil || !write {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("handoff queue: marshal: %w", err)
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("handoff queue: write tmp: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("handoff queue: rename: %w", err)
	}
	return nil
}
