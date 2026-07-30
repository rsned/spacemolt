package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// makeOnQuarantine builds the supervisor callback that files a rescue request
// for a quarantined worker: resolve the in-game username, resolve the system
// id, size the fuel transfer, enqueue. Every enrichment failure degrades
// gracefully (empty username / fallback fuel) — the record must always land.
func makeOnQuarantine(ctx context.Context, logger *log.Logger, queue *rescue.Queue, kb knowledge.Base, fleetName string) func(supervisor.WorkerInfo, string) {
	return func(w supervisor.WorkerInfo, reason string) {
		st := w.LastStatus
		rec := rescue.Record{
			AgentID: w.AgentID, Fleet: fleetName, Reason: reason,
			System: st.System, POI: st.POI, Fuel: st.Fuel, MaxFuel: st.MaxFuel,
			RescueFuel: rescue.FuelFallback,
		}
		if u, err := rescue.ResolveUsername("data/agents", w.AgentID); err != nil {
			logger.Printf("rescue: username for %s: %v (operator must fill target_username)", w.AgentID, err)
		} else {
			rec.TargetUsername = u
		}
		if kb != nil {
			if systems, err := kb.GetSystems(ctx); err == nil {
				if id, ok := rescue.ResolveSystemID(systems, st.System); ok {
					rec.SystemID = id
					if fuel, err := rescue.FuelForSystem(ctx, kb, id); err == nil {
						rec.RescueFuel = fuel
					} else {
						logger.Printf("rescue: fuel sizing for %s: %v (using fallback %d)", w.AgentID, err, rescue.FuelFallback)
					}
				} else {
					logger.Printf("rescue: cannot resolve system %q for %s (using fallback fuel)", st.System, w.AgentID)
				}
			} else {
				logger.Printf("rescue: GetSystems: %v", err)
			}
		} else {
			logger.Printf("rescue: no KB available; cannot resolve system_id for %s — record will need operator attention", w.AgentID)
		}
		if ok, err := queue.Enqueue(rec); err != nil {
			logger.Printf("rescue: enqueue %s: %v", w.AgentID, err)
		} else if ok {
			logger.Printf("rescue: queued %s (%s @ %s/%s, %d fuel)", w.AgentID, reason, rec.System, rec.POI, rec.RescueFuel)
		}
	}
}

// warnedExhausted tracks agents we have already warned about, so a terminal
// record does not reprint on every status tick. Guarded because pollRescues
// and the boot restore can both reach it.
var (
	warnedExhaustedMu sync.Mutex
	warnedExhausted   = map[string]bool{}
)

// warnRescueExhausted prints the operator-action line for a rescue that has
// run out of assisters, once per agent per process. The worker stays
// quarantined and nothing retries, so this log is the only signal that a
// worker is permanently out of the fleet.
func warnRescueExhausted(logger *log.Logger, rec rescue.Record) {
	warnedExhaustedMu.Lock()
	defer warnedExhaustedMu.Unlock()
	if warnedExhausted[rec.AgentID] {
		return
	}
	warnedExhausted[rec.AgentID] = true
	logger.Printf("rescue: ALERT %s is UNRESCUABLE after %d attempts by [%s] and stays quarantined "+
		"— operator action needed. last error: %s. Check the strandee's LIVE position first: a stale "+
		"record POI sends every rescuer to the wrong place.",
		rec.AgentID, rec.Attempts, strings.Join(rec.FailedBy, ","), rec.Error)
}

// restoreQuarantine runs once at boot, before the supervisor launches anyone:
// agents of this fleet with an open rescue record stay quarantined instead of
// launching stranded; done records archive immediately and launch normally.
func restoreQuarantine(logger *log.Logger, fleet *supervisor.Fleet, queue *rescue.Queue, histPath, fleetName string) {
	recs, err := queue.List()
	if err != nil {
		logger.Printf("rescue: boot queue read: %v (launching full roster)", err)
		return
	}
	for _, rec := range recs {
		if rec.Fleet != fleetName {
			continue
		}
		if rec.Status == rescue.StatusDone {
			archiveRescue(logger, queue, histPath, rec.AgentID)
			continue
		}
		fleet.Quarantine(rec.AgentID, rec.Reason)
		logger.Printf("rescue: %s restored to quarantine at boot (%s, status %s)", rec.AgentID, rec.Reason, rec.Status)
	}
}

