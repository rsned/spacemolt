package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
	"github.com/rsned/spacemolt/pkg/strategy"
	"github.com/rsned/spacemolt/pkg/version"
)

// Retry configuration constants
const (
	MaxConnectionRetries = 3
	MaxLoginRetries      = 3
)

// Manager manages multiple agents with game connections
type Manager struct {
	runners         map[string]*Runner
	strategyRunners map[string]*StrategyRunner
	kb              knowledge.Base
	llm             *llm.Client
	credsProvider   credentials.Provider
	mu              sync.RWMutex

	// Configuration
	maxAgents     int
	gameServerURL string
	agentsDataDir string
	runnerConfig  RunnerConfig
	debugLogger   *log.Logger
}

// ManagerConfig holds configuration for the agent manager
type ManagerConfig struct {
	MaxAgents     int
	GameServerURL string
	AgentsDataDir string
	RunnerConfig  RunnerConfig
	DebugLogger   *log.Logger
}

// DefaultManagerConfig returns sensible defaults
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		MaxAgents:     10,
		GameServerURL: "wss://game.spacemolt.com/ws",
		AgentsDataDir: "data/agents",
		RunnerConfig:  DefaultRunnerConfig(),
		DebugLogger:   log.Default(),
	}
}

// NewManager creates a new agent manager
func NewManager(
	kb knowledge.Base,
	llmClient *llm.Client,
	credsProvider credentials.Provider,
	config ManagerConfig,
) *Manager {
	if config.MaxAgents <= 0 {
		config.MaxAgents = 10
	}
	if config.GameServerURL == "" {
		config.GameServerURL = "wss://game.spacemolt.com/ws"
	}
	if config.AgentsDataDir == "" {
		config.AgentsDataDir = "data/agents"
	}
	if config.DebugLogger == nil {
		config.DebugLogger = log.Default()
	}

	return &Manager{
		runners:         make(map[string]*Runner),
		strategyRunners: make(map[string]*StrategyRunner),
		kb:              kb,
		llm:             llmClient,
		credsProvider:   credsProvider,
		maxAgents:       config.MaxAgents,
		gameServerURL:   config.GameServerURL,
		agentsDataDir:   config.AgentsDataDir,
		runnerConfig:    config.RunnerConfig,
		debugLogger:     config.DebugLogger,
	}
}

// NewManagerLegacy creates a manager with legacy signature (for backwards compatibility)
func NewManagerLegacy(
	kb knowledge.Base,
	llmClient *llm.Client,
	credsProvider credentials.Provider,
	maxAgents int,
) *Manager {
	config := DefaultManagerConfig()
	config.MaxAgents = maxAgents
	return NewManager(kb, llmClient, credsProvider, config)
}

// SpawnAgent creates and starts a new agent with game connection
// This is a wrapper around SpawnAgentWithGame for backwards compatibility
func (m *Manager) SpawnAgent(ctx context.Context, personality Personality) (Agent, error) {
	runner, err := m.SpawnAgentWithGame(ctx, personality)
	if err != nil {
		return nil, err
	}
	return runner.GetAgent(), nil
}

// GetRunner retrieves a runner by ID
func (m *Manager) GetRunner(id string) (*Runner, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, ok := m.runners[id]
	return runner, ok
}

// GetAgent retrieves an agent by ID
func (m *Manager) GetAgent(id string) (Agent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, ok := m.runners[id]
	if !ok {
		return nil, false
	}
	return runner.GetAgent(), true
}

// ListRunners returns all runners
func (m *Manager) ListRunners() []*Runner {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runners := make([]*Runner, 0, len(m.runners))
	for _, runner := range m.runners {
		runners = append(runners, runner)
	}
	return runners
}

// ListAgents returns all agents
func (m *Manager) ListAgents() []Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]Agent, 0, len(m.runners))
	for _, runner := range m.runners {
		agents = append(agents, runner.GetAgent())
	}
	return agents
}

