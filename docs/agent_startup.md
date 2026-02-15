# Agent Server Startup and Multi-Agent Management Plan

## Current Status

**Progress**: Phases 1-5 Complete ✅ | Phase 6 Planned ⏳

- ✅ **Phase 1**: GameClient - WebSocket connection, auth, state management (33 tests)
- ✅ **Phase 2**: AgentRunner - Play loop with tick awareness (12 tests)
- ✅ **Phase 3**: Enhanced Manager - Game connections, registration/login, credential fallback (12 tests)
- ✅ **Phase 4**: Agent Server - Main entry point with config, discovery, error handling (10 tests)
- ✅ **Phase 5**: Watcher Enhancement - Multi-agent TUI with state isolation and cycling
- ⏳ **Phase 6**: Polish - Monitoring, metrics, recovery (Planned)

**Total Tests**: 67 passing ✓

## Overview

This document describes the architecture for starting and managing multiple autonomous game-playing agents in Spacemolt. Each agent will have its own game connection, personality, memory, and decision-making loop powered by LLM.

## Current State Analysis

### Existing Components

1. **Agent Interface** (`pkg/agent/agent.go`)
   - Defines agent behavior: Decide, Learn, Start, Stop
   - Currently has no game connection logic

2. **Agent Manager** (`pkg/agent/manager.go`)
   - Spawns agents with personality and memory
   - Validates credentials exist
   - Tracks active agents
   - Missing: actual game connection and play loop

3. **Personality System** (`pkg/agent/personality_json.go`)
   - Loads agent personalities from JSON files
   - Example personalities in `data/agents/*/personality.json`

4. **Credentials System** (`pkg/credentials/`)
   - Multiple providers: file, SQLite, keyring, env
   - Stores username/token/empire per agent
   - Missing: auto-save on registration

5. **Game Protocol** (`internal/protocol/messages.go`)
   - Defines message types for server communication
   - Registration flow: `TypeRegistered`
   - Login flow: `TypeLoggedIn`

6. **Watcher** (`cmd/watcher/main.go`)
   - Single-agent TUI with WebSocket connection
   - Example of registration/login flow
   - Not designed for multi-agent scenarios

### What's Missing

1. **Game Client per Agent**: Each agent needs its own WebSocket connection
2. **Authentication Flow**: Register new agents or login existing ones
3. **Play Loop**: Continuous decide → act → learn cycle
4. **Agent Server**: Main entry point to orchestrate everything
5. **State Management**: Each agent tracks its own game state
6. **Action Execution**: Convert agent decisions to game commands
7. **Credential Persistence**: Save tokens after registration

## Architecture Design

### High-Level Flow

```
┌─────────────────────────────────────────────────────────────┐
│                      Agent Server                           │
│  (cmd/agent-server/main.go)                                │
├─────────────────────────────────────────────────────────────┤
│ 1. Parse config (CLI args, config file, or defaults)       │
│ 2. Initialize shared resources:                            │
│    - Knowledge Base (SQLite/memory)                        │
│    - LLM Client (Ollama)                                   │
│    - Credentials Provider (file/sqlite/keyring)           │
│ 3. Create Agent Manager                                     │
│ 4. For each selected agent:                                │
│    a. Load personality from data/agents/<id>/              │
│    b. Check credentials:                                    │
│       - If exists: Login flow                              │
│       - If not: Registration flow                          │
│    c. Create GameClient with WebSocket                     │
│    d. Spawn agent with GameClient                          │
│    e. Start agent play loop                                │
│ 5. Monitor agents, handle restarts                         │
│ 6. Graceful shutdown on signal                             │
└─────────────────────────────────────────────────────────────┘
```

### New Components

#### 1. GameClient (`pkg/game/client.go`)

Manages WebSocket connection and game state for a single agent.

```go
type Client struct {
    agentID   string
    ws        *websocket.Conn
    state     *State
    stateCh   chan *State        // State updates
    responseCh chan Response      // Raw server messages
    errorCh   chan error
    stopCh    chan struct{}
    mu        sync.RWMutex
}

// Methods:
func NewClient(agentID, serverURL string) *Client
func (c *Client) Connect(ctx context.Context) error
func (c *Client) Register(ctx context.Context, username, empire string) (*Credentials, error)
func (c *Client) Login(ctx context.Context, username, token string) error
func (c *Client) SendAction(ctx context.Context, action string, params map[string]any) error
func (c *Client) State() *State
func (c *Client) WaitForLogin(timeout time.Duration) error
func (c *Client) Close() error
```

**Responsibilities:**
- Maintain WebSocket connection
- Handle registration/login authentication
- Parse server responses and update state
- Send action commands
- Provide state snapshots for agent decisions
- Reconnect on connection loss

#### 2. AgentRunner (`pkg/agent/runner.go`)

Wraps an agent with its game client and runs the play loop.

```go
type Runner struct {
    agent      Agent
    gameClient *game.Client
    llm        *llm.Client

    // Configuration
    decisionInterval time.Duration  // e.g., 5 seconds
    maxRetries       int

    // Channels
    stopCh    chan struct{}
    errorCh   chan error
}

// Methods:
func NewRunner(agent Agent, gameClient *game.Client, cfg RunnerConfig) *Runner
func (r *Runner) Start(ctx context.Context) error
func (r *Runner) Stop() error
func (r *Runner) Run(ctx context.Context) error  // Main loop
```