// pollRescues runs each status tick: any of our quarantined workers whose
// record went done is archived and released for relaunch.
func pollRescues(logger *log.Logger, sup *supervisor.Supervisor, queue *rescue.Queue, histPath, fleetName, agentsDir string, fee int, snap []supervisor.WorkerInfo) {
	quarantined := false
	for _, w := range snap {
		if w.Quarantined {
			quarantined = true
			break
		}
	}
	if !quarantined {
		return
	}
	recs, err := queue.List()
	if err != nil {
		logger.Printf("rescue: queue read: %v", err)
		return
	}
	byAgent := make(map[string]rescue.Record, len(recs))
	for _, rec := range recs {
		byAgent[rec.AgentID] = rec
	}
	for _, w := range snap {
		if !w.Quarantined {
			continue
		}
		rec, ok := byAgent[w.AgentID]
		if !ok {
			// Operator deleted the record: treat as manually resolved.
			logger.Printf("rescue: no record for quarantined %s; releasing", w.AgentID)
			sup.ReleaseQuarantine(w.AgentID)
			continue
		}
		if rec.Fleet == fleetName && rec.Status == rescue.StatusFailed {
			// Terminal: every assister has had a turn (see RescueMaxAttempts).
			// Nothing will retry, and the worker stays quarantined — which is a
			// silent hole unless we say so. Once per process per agent, so the
			// status tick does not spam.
			warnRescueExhausted(logger, rec)
			continue
		}
		if rec.Fleet == fleetName && rec.Status == rescue.StatusDone {
			// Archive first, and only write the fee debt once the record is
			// provably removed from the queue. If archiveRescue fails (e.g. a
			// queue write error), the record stays StatusDone and quarantined
			// and we retry cleanly next tick — writing the debt before archive
			// would double-charge the hauler on such a retry.
			if !archiveRescue(logger, queue, histPath, w.AgentID) {
				continue
			}
			// Reimburse the rescuer, but only when an assister actually spent
			// fuel: tows, operator-manual done-flips, and skip-and-release
			// (RescueFuel 0 / no ClaimedBy) owe nothing.
			if fee > 0 && rec.ClaimedBy != "" && rec.RescueFuel > 0 {
				if recipient, err := rescue.ResolveUsername(agentsDir, rec.ClaimedBy); err != nil {
					logger.Printf("rescue: fee recipient for %s (rescuer %s): %v; skipping fee", w.AgentID, rec.ClaimedBy, err)
				} else if err := rescue.AppendDebt(agentsDir, w.AgentID, rescue.Debt{Recipient: recipient, Credits: fee}); err != nil {
					logger.Printf("rescue: record fee debt for %s: %v", w.AgentID, err)
				} else {
					logger.Printf("rescue: %s owes %d cr fee to %s (%s)", w.AgentID, fee, recipient, rec.ClaimedBy)
				}
			}
			sup.ReleaseQuarantine(w.AgentID)
			logger.Printf("rescue: %s rescued (+%d fuel by %s); rejoining fleet", w.AgentID, rec.RescueFuel, rec.ClaimedBy)
		}
	}
}

// archiveRescue moves a record out of the queue into the history jsonl. It
// returns true only when the record was actually removed from the queue;
// callers gate follow-up side effects (fee debt, quarantine release) on that
// so a queue-write failure retries cleanly instead of double-applying them.
// A history-append failure after a successful removal still returns true — the
// record is already gone from the queue, so the follow-ups are correct.
func archiveRescue(logger *log.Logger, queue *rescue.Queue, histPath, agentID string) bool {
	rec, err := queue.Remove(agentID)
	if err != nil || rec == nil {
		logger.Printf("rescue: archive %s: rec=%v err=%v", agentID, rec, err)
		return false
	}
	line, err := json.Marshal(rec)
	if err != nil {
		logger.Printf("rescue: marshal history %s: %v", agentID, err)
		return true
	}
	f, err := os.OpenFile(histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Printf("rescue: open history: %v", err)
		return true
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.Write(append(line, '\n')); err != nil {
		logger.Printf("rescue: append history: %v", err)
	}
	return true
}
