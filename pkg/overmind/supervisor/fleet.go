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
	AgentID string
	Role    string
	Station string
	PID     int
	// Version/Commit/BuiltAt/CodeDirty/Modified are the worker binary's build
	// identity, reported in its Hello (buildinfo.Get). Empty Version = a
	// pre-feature "legacy" worker. Modified is the raw vcs.modified cosmetic
	// flag; CodeDirty is the color-relevant one (uncommitted code outside data/).
	Version    string
	Commit     string
	BuiltAt    string
	CodeDirty  bool
	Modified   bool
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
	// Leaving means a membership-remove is in progress: the worker has been
	// sent a drain and will be stopped and dropped from the fleet once idle
	// (or after the remove-drain timeout). Surfaced to the dashboard as the
	// "draining" chip.
	Leaving bool
	// DisconnectedSince is when the worker first reported its game-server
	// connection down (LastStatus.Disconnected) without a reconnect since; zero
	// when connected. A disconnected worker keeps heartbeating over the control
	// socket, so the silence watchdog cannot see it — but its progress freezes,
	// which would trip the stall watchdog. The supervisor leaves it to the
	// reconnect gate for DisconnectGrace before restarting, because a restart's
	// fresh login cannot succeed during a per-IP block and deepens it.
	DisconnectedSince time.Time
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
	w.Version, w.Commit, w.BuiltAt = h.Version, h.Commit, h.BuiltAt
	w.CodeDirty, w.Modified = h.CodeDirty, h.Modified
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
	// Track the game-connection state so the watchdogs can leave a reconnecting
	// worker to the reconnect gate rather than restarting it into a login storm.
	if st.Disconnected {
		if w.DisconnectedSince.IsZero() {
			w.DisconnectedSince = now
		}
	} else {
		w.DisconnectedSince = time.Time{}
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
//   - Quiesced workers are exempt for the same reason: an operator parked them.
//     They can be parked UNDOCKED (a hauler between runs), so without this the
//     stall detector would restart exactly the workers just taken out of service.
//   - A zero LastProgress (never seen) or non-positive stallTimeout disables it.
//
// stallTimeout is deliberately generous so a healthy mobile worker — which moves
// (system/POI change) or trades (credits change) well within the window — is never
// flagged; only a genuinely wedged worker trips it.
func Stalled(info WorkerInfo, now time.Time, stallTimeout time.Duration) bool {
	if stallTimeout <= 0 || info.LastProgress.IsZero() {
		return false
	}
	if info.LastStatus.Docked || info.LastStatus.Drained || info.LastStatus.Quiesced {
		return false
	}
	return now.Sub(info.LastProgress) > StallTimeoutFor(info, stallTimeout)
}

// MinerStallFactor multiplies StallTimeout for the miner role, giving a
// working miner a 90-minute leash against the default 15.
//
// statusProgressed counts four things as forward motion: system, POI, credits
// and the docked flag. A miner in its working state changes none of them — it
// sits undocked at one resource POI filling its hold, and ore pays nothing
// until it is delivered somewhere else. Cargo is the only thing that moves,
// and cargo is deliberately not a progress signal because the client's
// CargoUsed drifts upward permanently (deposit_items never clears the local
// cargo slice), which would let a cargo-churning worker satisfy the watchdog
// forever.
//
// So mining is the one activity whose success is indistinguishable from being
// frozen, and the watchdog killed it on that resemblance: live 2026-08-23 the
// mining fleet carried 92 restarts where craft and hunt carried 0, miner-4
// dying at 88/100 iterations mid-mine on a ~15-minute cycle. Worse, Stranded
// gates on Stalled, so a working miner that happened to be low on fuel was
// quarantined as fuel-dead — an outcome only a rescue undoes.
//
// A longer leash, not an exemption: a genuinely wedged miner is still caught,
// just after 90 minutes instead of 15. That trade is deliberate. Restarting a
// working miner destroys real progress every 15 minutes; the cost of noticing
// a real wedge late is that it idles longer.
//
// Ideally this would also require the miner to be AT a resource POI, but the
// supervisor cannot tell: control.Status carries the POI id (e.g.
// "cache_mineral_fields", "forgotten_prism") and no POI type, so there is
// nothing to classify against without putting the type on the wire. Role is
// the signal that is actually available. Stalled already exempts docked
// workers, so this only ever widens the window for an undocked miner.
const MinerStallFactor = 6

// StallTimeoutFor returns the stall window that applies to one worker: the
// configured base for every role except miner, which gets MinerStallFactor
// times it. Callers pass the configured StallTimeout and the role adjustment
// happens here, so Stalled and Stranded (which gates on Stalled) cannot drift
// apart.
func StallTimeoutFor(info WorkerInfo, base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	if info.Role == "miner" {
		return base * MinerStallFactor
	}
	return base
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

// MarkLeaving flags a worker as being removed from the fleet (drain sent,
// stop pending). Creates the entry if absent so a booting worker can still
// be marked.
func (f *Fleet) MarkLeaving(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.get(agentID).Leaving = true
}

// ClearLeaving clears the removal-in-progress flag (rolling update: the same
// agent relaunches, so its entry stays and must not keep the draining chip).
func (f *Fleet) ClearLeaving(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w, ok := f.workers[agentID]; ok {
		w.Leaving = false
	}
}

// Remove drops a worker from the registry entirely (membership removal
// complete). Unknown ids are a no-op — Remove must not create entries.
func (f *Fleet) Remove(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.workers, agentID)
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
		// Report the window that actually applied, not the configured base --
		// a miner's is MinerStallFactor times longer and a reason string
		// naming the base would misdescribe why it was quarantined.
		return true, fmt.Sprintf("fuel-dead: stalled >%s undocked, fuel %.0f/%.0f",
			StallTimeoutFor(info, stallTimeout), st.Fuel, st.MaxFuel)
	}
	return false, ""
}
