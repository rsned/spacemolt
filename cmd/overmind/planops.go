package main

import (
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/overmind/plans"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
	"github.com/rsned/spacemolt/pkg/overmind/tasks"
)

// rosterFromFleet extracts the craft-plan pin roster — agent id + home
// station for every craftsman-role worker — from the main fleet's specs,
// preserving fleet-file order. Non-craftsman entries are skipped; an empty
// fleet or one with no craftsman workers returns nil.
func rosterFromFleet(specs []supervisor.WorkerSpec) []plans.RosterAgent {
	var roster []plans.RosterAgent
	for _, w := range specs {
		if w.Role != "craftsman" {
			continue
		}
		roster = append(roster, plans.RosterAgent{AgentID: w.AgentID, Station: w.Station})
	}
	return roster
}

// managedFromFleet builds the plan runner's Managed set (holder agent_id ->
// home station) from a fleet's worker specs, e.g. the marketbot fleet loaded
// via --holders-roster.
func managedFromFleet(specs []supervisor.WorkerSpec) map[string]string {
	managed := make(map[string]string, len(specs))
	for _, w := range specs {
		managed[w.AgentID] = w.Station
	}
	return managed
}

// buildPlanRunner constructs the craft-plan runner when planQueuePath is set,
// or returns nil (the plan runner is entirely optional). roster comes from
// the main fleet's craftsman-role workers; the Managed holder set comes from
// a separate holders-roster fleet file (typically the marketbot fleet) —
// missing/unreadable is fatal here since the operator explicitly asked for
// plan execution with marketbot cooperation. A main fleet with no craftsman
// workers is not fatal: plans needing a craft pin will simply park, per the
// Runner's documented behavior.
func buildPlanRunner(logger *log.Logger, taskStore *tasks.Store, mainSpecs []supervisor.WorkerSpec, planQueuePath, planStateDir, handoffQueuePath, holdersRosterPath string) *plans.Runner {
	if planQueuePath == "" {
		return nil
	}

	roster := rosterFromFleet(mainSpecs)
	if len(roster) == 0 {
		logger.Printf("warning: no craftsman-role workers found; plans needing a craft pin will park")
	}

	holderSpecs, err := supervisor.LoadFleet(holdersRosterPath)
	if err != nil {
		logger.Fatalf("load holders roster %s: %v", holdersRosterPath, err)
	}
	managed := managedFromFleet(holderSpecs)

	if err := os.MkdirAll(planQueuePath, 0o755); err != nil {
		logger.Fatalf("plan queue dir %s: %v", planQueuePath, err)
	}
	if err := os.MkdirAll(planStateDir, 0o755); err != nil {
		logger.Fatalf("plan state dir %s: %v", planStateDir, err)
	}

	logger.Printf("plan runner enabled: queue=%s state=%s roster=%d managed=%d",
		planQueuePath, planStateDir, len(roster), len(managed))

	return &plans.Runner{
		QueueDir: planQueuePath,
		StateDir: planStateDir,
		Store:    taskStore,
		Handoff:  handoff.NewQueue(handoffQueuePath),
		Roster:   roster,
		Managed:  managed,
		Logger:   logger,
	}
}
