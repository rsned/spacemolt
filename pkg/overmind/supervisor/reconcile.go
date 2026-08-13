package supervisor

import (
	"fmt"
	"time"
)

// FleetSide is one fleet's half of a secondment: where its overrides sidecar
// lives, how to make it re-read the roster, and how to ask whether an agent's
// worker is currently running under it.
type FleetSide struct {
	// Name is the fleet's short name, matched against Secondment.HomeFleet /
	// AwayFleet.
	Name string
	// OverridesPath is the membership sidecar this fleet reads at boot and on
	// SIGHUP.
	OverridesPath string
	// Reload makes the fleet re-read its roster (in production: SIGHUP its
	// overmind through the control socket).
	Reload func() error
	// Running reports whether this fleet currently has a worker process for
	// agentID. The reconciler will not start an agent in one fleet until the
	// other says it has stopped.
	Running func(agentID string) (bool, error)
}

// ReconcileOptions tunes one reconciliation sweep.
type ReconcileOptions struct {
	// MaxInFlight caps how many agents may be away at once. The home fleet's
	// coverage never drops by more than this, however many workers qualify in
	// the same minute. Zero means one.
	MaxInFlight int
	// StopTimeout bounds the wait for a worker to actually exit its home fleet
	// before the away fleet is allowed to start it. Exceeding it fails the trip
	// rather than risking two live sessions for one agent. Zero means 90s.
	StopTimeout time.Duration
	// PollInterval is how often the stop is re-checked. Zero means 3s.
	PollInterval time.Duration
	// Now supplies the timestamp written into the ledger. Zero value means
	// time.Now (injected for deterministic tests).
	Now func() time.Time
	// Sleep waits between stop polls (injected for deterministic tests).
	Sleep func(time.Duration)
	// Graduated reports whether a seconded agent has finished what it was
	// loaned out for — for the unlock trip, whether it now holds the pirate
	// unlock. Returning true starts the journey home.
	//
	// This lives on the reconciler, not in the worker, so that the whole
	// lifecycle has one owner. A worker that could decide its own return would
	// also need to move itself between fleets, and a worker that can start
	// itself in a fleet is how the same agent ends up running twice.
	//
	// nil leaves seconded agents where they are: an away fleet that never
	// releases anyone is a visible, diagnosable state, whereas a default of
	// "assume done" would yank agents back before they finished.
	Graduated func(agentID string) (bool, error)
}

func (o ReconcileOptions) maxInFlight() int {
	if o.MaxInFlight <= 0 {
		return 1
	}
	return o.MaxInFlight
}

func (o ReconcileOptions) stopTimeout() time.Duration {
	if o.StopTimeout <= 0 {
		return 90 * time.Second
	}
	return o.StopTimeout
}

func (o ReconcileOptions) pollInterval() time.Duration {
	if o.PollInterval <= 0 {
		return 3 * time.Second
	}
	return o.PollInterval
}

func (o ReconcileOptions) now() time.Time {
	if o.Now == nil {
		return time.Now().UTC()
	}
	return o.Now()
}

func (o ReconcileOptions) sleep(d time.Duration) {
	if o.Sleep == nil {
		time.Sleep(d)
		return
	}
	o.Sleep(d)
}