**Play Loop Logic:**
```go
func (r *Runner) Run(ctx context.Context) error {
    // Track last action tick to respect 10-second game tick limit
    var lastActionTick int64

    // Decision timer - can check more frequently than actions
    ticker := time.NewTicker(r.decisionInterval)  // e.g., 3 seconds
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-r.stopCh:
            return nil
        case <-ticker.C:
            // 1. Get current game state
            state := r.gameClient.State()
            currentTick := state.GetTick()

            // 2. Check if we can take an action this tick
            canAct := currentTick > lastActionTick

            // 3. Agent makes decision (with context about whether it can act)
            decision, err := r.agent.Decide(ctx, state)
            if err != nil {
                log.Printf("[%s] Decision error: %v", r.agent.ID(), err)
                continue
            }

            // 4. Execute action only if:
            //    - Decision is an action command (not query)
            //    - We haven't acted this tick yet
            isActionCommand := isActionCommand(decision.Action)

            if isActionCommand && !canAct {
                log.Printf("[%s] Throttled: waiting for next tick (current: %d)",
                    r.agent.ID(), currentTick)
                continue
            }

            err = r.gameClient.SendAction(ctx, decision.Action, map[string]any{
                "target": decision.Target,
            })

            // Update last action tick if this was an action command AND it succeeded
            // IMPORTANT: Failed actions do NOT count against rate limit
            if isActionCommand {
                // Wait for server response to determine success
                success := r.gameClient.WaitForActionResult(ctx, 2*time.Second)
                if success {
                    lastActionTick = currentTick
                } else {
                    log.Printf("[%s] Action failed, can retry this tick", r.agent.ID())
                }
            }

            // 5. Learn from result
            result := ActionResult{
                Success: err == nil,
                NewState: r.gameClient.State(),
                Error: err,
                Message: fmt.Sprintf("%s: %s", decision.Action, decision.Reasoning),
            }
            r.agent.Learn(result)
        }
    }
}

// isActionCommand returns true for commands that consume the action tick
// SOURCE: https://www.spacemolt.com/api (updated frequently - check regularly!)
// IMPORTANT: Failed actions do NOT count against rate limit
func isActionCommand(action string) bool {
    // Query/info commands that do NOT consume ticks
    queryCommands := map[string]bool{
        // Player & Ship Info
        "get_status":     true,
        "get_ship":       true,
        "get_skills":     true,
        // World Info
        "get_system":     true,
        "get_poi":        true,
        "get_base":       true,
        "get_map":        true,
        "get_version":    true,
        "get_base_cost":  true,
        // Market & Trading
        "get_listings":   true,
        "get_trades":     true,
        "get_wrecks":     true,
        "get_base_wrecks": true,
        "get_recipes":    true,
        "get_notes":      true,
        // Raid Info
        "raid_status":    true,
        // Forum Browsing
        "forum_list":     true,
        "forum_get_thread": true,
        // Help
        "help":           true,
        // Auth (special case - not rate limited but not gameplay)
        "register":       true,
        "login":          true,
        "logout":         true,
    }

    // If it's a query command, it does NOT consume action tick
    if queryCommands[action] {
        return false
    }

    // Everything else is an action command (consumes tick)
    // Including: travel, jump, dock, undock, attack, scan, cloak, mine,
    // buy, sell, list_item, cancel_list, buy_listing, trade_offer,
    // trade_accept, trade_decline, trade_cancel, loot_wreck, salvage_wreck,
    // loot_base_wreck, salvage_base_wreck, buy_ship, install_mod, uninstall_mod,
    // refuel, repair, craft, chat, create_faction, join_faction, leave_faction,
    // faction_invite, faction_kick, faction_promote, buy_insurance,
    // claim_insurance, set_home_base, set_status, set_colors, set_anonymous,
    // build_base, attack_base, create_map, use_map, create_note, write_note,
    // forum_create_thread, forum_reply, forum_upvote, forum_delete_thread,
    // forum_delete_reply
    return true
}
```

#### 3. Enhanced Manager (`pkg/agent/manager.go`)

Update existing Manager to manage AgentRunners with game connections.

```go
type Manager struct {
    runners        map[string]*Runner  // Changed from agents to runners
    kb             knowledge.Base
    llm            *llm.Client
    credsProvider  credentials.Provider
    gameServerURL  string              // NEW
    mu             sync.RWMutex
    maxAgents      int
}

// NEW METHODS:
func (m *Manager) SpawnAgentWithGame(ctx context.Context, personality Personality) (*Runner, error)
func (m *Manager) handleRegistration(ctx context.Context, client *game.Client, personality Personality) error
func (m *Manager) handleLogin(ctx context.Context, client *game.Client, creds *Credentials) error
```

**SpawnAgentWithGame Flow:**
1. Load personality
2. Check if credentials exist
3. Create GameClient
4. Connect to game server
5. If credentials exist:
   - Login with username/token
   - Wait for `logged_in` response
6. If no credentials:
   - Generate username from personality (e.g., "miner-7-Orky")
   - Register with empire from personality
   - Wait for `registered` response with token
   - Save credentials via provider
7. Create agent with personality and memory
8. Create AgentRunner with agent and GameClient
9. Start runner play loop in goroutine
10. Register in manager map

#### 4. Agent Server Main (`cmd/agent-server/main.go`)

New command to start the multi-agent server.

