package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/api"
	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
	"github.com/rsned/spacemolt/pkg/registry"
	"github.com/rsned/spacemolt/pkg/tot"
)

// CLI flags
var (
	// Agent selection
	agentsFlag = flag.String("agents", "", "Comma-separated list of agent IDs to start (highest priority)")
	agentsDir  = flag.String("agents-dir", "data/agents", "Directory containing agent personalities")
	configFile = flag.String("config", "agents_config.yaml", "Path to configuration file")

	// Server configuration
	serverURL = flag.String("server-url", "wss://game.spacemolt.com/ws", "Game server WebSocket URL")

	// Knowledge base
	dbBackend = flag.String("db-backend", "sqlite", "Knowledge base backend: sqlite or memory")
	dbPath    = flag.String("db-path", "data/spacemolt-knowledge.db", "Path to SQLite database")

	// LLM configuration
	llmURL   = flag.String("llm-url", "http://localhost:11434", "LLM server URL (Ollama)")
	llmModel = flag.String("llm-model", "qwen3:8b", "LLM model name")

	// Credentials
	credsBackend = flag.String("creds-backend", "file", "Credentials backend: file, sqlite, or keyring")
	credsPath    = flag.String("creds-path", "data/agents", "Path for file-based credentials storage (deprecated: use --agents-dir)")

	// Agent manager settings
	maxAgents        = flag.Int("max-agents", 10, "Maximum number of concurrent agents")
	decisionInterval = flag.Duration("decision-interval", 10*time.Second, "Decision interval for agents")

	// HTTP API settings
	httpPort = flag.Int("http-port", 0, "Enable HTTP API on port (0 = disabled)")

	// Status registry settings
	registryURL = flag.String("registry-url", "", "Status registry URL (e.g., http://localhost:8081)")
)

// AgentsConfig represents the configuration file structure
type AgentsConfig struct {
	Server struct {
		URL string `yaml:"url"`
	} `yaml:"server"`

	Agents struct {
		MaxConcurrent    int           `yaml:"max_concurrent"`
		DecisionInterval time.Duration `yaml:"decision_interval"`
		Enabled          []string      `yaml:"enabled"`
	} `yaml:"agents"`
}

// migrateCredentials migrates credentials from old location to new consolidated location
// Old: data/credentials/AGENT_ID/credentials.json
// New: data/agents/AGENT_ID/credentials.json
func migrateCredentials(oldCredsPath, agentsDir string) error {
	// Skip migration if paths are the same
	if oldCredsPath == agentsDir {
		return nil
	}

	// Check if old credentials directory exists
	oldDir := oldCredsPath

	// Only migrate if oldDir looks like a credentials directory (not an agents directory)
	// We detect this by checking if it's named "credentials" or if user explicitly used it
	if oldDir != "data/credentials" {
		// For non-standard paths, check if the directory exists and has the old structure
		// If it doesn't exist, skip migration silently
		if _, err := os.Stat(oldDir); os.IsNotExist(err) {
			return nil
		}
		// If it exists and is different from agentsDir, proceed with migration
		// (useful for testing and custom configurations)
	}

	// Check if directory exists
	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		// No old credentials to migrate
		return nil
	}

	log.Printf("Checking for credentials to migrate from %s to %s...", oldDir, agentsDir)

	// Read old credentials directory
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return fmt.Errorf("failed to read old credentials directory: %w", err)
	}

	migratedCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		agentID := entry.Name()
		oldCredPath := filepath.Join(oldDir, agentID, "credentials.json")
		newCredPath := filepath.Join(agentsDir, agentID, "credentials.json")

		// Check if old credential file exists
		if _, err := os.Stat(oldCredPath); os.IsNotExist(err) {
			continue
		}

		// Check if new location already has credentials
		if _, err := os.Stat(newCredPath); err == nil {
			log.Printf("  [%s] Credentials already exist at new location, skipping", agentID)
			continue
		}

		// Create new agent directory
		newAgentDir := filepath.Join(agentsDir, agentID)
		if err := os.MkdirAll(newAgentDir, 0755); err != nil {
			log.Printf("  [%s] Failed to create directory: %v", agentID, err)
			continue
		}

		// Read old credentials
		data, err := os.ReadFile(oldCredPath)
		if err != nil {
			log.Printf("  [%s] Failed to read old credentials: %v", agentID, err)
			continue
		}

		// Write to new location
		if err := os.WriteFile(newCredPath, data, 0600); err != nil {
			log.Printf("  [%s] Failed to write new credentials: %v", agentID, err)
			continue
		}

		log.Printf("  ✓ [%s] Migrated credentials to %s", agentID, newCredPath)
		migratedCount++
	}

	if migratedCount > 0 {
		log.Printf("✓ Migrated %d agent credential(s) to new location", migratedCount)
		log.Printf("  Old credentials in %s can be safely deleted", oldDir)
	}

	return nil
}