// StopAgent stops and removes an agent
func (m *Manager) StopAgent(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	runner, ok := m.runners[id]
	if !ok {
		return fmt.Errorf("agent not found: %s", id)
	}

	if err := runner.Stop(); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	// Close game client
	if err := runner.GetGameClient().Close(); err != nil {
		m.debugLogger.Printf("[%s] Warning: failed to close game client: %v", id, err)
	}

	delete(m.runners, id)
	return nil
}

// StopAll stops all agents
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for id, runner := range m.runners {
		if err := runner.Stop(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to stop agent %s: %w", id, err)
		}
		// Close game client
		if err := runner.GetGameClient().Close(); err != nil {
			m.debugLogger.Printf("[%s] Warning: failed to close game client: %v", id, err)
		}
	}

	m.runners = make(map[string]*Runner)

	// Stop all strategy runners
	for id, sr := range m.strategyRunners {
		if err := sr.Stop(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to stop strategy agent %s: %w", id, err)
		}
		if err := sr.GameClient().Close(); err != nil {
			m.debugLogger.Printf("[%s] Warning: failed to close game client: %v", id, err)
		}
	}
	m.strategyRunners = make(map[string]*StrategyRunner)

	return firstErr
}

// AgentCount returns the number of active agents (LLM + strategy)
func (m *Manager) AgentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.runners) + len(m.strategyRunners)
}

// GetStatus returns the status of all agents
func (m *Manager) GetStatus() map[string]Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make(map[string]Status, len(m.runners))
	for id, runner := range m.runners {
		statuses[id] = runner.GetAgent().Status()
	}
	return statuses
}

// GetCredentials retrieves credentials for an agent
func (m *Manager) GetCredentials(ctx context.Context, agentID string) (*credentials.Credentials, error) {
	return m.credsProvider.GetCredentials(ctx, agentID)
}

// SpawnAgentWithGame creates an agent with game connection and starts it
func (m *Manager) SpawnAgentWithGame(ctx context.Context, personality Personality) (*Runner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.runners) >= m.maxAgents {
		return nil, fmt.Errorf("max agents limit reached: %d", m.maxAgents)
	}

	if _, exists := m.runners[personality.ID]; exists {
		return nil, fmt.Errorf("agent %s already exists", personality.ID)
	}

	m.debugLogger.Printf("[%s] Spawning agent with game connection", personality.ID)

	// Try to load credentials (provider first, then fallback)
	creds, err := m.loadCredentialsWithFallback(ctx, personality.ID)
	hasCredentials := err == nil

	// Create game client
	username := personality.ID
	password := ""
	if hasCredentials {
		username = creds.Username
		password = creds.Password
	}

	gameClient := game.NewClient(m.gameServerURL, username, password, m.debugLogger)

	// Set up automatic reconnection handler
	// Note: We pass nil as the wrapped handler since we don't need additional handling
	reconnectHandler := game.NewReconnectingHandler(gameClient, nil, ctx, m.debugLogger)
	gameClient.SetHandler(reconnectHandler)

	// Connect to game server with retries
	if err := m.connectWithRetry(ctx, gameClient, personality.ID); err != nil {
		return nil, fmt.Errorf("failed to connect to game server: %w", err)
	}

	// Check server version compatibility
	if err := m.checkServerVersion(gameClient, personality.ID); err != nil {
		_ = gameClient.Close()
		return nil, fmt.Errorf("server version check failed: %w", err)
	}

	// Authenticate (register or login)
	if hasCredentials {
		m.debugLogger.Printf("[%s] Logging in with existing credentials", personality.ID)
		if err := m.loginWithRetry(ctx, gameClient, creds, personality.ID); err != nil {
			return nil, fmt.Errorf("login failed: %w", err)
		}
	} else {
		m.debugLogger.Printf("[%s] Registering new agent", personality.ID)
		if err := m.registerAgent(ctx, gameClient, personality); err != nil {
			return nil, fmt.Errorf("registration failed: %w", err)
		}
	}

	// Create agent memory
	memory := NewKBMemory(m.kb, personality.ID)

	// Create agent
	agent := NewBaseAgent(
		personality.ID,
		personality,
		memory,
		m.llm,
	)

	// Register agent in knowledge base
	if err := m.kb.RegisterAgent(
		ctx,
		personality.ID,
		personality.Name,
		personality.Role,
		personality.Faction,
		nil,
	); err != nil {
		m.debugLogger.Printf("[%s] Warning: failed to register in KB: %v", personality.ID, err)
	}

	// Create runner
	runner := NewRunner(agent, gameClient, m.runnerConfig)

	// Start runner
	if err := runner.Start(ctx); err != nil {
		_ = gameClient.Close()
		return nil, fmt.Errorf("failed to start runner: %w", err)
	}

	m.runners[personality.ID] = runner
	m.debugLogger.Printf("[%s] ✓ Agent spawned and running", personality.ID)

	return runner, nil
}

