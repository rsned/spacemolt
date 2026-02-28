package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/skills"
)

func main() {
	var (
		destination   = flag.String("destination", "haven", "Destination system ID")
		destinationPOI = flag.String("poi", "", "Destination POI name (optional)")
		agentID       = flag.String("agent", "miner-1", "Agent ID for route persistence")
		baseDir       = flag.String("data", "data", "Base directory for agent data")
		skillName     = flag.String("skill", "travel", "Skill to run (travel or recall)")
	)
	flag.Parse()

	logger := log.New(os.Stderr, "[test-travel] ", log.LstdFlags)

	// Get credentials from storage
	provider := credentials.NewFileProvider(*baseDir + "/agents")
	ctx := context.Background()

	creds, err := provider.GetCredentials(ctx, *agentID)
	if err != nil {
		logger.Fatalf("Get credentials for %s: %v", *agentID, err)
	}

	gameURL := "wss://game.spacemolt.com/ws"

	registry := skills.NewRegistry()
	if err := registry.LoadFromDir(*baseDir + "/skills"); err != nil {
		logger.Fatalf("Load skills: %v", err)
	}

	client := game.NewClient(gameURL, creds.Username, creds.Password, logger)

	if err := client.Connect(ctx); err != nil {
		logger.Fatalf("Connect: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Close error: %v", err)
		}
	}()

	if err := client.Login(ctx); err != nil {
		logger.Fatalf("Login: %v", err)
	}

	dispatcher := skills.NewClientDispatcher(client, *baseDir, *agentID, logger)
	executor := skills.NewExecutor(registry, dispatcher, logger)

	// Ensure system data is loaded before starting
	dispatcher.EnsureSystemData(ctx)

	params := map[string]string{}
	if *destination != "" {
		params["destination_system"] = *destination
	}
	if *destinationPOI != "" {
		params["destination_poi"] = *destinationPOI
	}

	logger.Printf("Starting %s skill...", *skillName)
	if *destination != "" {
		logger.Printf("Destination: %s", *destination)
		if *destinationPOI != "" {
			logger.Printf("POI: %s", *destinationPOI)
		}
	}

	if err := executor.RunWithParams(ctx, *skillName, params); err != nil {
		logger.Fatalf("Skill failed: %v", err)
	}

	logger.Println("Skill complete!")
	state := client.GetState()
	logger.Printf("Current system: %s", state.System.Name)
	if state.Player.DockedAtBase != "" {
		logger.Printf("Docked at: %s", state.Player.DockedAtBase)
	}
}
