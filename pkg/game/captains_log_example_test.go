package game_test

import (
	"fmt"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/game"
)

// Example demonstrates basic usage of the captain's log system
func Example_captainsLog() {
	agentID := "example-explorer"
	defer os.RemoveAll("data/agents/" + agentID) // Cleanup

	// Write a captain's log entry
	entry := &game.AgentLog{
		AgentName:   "Captain Example",
		CurrentGoal: "Exploring uncharted systems",
		Location:    "System: frontier-7, POI: asteroid_belt",
		Notes: []string{
			"Found rich mineral deposits",
			"Low fuel - need to return to station soon",
		},
	}

	err := game.WriteCaptainsLog(agentID, entry)
	if err != nil {
		log.Fatal(err)
	}

	// Read the latest log entry
	latest, err := game.ReadLatestCaptainsLog(agentID)
	if err != nil {
		log.Fatal(err)
	}

	if latest != nil {
		fmt.Printf("Agent: %s\n", latest.AgentName)
		fmt.Printf("Goal: %s\n", latest.CurrentGoal)
		fmt.Printf("Location: %s\n", latest.Location)
		fmt.Printf("Notes: %d\n", len(latest.Notes))
	}

	// Output:
	// Agent: Captain Example
	// Goal: Exploring uncharted systems
	// Location: System: frontier-7, POI: asteroid_belt
	// Notes: 2
}

// Example_recovery demonstrates how to recover from a captain's log on startup
func Example_recovery() {
	agentID := "recovery-example"
	defer os.RemoveAll("data/agents/" + agentID) // Cleanup

	// Simulate previous mission
	game.WriteCaptainsLog(agentID, &game.AgentLog{
		AgentName:   "Captain Recovery",
		CurrentGoal: "Mining operations in progress",
		Location:    "System: sol, POI: sol_belt",
	})

	// Agent restarts and recovers state
	log, err := game.ReadLatestCaptainsLog(agentID)
	if err != nil {
		fmt.Println("Error reading log")
		return
	}

	if log != nil {
		fmt.Printf("Resuming: %s\n", log.CurrentGoal)
		fmt.Printf("Last Location: %s\n", log.Location)
	} else {
		fmt.Println("No previous mission found, starting fresh")
	}

	// Output:
	// Resuming: Mining operations in progress
	// Last Location: System: sol, POI: sol_belt
}