// connectWithRetry attempts to establish WebSocket connection with retries
func (m *Manager) connectWithRetry(ctx context.Context, client *game.Client, agentID string) error {
	var lastErr error

	for attempt := 1; attempt <= MaxConnectionRetries; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(1<<uint(attempt-1)) * 2 * time.Second
			m.debugLogger.Printf("[%s] Connection retry %d/%d after %v",
				agentID, attempt, MaxConnectionRetries, backoff)
			time.Sleep(backoff)
		}

		if err := client.Connect(ctx); err != nil {
			lastErr = err
			m.debugLogger.Printf("[%s] Connection attempt %d failed: %v",
				agentID, attempt, err)
			continue
		}

		// Wait for ready signal
		select {
		case <-client.Ready():
			m.debugLogger.Printf("[%s] ✓ Connected successfully", agentID)
			return nil
		case <-time.After(5 * time.Second):
			lastErr = fmt.Errorf("connection ready timeout")
			m.debugLogger.Printf("[%s] Connection ready timeout", agentID)
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", MaxConnectionRetries, lastErr)
}

// checkServerVersion validates server version against documented API version
func (m *Manager) checkServerVersion(client *game.Client, agentID string) error {
	state := client.GetState()
	if state.ServerVersion == "" {
		m.debugLogger.Printf("[%s] Warning: No server version in welcome message, skipping version check", agentID)
		return nil
	}

	check, err := version.CheckVersion(state.ServerVersion)
	if err != nil {
		m.debugLogger.Printf("[%s] Warning: Version check failed: %v", agentID, err)
		// Don't fail on version check errors, just log them
		return nil
	}

	// Log version info
	m.debugLogger.Printf("[%s] Server version: %s, API docs version: %s",
		agentID, check.ServerVersion, check.ExpectedVersion)

	// Handle major version mismatch (error)
	if check.MajorMismatch {
		m.debugLogger.Printf("[%s] %s", agentID, check.ErrorMessage)
		fmt.Fprintf(os.Stderr, "\n[%s] VERSION MISMATCH ERROR:\n%s\n\n", agentID, check.ErrorMessage)
		return fmt.Errorf("major version mismatch: server %s vs expected %s",
			check.ServerVersion, check.ExpectedVersion)
	}

	// Handle minor version difference (warning)
	if check.WarningMessage != "" {
		banner := check.Banner()
		m.debugLogger.Printf("[%s] %s", agentID, banner)
		fmt.Fprint(os.Stderr, banner)
	}

	return nil
}

// loginWithRetry attempts to authenticate with retries
func (m *Manager) loginWithRetry(ctx context.Context, client *game.Client, creds *credentials.Credentials, agentID string) error {
	var lastErr error

	for attempt := 1; attempt <= MaxLoginRetries; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(1<<uint(attempt-1)) * 2 * time.Second
			m.debugLogger.Printf("[%s] Login retry %d/%d after %v",
				agentID, attempt, MaxLoginRetries, backoff)
			time.Sleep(backoff)
		}

		if err := client.Login(ctx); err != nil {
			lastErr = err
			m.debugLogger.Printf("[%s] Login attempt %d failed: %v",
				agentID, attempt, err)
			continue
		}

		m.debugLogger.Printf("[%s] ✓ Logged in successfully", agentID)
		return nil
	}

	return fmt.Errorf("login failed after %d attempts: %w", MaxLoginRetries, lastErr)
}