func main() {
	flag.Parse()

	log.Println("=== Spacemolt Agent Server ===")

	// 1. Determine which agents to load
	agentIDs := getAgentIDs(*agentsFlag, *agentsDir, *configFile)
	if len(agentIDs) == 0 {
		log.Println("❌ No agents found to start")
		log.Println("Options:")
		log.Println("  1. Use --agents flag: --agents=miner-2,explorer-7")
		log.Println("  2. Set SPACEMOLT_AGENTS env: export SPACEMOLT_AGENTS=miner-2,explorer-7")
		log.Println("  3. Create agents_config.yaml with enabled agents list")
		log.Printf("  4. Add agent personalities to %s/\n", *agentsDir)
		os.Exit(1)
	}

	log.Printf("Found %d agent(s) to start: %v\n", len(agentIDs), agentIDs)

	// 2. Initialize shared resources
	ctx := context.Background()

	kb, err := initKnowledgeBase(*dbBackend, *dbPath)
	if err != nil {
		log.Fatalf("❌ Failed to initialize knowledge base: %v", err)
	}
	defer kb.Close() //nolint:errcheck
	log.Printf("✓ Knowledge base initialized (%s)", *dbBackend)

	llmClient, err := initLLMClient(*llmURL, *llmModel)
	if err != nil {
		log.Fatalf("❌ Failed to initialize LLM client: %v", err)
	}
	log.Printf("✓ LLM client initialized (%s @ %s)", *llmModel, *llmURL)
	if llmClient.HasPromptManager() {
		log.Println("✓ Prompt management system enabled")
	} else {
		log.Println("⚠ Prompt management system not available, using fallback prompts")
	}

	// Migrate credentials from old location to new location
	if err := migrateCredentials(*credsPath, *agentsDir); err != nil {
		log.Printf("⚠ Warning: Credential migration had issues: %v", err)
	}

	credsProvider, err := initCredentialsProvider(*credsBackend, *agentsDir, *credsPath)
	if err != nil {
		log.Fatalf("❌ Failed to initialize credentials provider: %v", err)
	}
	log.Printf("✓ Credentials provider initialized (%s)", *credsBackend)

	// 3. Create Manager
	managerConfig := agent.DefaultManagerConfig()
	managerConfig.MaxAgents = *maxAgents
	managerConfig.GameServerURL = *serverURL
	managerConfig.AgentsDataDir = *agentsDir
	managerConfig.RunnerConfig.DecisionInterval = *decisionInterval
	managerConfig.DebugLogger = log.Default()

	mgr := agent.NewManager(kb, llmClient, credsProvider, managerConfig)
	log.Printf("✓ Agent manager created (max agents: %d)", *maxAgents)

	// 4. Spawn agents
	log.Println("\n=== Spawning Agents ===")
	successCount := 0
	failedAgents := []string{}

	for _, agentID := range agentIDs {
		personality, err := loadPersonality(*agentsDir, agentID)
		if err != nil {
			log.Printf("❌ [%s] Failed to load personality: %v", agentID, err)
			failedAgents = append(failedAgents, agentID)
			continue
		}

		log.Printf("[%s] Spawning agent: %s (%s)", agentID, personality.Name, personality.Role)
		runner, err := mgr.SpawnAgentWithGame(ctx, personality)
		if err != nil {
			log.Printf("❌ [%s] Failed to spawn: %v", agentID, err)
			failedAgents = append(failedAgents, agentID)
			continue
		}

		// Wire Tree-of-Thought evaluator if agent has decision_mode="tot"
		if personality.DecisionMode == "tot" {
			totAdapter := &tot.RunnerAdapter{
				Eval: tot.NewEvaluator(llmClient, *llmModel),
			}
			runner.SetToTEvaluator(totAdapter)
			log.Printf("✓ [%s] Thought Engine enabled", agentID)
		}

		log.Printf("✓ [%s] Started successfully (faction: %s)", agentID, personality.Faction)
		successCount++
		_ = runner // runner is now managed by the manager
	}

	// 5. Check if all agents failed
	if successCount == 0 {
		log.Println("\n❌ FATAL: All agents failed to start")
		if len(failedAgents) > 0 {
			log.Printf("Failed agents: %v", failedAgents)
		}
		log.Println("\nPossible causes:")
		log.Println("  - Game server unreachable")
		log.Println("  - Invalid credentials")
		log.Println("  - Network connectivity issues")
		log.Println("  - Personality files missing or invalid")
		log.Println("\nCheck logs above for details")
		os.Exit(1)
	}

	// 6. Log summary
	totalAgents := len(agentIDs)
	log.Println("\n=== Agent Server Started ===")
	log.Printf("✓ Successfully started: %d/%d agents", successCount, totalAgents)
	if len(failedAgents) > 0 {
		log.Printf("⚠ Failed to start: %d agents: %v", len(failedAgents), failedAgents)
	}
	log.Printf("Server URL: %s", *serverURL)
	log.Printf("Decision interval: %v", *decisionInterval)

	// 7. Optionally start HTTP API
	var apiServer *api.Server
	if *httpPort > 0 {
		apiServer = api.NewServer(mgr, *httpPort)
		apiServer.SetLLMClient(llmClient)

		// Wire up event callbacks from runners to stream manager
		streamMgr := apiServer.GetStreamManager()
		for _, runner := range mgr.ListRunners() {
			runner.SetEventCallback(func(agentID string, eventType string, data interface{}) {
				streamMgr.Publish(agentID, api.Event{
					AgentID:   agentID,
					Type:      eventType,
					Timestamp: time.Now(),
					Data:      data,
				})
			})
		}

		// Start HTTP server in background
		go func() {
			log.Printf("✓ HTTP API listening on :%d", *httpPort)
			if err := apiServer.Start(); err != nil {
				log.Printf("HTTP server error: %v", err)
			}
		}()
	}

	// 7.5. Optionally register with status registry
	var regClient *registry.Client
	if *registryURL != "" {
		toolID := "agent-server-main"
		regClient = registry.NewClient(*registryURL, toolID)

		// Prepare capabilities
		capabilities := map[string]interface{}{
			"http_api": fmt.Sprintf("http://localhost:%d", *httpPort),
		}
		if *httpPort == 0 {
			capabilities["http_api"] = nil
		}

		// Register with registry
		reg := registry.ToolRegistration{
			ToolID:       toolID,
			ToolType:     registry.ToolTypeAgentServer,
			PID:          os.Getpid(),
			Status:       "running",
			Capabilities: capabilities,
			Metadata: map[string]interface{}{
				"agents_count":      successCount,
				"total_agents":      totalAgents,
				"failed_agents":     failedAgents,
				"server_url":        *serverURL,
				"decision_interval": *decisionInterval,
			},
		}

		if err := regClient.Register(reg); err != nil {
			log.Printf("⚠ Warning: Failed to register with status registry: %v", err)
			regClient = nil
		} else {
			log.Printf("✓ Registered with status registry: %s", *registryURL)

			// Start heartbeat goroutine
			regClient.StartHeartbeat(ctx, 5*time.Second, func() (status, action string) {
				runners := mgr.ListRunners()
				return "running", fmt.Sprintf("Managing %d agents", len(runners))
			})
		}
	}

	log.Println("\nPress Ctrl+C to stop all agents and exit")

	// 8. Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// 9. Wait for shutdown signal
	<-sigCh
	log.Println("\n\n=== Shutting Down ===")

	// 10. Shutdown HTTP API if running
	if apiServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Warning: HTTP shutdown error: %v", err)
		}
	}

	// 10.5. Deregister from status registry
	if regClient != nil {
		if err := regClient.Deregister(); err != nil {
			log.Printf("Warning: Failed to deregister from status registry: %v", err)
		} else {
			log.Printf("✓ Deregistered from status registry")
		}
	}

	log.Println("Stopping all agents...")

	// 11. Stop all agents
	if err := mgr.StopAll(); err != nil {
		log.Printf("Warning: Error during shutdown: %v", err)
	}

	log.Printf("✓ Stopped %d agents", mgr.AgentCount())
	log.Println("Goodbye!")
}