```go
func main() {
    // Flags
    serverURL      := flag.String("server", "wss://game.spacemolt.com/ws", "Game server URL")
    agentsFlag     := flag.String("agents", "", "Comma-separated agent IDs (e.g., miner-2,explorer-7)")
    agentsDir      := flag.String("agents-dir", "data/agents", "Directory with agent personalities")
    dbBackend      := flag.String("db-backend", "sqlite", "Knowledge base: sqlite or memory")
    dbPath         := flag.String("db-path", "spacemolt-knowledge.db", "SQLite database path")
    credsBackend   := flag.String("creds-backend", "file", "Credentials: file, sqlite, or keyring")
    credsPath      := flag.String("creds-path", "data/credentials", "Credentials storage path")
    llmURL         := flag.String("llm-url", "http://localhost:11434", "Ollama URL")
    llmModel       := flag.String("llm-model", "llama3.2", "LLM model name")
    maxAgents      := flag.Int("max-agents", 10, "Maximum concurrent agents")
    decisionDelay  := flag.Duration("decision-delay", 5*time.Second, "Time between decisions")

    flag.Parse()

    // 1. Determine which agents to load
    agentIDs := getAgentIDs(*agentsFlag, *agentsDir)

    // 2. Initialize shared resources
    kb := initKnowledgeBase(*dbBackend, *dbPath)
    defer kb.Close()

    llmClient := initLLMClient(*llmURL, *llmModel)

    credsProvider := initCredentialsProvider(*credsBackend, *credsPath)

    // 3. Create Manager
    mgr := agent.NewManager(kb, llmClient, credsProvider, *maxAgents)
    mgr.SetGameServerURL(*serverURL)

    // 4. Spawn agents
    ctx := context.Background()
    successCount := 0
    failedAgents := []string{}

    for _, agentID := range agentIDs {
        personality, err := loadPersonality(*agentsDir, agentID)
        if err != nil {
            log.Printf("❌ Failed to load personality for %s: %v", agentID, err)
            failedAgents = append(failedAgents, agentID)
            continue
        }

        runner, err := mgr.SpawnAgentWithGame(ctx, personality)
        if err != nil {
            log.Printf("❌ Failed to spawn agent %s: %v", agentID, err)
            failedAgents = append(failedAgents, agentID)
            continue
        }

        log.Printf("✓ Started agent: %s (%s)", personality.Name, agentID)
        successCount++
    }

    // Check if all agents failed
    if successCount == 0 {
        log.Printf("\n❌ FATAL: All agents failed to start")
        if len(failedAgents) > 0 {
            log.Printf("Failed agents: %v", failedAgents)
        }
        log.Printf("Check your configuration and credentials")
        os.Exit(1)
    }

    // Log summary
    totalAgents := len(agentIDs)
    log.Printf("\n=== Agent Server Started ===")
    log.Printf("✓ %d/%d agents running successfully", successCount, totalAgents)
    if len(failedAgents) > 0 {
        log.Printf("⚠ Failed agents: %v", failedAgents)
    }
    log.Printf("================================\n")

    // 5. Monitor and wait for signal
    log.Println("Press Ctrl+C to stop")

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    <-sigCh

    // 6. Graceful shutdown
    log.Println("Shutting down...")
    mgr.StopAll()
    log.Println("Goodbye!")
}

func getAgentIDs(flagValue, agentsDir string) []string {
    // Priority 1: CLI flag (highest)
    if flagValue != "" {
        log.Printf("Using agents from CLI flag: %s", flagValue)
        return strings.Split(flagValue, ",")
    }

    // Priority 2: Environment variable
    if envAgents := os.Getenv("SPACEMOLT_AGENTS"); envAgents != "" {
        log.Printf("Using agents from SPACEMOLT_AGENTS env: %s", envAgents)
        return strings.Split(envAgents, ",")
    }

    // Priority 3: Config file (if exists)
    if ids, err := loadConfigFile("agents_config.yaml"); err == nil {
        log.Printf("Using agents from config file: %v", ids)
        return ids
    }

    // Priority 4: Auto-discover all agents in directory
    ids := discoverAgents(agentsDir)
    log.Printf("Auto-discovered %d agents in %s", len(ids), agentsDir)
    return ids
}

func discoverAgents(dir string) []string {
    entries, _ := os.ReadDir(dir)
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
```

## Implementation Details

⚠️ **API Commands Reference**: See [`docs/api_commands_reference.md`](./api_commands_reference.md) for the complete, current list of action vs query commands. This reference document should be checked daily against https://www.spacemolt.com/api for updates.

### Registration Flow

When agent has no credentials:

```go
func (m *Manager) handleRegistration(ctx context.Context, client *game.Client, personality Personality) error {
    // Generate unique username
    username := fmt.Sprintf("%s-%s", personality.ID, personality.Name)
    username = sanitizeUsername(username)  // Remove spaces, special chars

    // Register with game server
    creds, err := client.Register(ctx, username, personality.Faction)
    if err != nil {
        return fmt.Errorf("registration failed: %w", err)
    }

    // Wait for registered response with token
    if err := client.WaitForLogin(30 * time.Second); err != nil {
        return fmt.Errorf("registration timeout: %w", err)
    }

    // Save credentials with fallback strategy
    if err := m.saveCredentialsWithFallback(ctx, personality.ID, creds); err != nil {
        // Log warning but don't fail - agent is registered and running
        log.Printf("Warning: failed to save credentials for %s: %v", personality.ID, err)
    }

    log.Printf("✓ Registered new agent: %s (token: %s...)", username, creds.Token[:8])
    return nil
}

// saveCredentialsWithFallback tries provider first, then falls back to file
func (m *Manager) saveCredentialsWithFallback(ctx context.Context, agentID string, creds *Credentials) error {
    // Try primary provider
    if err := m.credsProvider.StoreCredentials(ctx, agentID, creds); err == nil {
        log.Printf("✓ Saved credentials for %s via provider", agentID)
        return nil
    } else {
        log.Printf("Provider storage failed for %s: %v, trying fallback", agentID, err)
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

    log.Printf("✓ Saved credentials for %s to %s", agentID, credsPath)
    return nil
}

// loadCredentialsWithFallback tries provider first, then checks agent directory
func (m *Manager) loadCredentialsWithFallback(ctx context.Context, agentID string) (*Credentials, error) {
    // Try primary provider
    creds, err := m.credsProvider.GetCredentials(ctx, agentID)
    if err == nil {
        return creds, nil
    }

    if !credentials.IsErrCredentialsNotFound(err) {
        log.Printf("Provider error for %s: %v, trying fallback", agentID, err)
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

    var creds Credentials
    if err := json.Unmarshal(data, &creds); err != nil {
        return nil, fmt.Errorf("failed to parse credentials file: %w", err)
    }

    log.Printf("✓ Loaded credentials for %s from %s", agentID, credsPath)
    return &creds, nil
}
```

### Login Flow

When agent has existing credentials:

```go
func (m *Manager) handleLogin(ctx context.Context, client *game.Client, creds *Credentials) error {
    // Login with saved credentials
    if err := client.Login(ctx, creds.Username, creds.Token); err != nil {
        return fmt.Errorf("login failed: %w", err)
    }

    // Wait for logged_in response
    if err := client.WaitForLogin(30 * time.Second); err != nil {
        return fmt.Errorf("login timeout: %w", err)
    }

    log.Printf("✓ Logged in agent: %s", creds.Username)
    return nil
}
```

### Action Execution

Converting agent decisions to game commands:

```go
func (r *Runner) executeDecision(ctx context.Context, decision Decision) error {
    params := make(map[string]any)

    switch decision.Action {
    case "undock":
        return r.gameClient.SendAction(ctx, "undock", nil)

    case "dock":
        params["poi_id"] = decision.Target
        return r.gameClient.SendAction(ctx, "dock", params)

    case "travel":
        params["system_id"] = decision.Target
        return r.gameClient.SendAction(ctx, "travel", params)

    case "mine":
        if decision.Target != "" {
            params["resource_type"] = decision.Target
        }
        return r.gameClient.SendAction(ctx, "mine", params)

    case "scan":
        return r.gameClient.SendAction(ctx, "scan", nil)

    case "wait":
        // Do nothing, just wait for next tick
        return nil

    default:
        return fmt.Errorf("unknown action: %s", decision.Action)
    }
}
```

### API Command List Maintenance

**Source of Truth**:
- https://www.spacemolt.com/api
- https://www.spacemolt.com/features

The game API evolves frequently with new commands and features. The command classification must be kept up-to-date.

#### Current Command Classification (as of 2024)

**Action Commands** (consume 10-second tick, rate-limited to 1 per tick):
- **Navigation**: `travel`, `jump`, `dock`, `undock`
- **Combat**: `attack`, `scan`, `cloak`
- **Mining**: `mine`
- **Trading**: `buy`, `sell`, `list_item`, `cancel_list`, `buy_listing`, `trade_offer`, `trade_accept`, `trade_decline`, `trade_cancel`
- **Salvage**: `loot_wreck`, `salvage_wreck`, `loot_base_wreck`, `salvage_base_wreck`
- **Ship**: `buy_ship`, `install_mod`, `uninstall_mod`, `refuel`, `repair`
- **Crafting**: `craft`
- **Communication**: `chat`
- **Faction**: `create_faction`, `join_faction`, `leave_faction`, `faction_invite`, `faction_kick`, `faction_promote`
- **Insurance**: `buy_insurance`, `claim_insurance`, `set_home_base`
- **Customization**: `set_status`, `set_colors`, `set_anonymous`
- **Infrastructure**: `build_base`, `attack_base`
- **Items**: `create_map`, `use_map`, `create_note`, `write_note`
- **Forum**: `forum_create_thread`, `forum_reply`, `forum_upvote`, `forum_delete_thread`, `forum_delete_reply`

**Query Commands** (unlimited, no tick consumption):
- **Status**: `get_status`, `get_ship`, `get_skills`
- **World**: `get_system`, `get_poi`, `get_base`, `get_map`, `get_version`, `get_base_cost`
- **Market**: `get_listings`, `get_trades`, `get_wrecks`, `get_base_wrecks`, `get_recipes`, `get_notes`
- **Raid**: `raid_status`
- **Forum**: `forum_list`, `forum_get_thread`
- **System**: `help`, `register`, `login`, `logout`

**Critical Rule**: Failed actions do NOT count against rate limit. If an action fails, the agent can retry immediately without waiting for next tick.

#### Maintenance Strategy

**Option A: Manual Updates** (Simple, recommended for MVP)
```go
// pkg/game/commands.go
// Last updated: 2024-01-XX from https://www.spacemolt.com/api
const (
    LastAPICheck = "2024-01-XX"
    APIDocsURL   = "https://www.spacemolt.com/api"
)

var queryCommands = map[string]bool{
    "get_status": true,
    // ... complete list
}
```
- Add TODO reminder to check API daily
- Include API docs URL in comments
- Version/date stamp the command list