// registerAgent registers a new agent and saves credentials
func (m *Manager) registerAgent(ctx context.Context, client *game.Client, personality Personality) error {
	// Generate username from personality
	username := sanitizeUsername(personality.Name)

	// Register with game server
	// Note: As of v0.41+, all five empires are allowed for new registrations
	// TODO: Change this to random(solarian, voidborn, crimson, outerrim, nebula)
	empire := "solarian"
	m.debugLogger.Printf("[%s] Registering with empire: %s (personality faction: %s)",
		personality.ID, empire, personality.Faction)

	if err := client.Register(ctx, empire, ""); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Get the password from state
	state := client.GetState()
	if state.Password == "" {
		return fmt.Errorf("no password received after registration")
	}

	// Save credentials
	creds := &credentials.Credentials{
		Username: username,
		Password: state.Password,
		Empire:   empire, // Store the actual game empire, not personality faction
	}

	if err := m.saveCredentialsWithFallback(ctx, personality.ID, creds); err != nil {
		m.debugLogger.Printf("[%s] Warning: failed to save credentials: %v", personality.ID, err)
	}

	m.debugLogger.Printf("[%s] ✓ Registered as %s (empire: %s)", personality.ID, username, empire)
	return nil
}

// saveCredentialsWithFallback tries provider first, then falls back to file
func (m *Manager) saveCredentialsWithFallback(ctx context.Context, agentID string, creds *credentials.Credentials) error {
	// Try primary provider
	if err := m.credsProvider.StoreCredentials(ctx, agentID, creds); err == nil {
		m.debugLogger.Printf("[%s] ✓ Saved credentials via provider", agentID)
		return nil
	} else {
		m.debugLogger.Printf("[%s] Provider storage failed: %v, trying fallback", agentID, err)
	}

	// Fallback: save to agent directory
	agentDir := filepath.Join(m.agentsDataDir, agentID)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	credsPath := filepath.Join(agentDir, "credentials.json")
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	if err := os.WriteFile(credsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	m.debugLogger.Printf("[%s] ✓ Saved credentials to %s", agentID, credsPath)
	return nil
}

// loadCredentialsWithFallback tries provider first, then checks agent directory
func (m *Manager) loadCredentialsWithFallback(ctx context.Context, agentID string) (*credentials.Credentials, error) {
	// Try primary provider
	creds, err := m.credsProvider.GetCredentials(ctx, agentID)
	if err == nil {
		return creds, nil
	}

	if !credentials.IsErrCredentialsNotFound(err) {
		m.debugLogger.Printf("[%s] Provider error: %v, trying fallback", agentID, err)
	}

	// Fallback: check agent directory
	credsPath := filepath.Join(m.agentsDataDir, agentID, "credentials.json")
	data, err := os.ReadFile(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, credentials.ErrCredentialsNotFound
		}
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	var fallbackCreds credentials.Credentials
	if err := json.Unmarshal(data, &fallbackCreds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials file: %w", err)
	}

	m.debugLogger.Printf("[%s] ✓ Loaded credentials from %s", agentID, credsPath)
	return &fallbackCreds, nil
}

// SpawnStrategyAgent creates a game-connected agent running a strategy instead of LLM.
func (m *Manager) SpawnStrategyAgent(ctx context.Context, agentID string, strat strategy.Strategy, params map[string]string) (*StrategyRunner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	totalAgents := len(m.runners) + len(m.strategyRunners)
	if totalAgents >= m.maxAgents {
		return nil, fmt.Errorf("max agents limit reached: %d", m.maxAgents)
	}

	if _, exists := m.runners[agentID]; exists {
		return nil, fmt.Errorf("agent %s already exists as LLM runner", agentID)
	}
	if _, exists := m.strategyRunners[agentID]; exists {
		return nil, fmt.Errorf("agent %s already exists as strategy runner", agentID)
	}

	m.debugLogger.Printf("[%s] spawning strategy agent (%s)", agentID, strat.Name())

	// Load credentials
	creds, err := m.loadCredentialsWithFallback(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("loading credentials for %s: %w", agentID, err)
	}

	// Create and connect game client
	gameClient := game.NewClient(m.gameServerURL, creds.Username, creds.Password, m.debugLogger)
	reconnectHandler := game.NewReconnectingHandler(gameClient, nil, ctx, m.debugLogger)
	gameClient.SetHandler(reconnectHandler)

	if err := m.connectWithRetry(ctx, gameClient, agentID); err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	if err := m.loginWithRetry(ctx, gameClient, creds, agentID); err != nil {
		_ = gameClient.Close()
		return nil, fmt.Errorf("login failed: %w", err)
	}

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:    agentID,
		Strategy:   strat,
		GameClient: gameClient,
		KB:         m.kb,
		Parameters: params,
		Logger:     m.debugLogger,
	})

	if err := sr.Start(ctx); err != nil {
		_ = gameClient.Close()
		return nil, fmt.Errorf("failed to start strategy runner: %w", err)
	}

	m.strategyRunners[agentID] = sr
	m.debugLogger.Printf("[%s] strategy agent (%s) spawned and running", agentID, strat.Name())
	return sr, nil
}

