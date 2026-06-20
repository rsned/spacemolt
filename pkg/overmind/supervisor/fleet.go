// Package supervisor runs the overmind control server and worker lifecycle.
package supervisor

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// WorkerInfo is the overmind's view of one worker.
type WorkerInfo struct {
	AgentID    string
	Role       string
	Station    string
	PID        int
	LastStatus control.Status
	LastSeen   time.Time
	Healthy    bool
	Restarts   int
}

// Fleet is the thread-safe in-memory registry of all workers.
type Fleet struct {
	mu      sync.RWMutex
	workers map[string]*WorkerInfo
}

// NewFleet returns an empty Fleet.
func NewFleet() *Fleet {
	return &Fleet{workers: make(map[string]*WorkerInfo)}
}

func (f *Fleet) get(agentID string) *WorkerInfo {
	w := f.workers[agentID]
	if w == nil {
		w = &WorkerInfo{AgentID: agentID}
		f.workers[agentID] = w
	}
	return w
}

// ApplyHello records a worker's identity on connect.
func (f *Fleet) ApplyHello(h control.Hello, pid int, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(h.AgentID)
	w.Role, w.Station, w.PID = h.Role, h.Station, pid
	w.LastSeen, w.Healthy = now, true
}

// ApplyStatus records a heartbeat.
func (f *Fleet) ApplyStatus(agentID string, st control.Status, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.LastStatus, w.LastSeen, w.Healthy = st, now, true
}

// MarkRestart increments the restart counter and marks the worker unhealthy.
func (f *Fleet) MarkRestart(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.Restarts++
	w.Healthy = false
}

// Snapshot returns a copy of all worker infos, sorted by AgentID.
func (f *Fleet) Snapshot() []WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]WorkerInfo, 0, len(f.workers))
	for _, w := range f.workers {
		out = append(out, *w)
	}
	slices.SortFunc(out, func(a, b WorkerInfo) int { return strings.Compare(a.AgentID, b.AgentID) })
	return out
}

// NeedsRestart reports whether a worker has been silent past the timeout.
func NeedsRestart(info WorkerInfo, now time.Time, silence time.Duration) bool {
	return now.Sub(info.LastSeen) > silence
}