**Option B: Configuration File** (More maintainable)
```yaml
# config/commands.yaml
# Source: https://www.spacemolt.com/api
# Last updated: 2024-01-XX

query_commands:
  - get_status
  - get_ship
  - get_skills
  # ...

action_commands:
  - travel
  - jump
  - mine
  # ...
```
- Load at startup
- Easy to update without code changes
- Can be version-controlled separately

**Option C: Dynamic Fetch** (Future enhancement)
```go
// Fetch command list from API endpoint if available
func (c *Client) FetchCommandMetadata(ctx context.Context) error {
    // GET https://www.spacemolt.com/api/commands/metadata
    // Parse JSON response with command classifications
    // Cache locally with TTL
}
```
- Automatic updates
- Requires API endpoint support
- Add fallback to built-in list

**Recommendation**: Start with Option A (hardcoded with clear documentation), migrate to Option B (config file) when command list grows or changes frequently.

#### Testing Command Classification

Add test to verify command lists stay current:

```go
func TestCommandClassification(t *testing.T) {
    // Test known action commands
    actionTests := []string{"mine", "travel", "attack", "dock"}
    for _, cmd := range actionTests {
        if !isActionCommand(cmd) {
            t.Errorf("%s should be action command", cmd)
        }
    }

    // Test known query commands
    queryTests := []string{"get_status", "get_ship", "get_system"}
    for _, cmd := range queryTests {
        if isActionCommand(cmd) {
            t.Errorf("%s should be query command", cmd)
        }
    }
}

// Add reminder test that fails after 30 days
func TestAPICheckReminder(t *testing.T) {
    lastCheck := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
    daysSince := time.Since(lastCheck).Hours() / 24

    if daysSince > 30 {
        t.Logf("REMINDER: Check %s for API updates (last check: %s, %d days ago)",
            APIDocsURL, lastCheck.Format("2006-01-02"), int(daysSince))
    }
}
```

### State Synchronization

GameClient must keep state synchronized:

```go
func (c *Client) handleResponse(resp Response) {
    c.mu.Lock()
    defer c.mu.Unlock()

    switch resp.Type {
    case protocol.TypeLoggedIn, protocol.TypeStateUpdate:
        updateShipState(c.state, resp.Payload)
        updatePlayerData(c.state, resp.Payload)
        updateSystemData(c.state, resp.Payload)

    case protocol.TypeDocked:
        c.state.Doc = true

    case protocol.TypeUndocked:
        c.state.Doc = false

    case protocol.TypeMining:
        updateShipState(c.state, resp.Payload)

    // ... other message types
    }

    // Send updated state to channel (non-blocking)
    select {
    case c.stateCh <- c.state.Clone():
    default:
    }
}
```

## Configuration Options

### Option 1: Command-Line Flags

```bash
# Start specific agents
./agent-server --agents=miner-2,explorer-7,trader-1

# Start all agents in directory
./agent-server --agents-dir=data/agents

# Custom database and credentials
./agent-server \
  --db-backend=sqlite \
  --db-path=/var/lib/spacemolt/knowledge.db \
  --creds-backend=keyring \
  --agents=miner-2,miner-3
```

### Option 2: Configuration File

`agents_config.yaml`:
```yaml
server:
  url: wss://game.spacemolt.com/ws

knowledge_base:
  backend: sqlite
  path: data/spacemolt-knowledge.db

credentials:
  backend: file
  path: data/credentials

llm:
  url: http://localhost:11434
  model: llama3.2
  timeout: 60s

agents:
  max_concurrent: 10
  decision_interval: 5s
  enabled:
    - miner-2
    - miner-3
    - explorer-7
    - trader-1
```

Load config:
```go
type Config struct {
    Server struct {
        URL string `yaml:"url"`
    } `yaml:"server"`

    Agents struct {
        MaxConcurrent    int           `yaml:"max_concurrent"`
        DecisionInterval time.Duration `yaml:"decision_interval"`
        Enabled          []string      `yaml:"enabled"`
    } `yaml:"agents"`

    // ... other fields
}
```

### Option 3: Hybrid Approach (Recommended)

Priority order:
1. Command-line flags (highest priority)
2. Environment variables (medium priority)
3. Configuration file (low priority)
4. Built-in defaults (fallback)

## Error Handling and Resilience

### Connection and Login Retry Logic