// SwapStrategy stops the current strategy runner for an agent and starts a new one.
func (m *Manager) SwapStrategy(ctx context.Context, agentID string, newStrat strategy.Strategy, params map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sr, exists := m.strategyRunners[agentID]
	if !exists {
		return fmt.Errorf("no strategy runner for agent %s", agentID)
	}

	// Stop the old strategy
	if err := sr.Stop(); err != nil {
		m.debugLogger.Printf("[%s] warning: error stopping old strategy: %v", agentID, err)
	}

	// Create and start new strategy runner reusing the same game client
	newSR := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:    agentID,
		Strategy:   newStrat,
		GameClient: sr.GameClient(),
		KB:         m.kb,
		Parameters: params,
		Logger:     m.debugLogger,
	})

	if err := newSR.Start(ctx); err != nil {
		return fmt.Errorf("failed to start new strategy: %w", err)
	}

	m.strategyRunners[agentID] = newSR
	m.debugLogger.Printf("[%s] swapped strategy to %s", agentID, newStrat.Name())
	return nil
}

// GetStrategyRunner retrieves a strategy runner by agent ID.
func (m *Manager) GetStrategyRunner(id string) (*StrategyRunner, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sr, ok := m.strategyRunners[id]
	return sr, ok
}

// ListStrategyRunners returns all strategy runners.
func (m *Manager) ListStrategyRunners() []*StrategyRunner {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runners := make([]*StrategyRunner, 0, len(m.strategyRunners))
	for _, sr := range m.strategyRunners {
		runners = append(runners, sr)
	}
	return runners
}

// StopStrategyAgent stops and removes a strategy agent.
func (m *Manager) StopStrategyAgent(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sr, ok := m.strategyRunners[id]
	if !ok {
		return fmt.Errorf("strategy agent not found: %s", id)
	}

	if err := sr.Stop(); err != nil {
		return fmt.Errorf("failed to stop strategy agent: %w", err)
	}

	if err := sr.GameClient().Close(); err != nil {
		m.debugLogger.Printf("[%s] Warning: failed to close game client: %v", id, err)
	}

	delete(m.strategyRunners, id)
	return nil
}

// sanitizeUsername removes invalid characters from username
func sanitizeUsername(s string) string {
	// Replace spaces and special chars with underscores
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, s)

	// Limit length
	if len(s) > 32 {
		s = s[:32]
	}

	return s
}
