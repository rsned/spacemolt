package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

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
func pollRescues(logger *log.Logger, sup *supervisor.Supervisor, queue *rescue.Queue, histPath, fleetName string, snap []supervisor.WorkerInfo) {
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
		if rec.Fleet == fleetName && rec.Status == rescue.StatusDone {
			archiveRescue(logger, queue, histPath, w.AgentID)
			sup.ReleaseQuarantine(w.AgentID)
			logger.Printf("rescue: %s rescued (+%d fuel by %s); rejoining fleet", w.AgentID, rec.RescueFuel, rec.ClaimedBy)
		}
	}
}

// archiveRescue moves a record out of the queue into the history jsonl.
func archiveRescue(logger *log.Logger, queue *rescue.Queue, histPath, agentID string) {
	rec, err := queue.Remove(agentID)
	if err != nil || rec == nil {
		logger.Printf("rescue: archive %s: rec=%v err=%v", agentID, rec, err)
		return
	}
	line, err := json.Marshal(rec)
	if err != nil {
		logger.Printf("rescue: marshal history %s: %v", agentID, err)
		return
	}
	f, err := os.OpenFile(histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Printf("rescue: open history: %v", err)
		return
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.Write(append(line, '\n')); err != nil {
		logger.Printf("rescue: append history: %v", err)
	}
}
