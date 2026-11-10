// Package creditreq is the shared credit-request channel: workers that cannot
// pay for fuel file a request into a flock-guarded JSON queue; the banker pays
// it. A sibling append-only history records filings and payments so the banker
// can enforce per-agent and per-day limits.
package creditreq

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Status is a credit request's lifecycle state.
type Status string

// Lifecycle: pending → paid, or pending → held (banker declined for now).
const (
	StatusPending Status = "pending"
	StatusHeld    Status = "held"
	StatusPaid    Status = "paid"
)

// Record is one agent's request for credits.
type Record struct {
	AgentID     string    `json:"agent_id"`
	Username    string    `json:"username"`
	BaseID      string    `json:"base_id"`
	SystemID    string    `json:"system_id"`
	Reason      string    `json:"reason"`
	HeldReason  string    `json:"held_reason,omitempty"`
	Credits     float64   `json:"credits"`
	Fuel        float64   `json:"fuel"`
	MaxFuel     float64   `json:"max_fuel"`
	Status      Status    `json:"status"`
	RequestedAt time.Time `json:"requested_at"`
	PaidAt      time.Time `json:"paid_at,omitzero"`
	Amount      int64     `json:"amount,omitempty"`
	Attempts    int       `json:"attempts,omitempty"`
}

func (r Record) open() bool { return r.Status == StatusPending || r.Status == StatusHeld }

// Queue is a handle on the shared queue file. Safe for concurrent use across
// processes: every operation takes an exclusive flock on a sidecar lock file,
// then atomically rewrites the queue via temp+rename.
type Queue struct {
	path string
	now  func() time.Time
}

// NewQueue returns a queue handle on path. The file need not exist yet.
func NewQueue(path string) *Queue {
	return &Queue{path: path, now: time.Now}
}

// List returns all records. A missing file is an empty queue.
func (q *Queue) List() ([]Record, error) {
	var out []Record
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		out = recs
		return nil, false, nil
	})
	return out, err
}

// File adds rec as a pending request unless the agent already has an open
// (pending or held) one. A finished (paid) record for the agent is replaced,
// keeping one record per agent. Reports whether a record was filed.
func (q *Queue) File(rec Record) (bool, error) {
	filed := false
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i, r := range recs {
			if r.AgentID != rec.AgentID {
				continue
			}
			if r.open() {
				return nil, false, nil
			}
			recs = append(recs[:i], recs[i+1:]...)
			break
		}
		rec.Status = StatusPending
		if rec.RequestedAt.IsZero() {
			rec.RequestedAt = q.now().UTC()
		}
		filed = true
		return append(recs, rec), true, nil
	})
	return filed, err
}

// Update applies mutate to the agent's record under the lock. It reports
// false when the agent has no record.
func (q *Queue) Update(agentID string, mutate func(*Record)) (bool, error) {
	found := false
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i := range recs {
			if recs[i].AgentID == agentID {
				mutate(&recs[i])
				found = true
				return recs, true, nil
			}
		}
		return nil, false, nil
	})
	return found, err
}

// Remove deletes the agent's record, if any.
func (q *Queue) Remove(agentID string) error {
	return q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i := range recs {
			if recs[i].AgentID == agentID {
				return append(recs[:i], recs[i+1:]...), true, nil
			}
		}
		return nil, false, nil
	})
}

// withLock runs fn over the current records under an exclusive flock. fn
// returns (newRecords, write, err); when write is true the queue file is
// atomically replaced. The lock lives on a sidecar .lock file so the rename
// never invalidates a held lock.
func (q *Queue) withLock(fn func([]Record) ([]Record, bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return fmt.Errorf("creditreq queue: mkdir: %w", err)
	}
	lock, err := os.OpenFile(q.path+".lock", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("creditreq queue: open lock: %w", err)
	}
	defer lock.Close() //nolint:errcheck
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("creditreq queue: flock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	var recs []Record
	raw, err := os.ReadFile(q.path)
	switch {
	case os.IsNotExist(err):
		// empty queue
	case err != nil:
		return fmt.Errorf("creditreq queue: read: %w", err)
	case len(raw) > 0:
		if err = json.Unmarshal(raw, &recs); err != nil {
			return fmt.Errorf("creditreq queue: parse %s: %w", q.path, err)
		}
	}

	next, write, err := fn(recs)
	if err != nil || !write {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("creditreq queue: marshal: %w", err)
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("creditreq queue: write tmp: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("creditreq queue: rename: %w", err)
	}
	return nil
}
