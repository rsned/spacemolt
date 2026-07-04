// Package supervisor runs the overmind control server and worker lifecycle.
package supervisor

import (
	"fmt"
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
	// LastProgress is the last time the worker's status showed forward motion
	// (its system, POI, credits, or docked flag changed). LastSeen tracks that a
	// heartbeat arrived at all; LastProgress tracks that the worker is actually
	// doing something. A worker can heartbeat forever while frozen — this is how
	// the stall watchdog tells "alive" from "making progress".
	LastProgress time.Time
	Healthy      bool
	Restarts     int
	// StallRestarts counts consecutive stall-watchdog restarts with no forward
	// progress in between. Progress (ApplyStatus with a changed status) resets
	// it. Reaching StallRestartLimit is the escalation signal for quarantine.
	StallRestarts int
	// Quarantined means the supervisor has pulled this worker from the fleet
	// (stranded — a restart cannot fix it) pending rescue. A quarantined
	// worker's process is stopped and it is never relaunched until
	// ClearQuarantine.
	Quarantined      bool
	QuarantineReason string
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

// ApplyHello records a worker's identity on connect. A fresh (re)connect resets
// the progress clock: the worker just started, so it has not yet had a chance to
// stall.
func (f *Fleet) ApplyHello(h control.Hello, pid int, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(h.AgentID)
	w.Role, w.Station, w.PID = h.Role, h.Station, pid
	w.LastSeen, w.LastProgress, w.Healthy = now, now, true
}

// ApplyStatus records a heartbeat, advancing the progress clock whenever the new
// status shows forward motion relative to the last one.
func (f *Fleet) ApplyStatus(agentID string, st control.Status, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	progressed := statusProgressed(w.LastStatus, st)
	if w.LastProgress.IsZero() || progressed {
		w.LastProgress = now
	}
	if progressed {
		w.StallRestarts = 0
	}
	w.LastStatus, w.LastSeen, w.Healthy = st, now, true
}

// statusProgressed reports whether new shows forward motion relative to old:
// the worker changed system, moved to a different POI, gained/spent credits, or
// docked/undocked. Any of these means it is doing something; none changing over a
// long window (while undocked) is the stall signature.
func statusProgressed(old, new control.Status) bool {
	return old.System != new.System ||
		old.POI != new.POI ||
		old.Credits != new.Credits ||
		old.Docked != new.Docked
}

// MarkRestart increments the restart counter and marks the worker unhealthy.
// It also zeroes LastProgress so the stall watchdog cannot re-fire on the
// fresh process before it reports in (Stalled is disabled on a zero
// LastProgress; ApplyHello restarts the clock).
func (f *Fleet) MarkRestart(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.Restarts++
	w.Healthy = false
	w.LastProgress = time.Time{}
}

// MarkStallRestart records that the stall watchdog is restarting this worker.
func (f *Fleet) MarkStallRestart(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.get(agentID).StallRestarts++
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

// Stalled reports whether a worker is heartbeating but frozen: undocked with no
// forward progress (system/POI/credits/docked unchanged) for longer than
// stallTimeout. This is the "healthy but stuck" state a heartbeat-only check
// cannot see — e.g. a shuttle stranded undocked in a station-less system, retrying
// dock forever (johnny_cab: 2.5 days). The guards keep it from firing on legitimate
// idle postures:
//   - Docked workers are exempt: a resident marketbot parked at its home station
//     between scheduled captures, or a shuttle camping a hub for passengers, is
//     docked and idle by design, not stuck.
//   - Drained workers are exempt: a drain (SIGUSR1) intentionally quiesces them.
//   - A zero LastProgress (never seen) or non-positive stallTimeout disables it.
//
// stallTimeout is deliberately generous so a healthy mobile worker — which moves
// (system/POI change) or trades (credits change) well within the window — is never
// flagged; only a genuinely wedged worker trips it.
func Stalled(info WorkerInfo, now time.Time, stallTimeout time.Duration) bool {
	if stallTimeout <= 0 || info.LastProgress.IsZero() {
		return false
	}
	if info.LastStatus.Docked || info.LastStatus.Drained {
		return false
	}
	return now.Sub(info.LastProgress) > stallTimeout
}

// DrainProgress reports drain quiescence across currently-healthy workers:
// total healthy, how many last reported Drained, and the sorted ids of those
// still busy. A worker that is healthy but has not yet reported a heartbeat
// counts as busy (not drained).
func (f *Fleet) DrainProgress() (idle, total int, busy []string) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, w := range f.workers {
		if !w.Healthy {
			continue
		}
		total++
		if w.LastStatus.Drained {
			idle++
		} else {
			busy = append(busy, w.AgentID)
		}
	}
	slices.Sort(busy)
	return idle, total, busy
}

// Quarantine flags a worker as pulled-from-fleet. Creates the entry if absent
// (the boot-time restore path runs before any Hello).
func (f *Fleet) Quarantine(agentID, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.Quarantined, w.QuarantineReason, w.Healthy = true, reason, false
}

// ClearQuarantine releases a worker for relaunch. Stall state is reset so the
// watchdog gives the rescued worker a fresh window before re-evaluating.
func (f *Fleet) ClearQuarantine(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.Quarantined, w.QuarantineReason = false, ""
	w.StallRestarts = 0
	w.LastProgress = time.Time{}
}

// IsQuarantined reports whether the worker is currently pulled from the fleet.
func (f *Fleet) IsQuarantined(agentID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	w, ok := f.workers[agentID]
	return ok && w.Quarantined
}

// Stranded reports whether a stalled worker is beyond what a restart can fix,
// and why. Two signals (spec 2026-07-03-stranded-worker-quarantine):
//   - fuel-dead: undocked + stalled + fuel below max(fuelFraction×MaxFuel,
//     fuelFloor) — it cannot move, so respawning is futile. Assist-role
//     workers are exempt (they legitimately run their tank down mid-rescue).
//   - escalation: stallRestartLimit consecutive stall-restarts produced no
//     progress — whatever is wrong, restarting is not fixing it.
func Stranded(info WorkerInfo, now time.Time, stallTimeout time.Duration, fuelFraction, fuelFloor float64, stallRestartLimit int) (bool, string) {
	if !Stalled(info, now, stallTimeout) {
		return false, ""
	}
	if stallRestartLimit > 0 && info.StallRestarts >= stallRestartLimit {
		return true, fmt.Sprintf("stalled: %d futile stall-restarts without progress", info.StallRestarts)
	}
	if info.Role == "assist" {
		return false, ""
	}
	st := info.LastStatus
	threshold := fuelFloor
	if frac := fuelFraction * st.MaxFuel; frac > threshold {
		threshold = frac
	}
	if st.Fuel < threshold {
		return true, fmt.Sprintf("fuel-dead: stalled >%s undocked, fuel %.0f/%.0f", stallTimeout, st.Fuel, st.MaxFuel)
	}
	return false, ""
}