// ReconcileSecondments advances every in-flight secondment by at most one step
// and returns a human-readable line per action taken.
//
// The ordering below is the entire safety argument. An agent started in two
// fleets at once does not merely waste a process: the game server closes the
// older session with status 4001 (session_replaced), so BOTH copies lose their
// connection and the agent stops working entirely. Therefore a move is always
//
//	add to the OLD fleet's removed-set -> reload old -> WAIT for its worker to
//	be gone -> remove from the NEW fleet's removed-set -> reload new
//
// and never the reverse or the two in parallel. If the wait times out the trip
// is marked failed and left alone: a half-applied move that is retried blindly
// is exactly how both halves end up running.
func ReconcileSecondments(ledgerPath string, home, awayFleet FleetSide, opts ReconcileOptions) ([]string, error) {
	led, err := LoadSecondments(ledgerPath)
	if err != nil {
		return nil, err
	}
	var log []string
	// Agents currently absent from their home fleet. A queued nomination costs
	// the home fleet nothing, so only actual absences count against the cap.
	away := led.Away()
	for i := range led.Entries {
		e := &led.Entries[i]
		switch e.Phase {
		case PhaseNominated:
			if away >= opts.maxInFlight() {
				// Someone is already out. Leave this one nominated — it is
				// queued, not dropped, and the next sweep will take it.
				continue
			}
			if err := moveAgent(e.AgentID, home, awayFleet, opts); err != nil {
				e.Phase, e.Note, e.UpdatedAt = PhaseFailed, err.Error(), opts.now().Format(time.RFC3339)
				log = append(log, fmt.Sprintf("%s: secondment FAILED leaving %s: %v", e.AgentID, home.Name, err))
				continue
			}
			e.Phase, e.Note, e.UpdatedAt = PhaseSeconded, "", opts.now().Format(time.RFC3339)
			away++
			log = append(log, fmt.Sprintf("%s: seconded %s -> %s", e.AgentID, home.Name, awayFleet.Name))
		case PhaseSeconded:
			if opts.Graduated == nil {
				continue
			}
			done, err := opts.Graduated(e.AgentID)
			if err != nil {
				// Not knowing is not the same as "not done": leave it seconded
				// and say so. The next sweep asks again.
				log = append(log, fmt.Sprintf("%s: graduation check failed: %v", e.AgentID, err))
				continue
			}
			if !done {
				continue
			}
			e.Phase, e.Note, e.UpdatedAt = PhaseReturning, "graduated", opts.now().Format(time.RFC3339)
			log = append(log, fmt.Sprintf("%s: graduated in %s; queued for return", e.AgentID, awayFleet.Name))
		case PhaseReturning:
			if err := moveAgent(e.AgentID, awayFleet, home, opts); err != nil {
				e.Phase, e.Note, e.UpdatedAt = PhaseFailed, err.Error(), opts.now().Format(time.RFC3339)
				log = append(log, fmt.Sprintf("%s: return FAILED leaving %s: %v", e.AgentID, awayFleet.Name, err))
				continue
			}
			e.Phase, e.Note, e.UpdatedAt = PhaseHome, "", opts.now().Format(time.RFC3339)
			away--
			log = append(log, fmt.Sprintf("%s: returned %s -> %s", e.AgentID, awayFleet.Name, home.Name))
		}
	}
	if len(log) > 0 {
		if err := SaveSecondments(ledgerPath, led); err != nil {
			return log, err
		}
	}
	return log, nil
}

// moveAgent performs the stop-then-start handover between two fleets. It is the
// only place that changes membership, so the ordering guarantee lives here once.
func moveAgent(agentID string, from, to FleetSide, opts ReconcileOptions) error {
	// 1. Remove from the fleet that has it.
	fromOv, err := LoadOverrides(from.OverridesPath)
	if err != nil {
		return fmt.Errorf("read %s overrides: %w", from.Name, err)
	}
	fromOv.Add(agentID)
	if err := SaveOverrides(from.OverridesPath, fromOv); err != nil {
		return fmt.Errorf("write %s overrides: %w", from.Name, err)
	}
	if err := from.Reload(); err != nil {
		return fmt.Errorf("reload %s: %w", from.Name, err)
	}

	// 2. Wait for the worker to actually be gone. This is the step that makes
	//    the handover safe; skipping it is what causes session_replaced.
	if err := waitStopped(agentID, from, opts); err != nil {
		return err
	}

	// 3. Only now let the other fleet have it.
	toOv, err := LoadOverrides(to.OverridesPath)
	if err != nil {
		return fmt.Errorf("read %s overrides: %w", to.Name, err)
	}
	toOv.Delete(agentID)
	if err := SaveOverrides(to.OverridesPath, toOv); err != nil {
		return fmt.Errorf("write %s overrides: %w", to.Name, err)
	}
	if err := to.Reload(); err != nil {
		return fmt.Errorf("reload %s: %w", to.Name, err)
	}
	return nil
}

// waitStopped blocks until from.Running(agentID) is false, or the timeout lapses.
func waitStopped(agentID string, from FleetSide, opts ReconcileOptions) error {
	deadline := opts.now().Add(opts.stopTimeout())
	for {
		running, err := from.Running(agentID)
		if err != nil {
			return fmt.Errorf("check %s worker: %w", from.Name, err)
		}
		if !running {
			return nil
		}
		if !opts.now().Before(deadline) {
			return fmt.Errorf("worker still running in %s after %s; refusing to start it in another fleet", from.Name, opts.stopTimeout())
		}
		opts.sleep(opts.pollInterval())
	}
}