// getAgentIDs determines which agents to start using priority order:
// 1. CLI flag (highest)
// 2. Environment variable
// 3. Configuration file
// 4. Auto-discover (lowest)
func getAgentIDs(flagValue, agentsDir, configFile string) []string {
	// Priority 1: CLI flag (highest)
	if flagValue != "" {
		log.Printf("Using agents from CLI flag: %s", flagValue)
		return parseAgentList(flagValue)
	}

	// Priority 2: Environment variable
	if envAgents := os.Getenv("SPACEMOLT_AGENTS"); envAgents != "" {
		log.Printf("Using agents from SPACEMOLT_AGENTS env: %s", envAgents)
		return parseAgentList(envAgents)
	}

	// Priority 3: Config file (if exists)
	if ids, err := loadConfigFile(configFile); err == nil && len(ids) > 0 {
		log.Printf("Using agents from config file (%s): %v", configFile, ids)
		return ids
	}

	// Priority 4: Auto-discover all agents in directory
	ids := discoverAgents(agentsDir)
	if len(ids) > 0 {
		log.Printf("Auto-discovered %d agents in %s", len(ids), agentsDir)
	}
	return ids
}

// parseAgentList splits a comma-separated string into agent IDs
func parseAgentList(s string) []string {
	parts := strings.Split(s, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}

// discoverAgents scans the agents directory for valid personalities
func discoverAgents(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Warning: Could not read agents directory %s: %v", dir, err)
		return nil
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			// Check if personality.json exists
			personalityPath := filepath.Join(dir, e.Name(), "personality.json")
			if _, err := os.Stat(personalityPath); err == nil {
				ids = append(ids, e.Name())
			}
		}
	}
	return ids
}