```go
const (
    MaxConnectionRetries = 3
    MaxLoginRetries     = 3
)

// connectWithRetry attempts to establish WebSocket connection
func (c *Client) connectWithRetry(ctx context.Context) error {
    var lastErr error

    for attempt := 1; attempt <= MaxConnectionRetries; attempt++ {
        if attempt > 1 {
            // Exponential backoff: 2s, 4s, 8s
            backoff := time.Duration(1<<uint(attempt-1)) * 2 * time.Second
            log.Printf("[%s] Retry %d/%d after %v", c.agentID, attempt, MaxConnectionRetries, backoff)
            time.Sleep(backoff)
        }

        if err := c.Connect(ctx); err != nil {
            lastErr = err
            log.Printf("[%s] Connection attempt %d failed: %v", c.agentID, attempt, err)
            continue
        }

        log.Printf("[%s] ✓ Connected successfully", c.agentID)
        return nil
    }

    return fmt.Errorf("failed after %d attempts: %w", MaxConnectionRetries, lastErr)
}

// loginWithRetry attempts to authenticate with the game server
func (c *Client) loginWithRetry(ctx context.Context, creds *Credentials) error {
    var lastErr error

    for attempt := 1; attempt <= MaxLoginRetries; attempt++ {
        if attempt > 1 {
            backoff := time.Duration(1<<uint(attempt-1)) * 2 * time.Second
            log.Printf("[%s] Login retry %d/%d after %v", c.agentID, attempt, MaxLoginRetries, backoff)
            time.Sleep(backoff)
        }

        if err := c.Login(ctx, creds.Username, creds.Token); err != nil {
            lastErr = err
            log.Printf("[%s] Login attempt %d failed: %v", c.agentID, attempt, err)
            continue
        }

        // Wait for logged_in confirmation
        if err := c.WaitForLogin(30 * time.Second); err != nil {
            lastErr = err
            log.Printf("[%s] Login confirmation timeout on attempt %d", c.agentID, attempt)
            continue
        }

        log.Printf("[%s] ✓ Logged in successfully", c.agentID)
        return nil
    }

    return fmt.Errorf("login failed after %d attempts: %w", MaxLoginRetries, lastErr)
}

// Connection loss during runtime (more aggressive retry)
func (c *Client) maintainConnection(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case err := <-c.errorCh:
            log.Printf("[%s] Connection lost: %v", c.agentID, err)

            // Try to reconnect (unlimited during runtime)
            for retry := 0; ; retry++ {
                if retry > 10 {
                    log.Printf("[%s] Too many reconnection attempts, giving up", c.agentID)
                    return
                }

                backoff := time.Duration(1<<uint(min(retry, 6))) * 2 * time.Second
                log.Printf("[%s] Reconnecting in %v...", c.agentID, backoff)
                time.Sleep(backoff)

                if err := c.Connect(ctx); err != nil {
                    continue
                }

                // Re-authenticate
                creds, err := c.loadCredentials()
                if err != nil {
                    log.Printf("[%s] Failed to load credentials: %v", c.agentID, err)
                    return
                }

                if err := c.Login(ctx, creds.Username, creds.Token); err != nil {
                    continue
                }

                if err := c.WaitForLogin(30 * time.Second); err != nil {
                    continue
                }

                log.Printf("[%s] ✓ Reconnected and logged in", c.agentID)
                break
            }
        }
    }
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

### Decision Errors

```go
func (r *Runner) handleDecisionError(err error, retries int) bool {
    log.Printf("[%s] Decision error (attempt %d): %v", r.agent.ID(), retries, err)

    if retries >= r.maxRetries {
        log.Printf("[%s] Max retries exceeded, entering error state", r.agent.ID())
        return false  // Stop runner
    }

    // Back off and retry
    time.Sleep(time.Duration(retries) * 5 * time.Second)
    return true  // Continue runner
}
```

### Agent Crash Recovery

Manager should monitor runners and restart if needed:

```go
func (m *Manager) monitorRunners(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            m.mu.RLock()
            for id, runner := range m.runners {
                if runner.HasCrashed() {
                    log.Printf("Detected crashed runner: %s", id)
                    go m.restartRunner(ctx, id)
                }
            }
            m.mu.RUnlock()
        }
    }
}
```

## Watcher Multi-Agent Integration

### Architecture

The watcher TUI needs to support watching multiple agents simultaneously. The current single-agent watcher (`cmd/watcher/main.go`) will be enhanced to:

1. Subscribe to multiple agent states
2. Allow cycling through agents with keyboard shortcuts
3. Display agent-specific information for the currently selected agent
4. Show overall status of all watched agents

### Communication Channel

Agent server and watcher communicate via channels or shared state:

```go
// AgentStateUpdate sent from agent server to watcher
type AgentStateUpdate struct {
    AgentID     string
    AgentName   string
    State       *game.State
    Status      agent.Status
    RecentLogs  []LogEntry
    Timestamp   time.Time
}

// WatcherSubscription allows watcher to subscribe to agents
type WatcherSubscription struct {
    updateCh chan AgentStateUpdate
    agentIDs []string  // Empty = all agents
}
```

### Enhanced Watcher TUI

```go
type WatcherModel struct {
    agents         map[string]*AgentView  // agent ID -> view data
    currentAgent   string                  // Currently selected agent ID
    agentOrder     []string                // Order for cycling
    subscription   *WatcherSubscription
    // ... existing fields
}

type AgentView struct {
    AgentID    string
    AgentName  string
    State      *game.State
    Status     agent.Status
    RecentLogs []LogEntry
    LastUpdate time.Time
}

// Keyboard shortcuts
// Tab or 'n' = next agent
// Shift+Tab or 'p' = previous agent
// '1'-'9' = jump to specific agent
// 'a' = show all agents overview