// loadConfigFile loads agent IDs from a YAML configuration file
func loadConfigFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config AgentsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config.Agents.Enabled, nil
}

// loadPersonality loads a personality from the agents directory
func loadPersonality(agentsDir, agentID string) (agent.Personality, error) {
	personalityPath := filepath.Join(agentsDir, agentID, "personality.json")
	return agent.LoadPersonalityJSON(personalityPath)
}

// initKnowledgeBase creates the knowledge base based on backend type
func initKnowledgeBase(backend, dbPath string) (knowledge.Base, error) {
	switch backend {
	case "sqlite":
		return knowledge.NewSQLiteKB(knowledge.Config{
			DBPath:       dbPath,
			WAL:          true,
			MaxOpenConns: 25,
			MaxIdleConns: 5,
			BusyTimeout:  5 * time.Second,
		})
	case "memory":
		return knowledge.NewMemoryKB(), nil
	default:
		return nil, fmt.Errorf("unknown db-backend: %s (use 'sqlite' or 'memory')", backend)
	}
}

// initLLMClient creates the LLM client
func initLLMClient(url, model string) (*llm.Client, error) {
	return llm.New(llm.Config{
		BaseURL:       url,
		Model:         model,
		Timeout:       180 * time.Second, // 3 min to accommodate ToT pipeline
		PromptsDir:    "data/prompts/templates",
		PromptsConfig: "data/prompts/config.yaml",
	})
}

// initCredentialsProvider creates the credentials provider based on backend type
func initCredentialsProvider(backend, agentsDir, credsPath string) (credentials.Provider, error) {
	switch backend {
	case "file":
		// Use agentsDir for file-based credentials (consolidated structure)
		// If user explicitly set --creds-path to something other than default, use that for backward compatibility
		providerPath := agentsDir
		if credsPath != "data/agents" && credsPath != "data/credentials" {
			// User set a custom path, respect it
			providerPath = credsPath
			log.Printf("Warning: Using custom credentials path %s (consider using --agents-dir instead)", credsPath)
		}
		return credentials.NewFileProvider(providerPath), nil
	case "sqlite":
		encryptor, err := credentials.NewEncryptorFromPassphrase([]byte(os.Getenv("SPACEMOLT_PASSPHRASE")))
		if err != nil {
			// If no passphrase, use a default one (not recommended for production)
			log.Println("Warning: Using default passphrase for credentials encryption")
			encryptor, _ = credentials.NewEncryptorFromPassphrase([]byte("default-insecure-passphrase"))
		}
		return credentials.NewSQLiteProvider(credsPath, encryptor)
	case "keyring":
		return credentials.NewKeyringProvider("spacemolt"), nil
	default:
		return nil, fmt.Errorf("unknown creds-backend: %s (use 'file', 'sqlite', or 'keyring')", backend)
	}
}