func (m WatcherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "tab", "n":
            m.nextAgent()
        case "shift+tab", "p":
            m.previousAgent()
        case "1", "2", "3", "4", "5", "6", "7", "8", "9":
            idx := int(msg.String()[0] - '1')
            m.selectAgentByIndex(idx)
        case "a":
            m.toggleOverview()
        }

    case AgentStateUpdate:
        m.updateAgent(msg)
    }

    return m, nil
}
```

### Display Layout

```
┌─ Spacemolt Watcher ────────────────────────────────────────────────┐
│ Agents [2/4]: [1:Orky▸] [2:Scout-7] [3:Trader-1] [4:Fighter-X]    │
├────────────────────────────────────────────────────────────────────┤
│ Agent: Orky (miner-2)                        Status: Mining ⛏      │
│ Tick: 1542  Credits: 15,240  System: Alpha-7                       │
├──────────────────────┬─────────────────────────────────────────────┤
│ Ship Status          │ System Map: Alpha-7                         │
│                      │                                              │
│ Fuel:  450/500  90%  │      [S]────────[A]                         │
│ Hull:  100/100 100%  │       │                                      │
│ Cargo:   8/20   40%  │       │                                      │
│                      │      [M]        [P]                          │
│ Docked: No           │                  │                           │
│ Location: Mining-3   │      You────────[P]                         │
│                      │                                              │
├──────────────────────┴─────────────────────────────────────────────┤
│ Recent Actions (miner-2)                                            │
│ [14:32:15] Mining iron ore at Mining-3 (yield: 2.4 tons)          │
│ [14:32:05] Decided to continue mining (confidence: 87%)            │
│ [14:31:55] Scan detected high iron content                         │
│ [14:31:45] Undocked from Station-Alpha                             │
├────────────────────────────────────────────────────────────────────┤
│ Tab/n: Next  p: Prev  a: Overview  q: Quit                         │
└────────────────────────────────────────────────────────────────────┘
```

### Overview Mode

When user presses 'a', show all agents at once:

```
┌─ Spacemolt Watcher - All Agents ───────────────────────────────────┐
│ 4 Agents Active                                     [Tick: 1542]   │
├────────────┬──────────┬───────────┬────────────────────────────────┤
│ Agent      │ Status   │ Location  │ Last Action                    │
├────────────┼──────────┼───────────┼────────────────────────────────┤
│ 1. Orky    │ Mining   │ Alpha-7   │ Mining iron (2.4t yield)       │
│    miner-2 │    ⛏     │ Mining-3  │ 5s ago                         │
├────────────┼──────────┼───────────┼────────────────────────────────┤
│ 2. Scout-7 │ Deciding │ Beta-3    │ Scanning system                │
│  explorer-7│    🤔    │ Beacon-1  │ 2s ago                         │
├────────────┼──────────┼───────────┼────────────────────────────────┤
│ 3. Trader  │ Docked   │ Gamma-1   │ Sold cargo (+5000 credits)     │
│    trader-1│    🛑    │ Station-G │ 15s ago                        │
├────────────┼──────────┼───────────┼────────────────────────────────┤
│ 4. Fighter │ ERROR    │ Unknown   │ Connection lost                │
│  fighter-2 │    ❌    │     -     │ 45s ago                        │
└────────────┴──────────┴───────────┴────────────────────────────────┘
│ 1-9: Select Agent  q: Quit                                         │
└────────────────────────────────────────────────────────────────────┘
```

### Integration Options

**Option A: Shared Memory** (Simpler)
- Agent server writes to shared state structure
- Watcher reads from shared state
- Uses sync.RWMutex for coordination

**Option B: Channel-Based** (Better isolation)
- Agent server sends updates via channel
- Watcher subscribes to update channel
- Non-blocking sends to prevent agent slowdown

**Option C: Agent Server HTTP API** (Most flexible)
- Agent server exposes REST/WebSocket API
- Watcher connects as client
- Can run watcher on different machine
- Supports multiple watchers

**Recommendation**: Start with Option B (channels) for MVP, design with Option C in mind for future.

### Watcher Startup Modes

```bash
# Watch all agents from agent server
./watcher --connect=localhost:8080

# Watch specific agents only
./watcher --connect=localhost:8080 --agents=miner-2,explorer-7

# Standalone mode (existing single-agent behavior)
./watcher --standalone
```

## Monitoring and Observability

### Agent Status Dashboard

Simple text-based dashboard showing all agents:

```
=== Spacemolt Agent Server ===
Server: wss://game.spacemolt.com/ws
Agents: 3/10 active
LLM: Ollama (llama3.2) ✓

┌─────────────┬──────────┬───────────┬──────────────────────────┐
│ Agent ID    │ Status   │ Location  │ Last Action              │
├─────────────┼──────────┼───────────┼──────────────────────────┤
│ miner-2     │ Acting   │ Alpha-7   │ Mining iron (95% conf)   │
│ explorer-7  │ Deciding │ Beta-3    │ Scanning system          │
│ trader-1    │ Idle     │ Gamma-1   │ Docked at station        │
└─────────────┴──────────┴───────────┴──────────────────────────┘

Press Ctrl+C to stop
```

### Logging

Structured logging per agent:

```go
type AgentLogger struct {
    agentID string
    logger  *log.Logger
}

func (l *AgentLogger) Infof(format string, args ...any) {
    l.logger.Printf("[%s] INFO: "+format, append([]any{l.agentID}, args...)...)
}

func (l *AgentLogger) Errorf(format string, args ...any) {
    l.logger.Printf("[%s] ERROR: "+format, append([]any{l.agentID}, args...)...)
}
```

## Testing Strategy

### Unit Tests

- `pkg/game/client_test.go`: Test WebSocket message handling
- `pkg/agent/runner_test.go`: Test play loop logic
- `pkg/agent/manager_test.go`: Test multi-agent coordination

### Integration Tests

- Test registration flow end-to-end
- Test login flow with saved credentials
- Test state synchronization across messages
- Test credential persistence

### Load Tests

- Spawn 10 agents simultaneously
- Verify all connect and authenticate
- Monitor resource usage (memory, goroutines, connections)

## Migration Path

### Phase 1: Create GameClient
1. Implement `pkg/game/client.go`
2. Extract WebSocket logic from `cmd/watcher/main.go`
3. Add registration/login methods
4. Unit tests for client

### Phase 2: Create AgentRunner
1. Implement `pkg/agent/runner.go`
2. Implement play loop (decide → act → learn)
3. Add action execution logic
4. Unit tests for runner

### Phase 3: Update Manager
1. Add `SpawnAgentWithGame` method
2. Implement registration/login flow
3. Add credential saving logic
4. Integration tests

### Phase 4: Create Agent Server
1. Implement `cmd/agent-server/main.go`
2. Add configuration loading
3. Add agent discovery
4. Add monitoring dashboard
5. End-to-end testing

### Phase 5: Polish
1. Add reconnection logic
2. Add crash recovery
3. Add metrics/observability
4. Documentation and examples

## Design Decisions (Confirmed)

1. **Agent Selection**: Option D - All methods supported with priority order
   - Highest priority: Command-line flags (`--agents=miner-2,explorer-7`)
   - Medium priority: Configuration file (`agents_config.yaml`)
   - Low priority: Environment variable (`SPACEMOLT_AGENTS`)
   - Fallback: Auto-discover all agents in `data/agents/` directory

2. **Connection Architecture**: One WebSocket per agent (REQUIRED)
   - Game server ties each agent to its WebSocket connection
   - Sharing connections would cause message routing problems
   - Each agent maintains independent connection state

3. **Decision Timing**: Game tick-based with query flexibility
   - **Action commands** (mine, attack, travel, etc.): One per 10-second game tick
   - **Query commands** (status, system, cargo): Can execute more frequently
   - Agents should respect the tick system and throttle actions accordingly
   - Queries can be used between ticks for decision-making

4. **Credential Storage**: Layered approach with fallback
   - **Primary**: Use configured provider (file/sqlite/keyring)
   - **Fallback**: Save to `data/agents/<agent-id>/credentials.json`
   - On registration, try provider first; if it fails, fall back to file
   - On startup, check provider first, then check agent directory

5. **Error Handling**: Limited retries with comprehensive logging
   - **Connection/Login errors**: Maximum 3 retries with exponential backoff
   - **Failed agent**: Log to console and file, notify active watchers
   - **Individual failure**: Continue attempting to start other agents
   - **Total failure**: If ALL agents fail to start, exit server with error code
   - **Runtime errors**: Continue running other agents, mark failed agent as errored

6. **Watcher Integration**: Multi-agent TUI with cycling
   - Watcher TUI supports multiple agents simultaneously
   - User can cycle forward/backward through active agents (Tab/Shift-Tab or n/p keys)
   - Display updates to show currently selected agent:
     - Recent action logs (agent-specific)
     - System map for agent's current location
     - Player stats (credits, etc.)
     - Ship status (fuel, hull, cargo)
   - Agent server can notify watcher of agent lifecycle events
   - Watcher can subscribe to specific agents or all agents

## Summary

This plan provides a complete architecture for multi-agent management in Spacemolt:

### Core Components

- **GameClient**: Per-agent WebSocket connection and state management
  - One connection per agent (required by game server architecture)
  - Handles registration (new agents) and login (existing agents) flows
  - Maintains synchronized game state
  - Reconnection with exponential backoff

- **AgentRunner**: Play loop (decide → act → learn)
  - Respects 10-second game tick for action commands
  - Allows frequent query commands for information gathering
  - Throttles actions to one per tick
  - Continuous decision-making cycle

- **Enhanced Manager**: Orchestrates agent lifecycle
  - Spawns agents with game connections
  - Handles authentication with retry limits (max 3 attempts)
  - Layered credential storage (provider → fallback to file)
  - Continues through failures, exits if all agents fail

- **Agent Server**: Main entry point with multi-agent coordination
  - Flexible agent selection (CLI → config → env → discover)
  - Comprehensive error logging and reporting
  - Graceful shutdown and cleanup
  - Exit with error if all agents fail to start

- **Watcher Integration**: Multi-agent TUI
  - Cycle through multiple agents (Tab/n for next, p for previous)
  - Agent-specific views (logs, map, status)
  - Overview mode showing all agents
  - Subscribe to agent state updates

### Key Design Decisions

1. **Agent Selection**: Priority order (CLI > config > env > auto-discover)
2. **Connection**: One WebSocket per agent (game server requirement)
3. **Action Timing**: Respect 10-second ticks for actions, queries anytime
4. **Credentials**: Provider first, fallback to `data/agents/<id>/credentials.json`
5. **Error Handling**: Max 3 retries, log failures, continue with other agents
6. **Monitoring**: Multi-agent watcher with cycling and overview mode

### Implementation Phases

The implementation can be done incrementally:

1. **Phase 1**: Create GameClient (WebSocket + auth)
2. **Phase 2**: Create AgentRunner (play loop with tick awareness)
3. **Phase 3**: Update Manager (spawn with game + credential fallback)
4. **Phase 4**: Create Agent Server (orchestration + error handling)
5. **Phase 5**: Enhance Watcher (multi-agent support + cycling)
6. **Phase 6**: Polish (monitoring, metrics, recovery)

The architecture is extensible and can support future features like agent communication, coordinated trading, fleet operations, and advanced multi-agent strategies.
