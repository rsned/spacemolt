# Spacemolt Multi-Agent System Design

## Table of Contents
1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Package Structure](#package-structure)
4. [Agent System](#agent-system)
5. [LLM Integration](#llm-integration)
6. [Knowledge Base](#knowledge-base)
7. [Multi-Agent Coordination](#multi-agent-coordination)
8. [Human Watcher Interface](#human-watcher-interface)
9. [Implementation Phases](#implementation-phases)
10. [Data Models](#data-models)

---

## Overview

The Spacemolt Multi-Agent System transforms the single-client game into a platform where multiple autonomous AI agents explore, interact with, and learn about the game universe. A human watcher can observe and switch between agents to monitor their progress.

### Key Design Principles

1. **Autonomy**: Agents are self-controlled and choose their own actions
2. **Personality-Driven**: Agent behavior emerges from personality traits and motivations
3. **Knowledge Accumulation**: Agents learn and persist knowledge across sessions
4. **Collaboration**: Agents share knowledge with allies
5. **Observability**: Humans can watch and understand agent decisions

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Human Watcher TUI                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐ │
│  │Agents    │ │  Map     │ │  Status  │ │  Agent Log   │ │
│  │[Explorer7]│ │          │ │          │ │              │ │
│  │[Miner-2]  │ │          │ │          │ │              │ │
│  │[Fighter]  │ │          │ │          │ │              │ │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────┘ │
│  ┌─────────────────────────────────────────────────────┐  │
│  │       Selected Agent Detail / Knowledge              │  │
│  └─────────────────────────────────────────────────────┘  │
└───────────────────────────┬───────────────────────────────┘
                            │
                    ┌───────▼────────┐
                    │ Agent Manager  │
                    │  (goroutines)  │
                    └───────┬────────┘
                            │
        ┌───────────────────┼───────────────────┐
        │                   │                   │
   ┌────▼────┐        ┌────▼────┐        ┌────▼────┐
   │Agent 1  │        │Agent 2  │        │Agent 3  │
   │Explorer │        │Miner    │        │Fighter  │
   └────┬────┘        └────┬────┘        └────┬────┘
        │                   │                   │
        └───────────────────┼───────────────────┘
                            │
                    ┌───────▼────────┐
                    │  Knowledge     │
                    │    Base        │
                    │   (SQLite)     │
                    └────────────────┘

                    ┌───────▼────────┐
                    │  LLM Service   │
                    │   (Ollama)     │
                    └────────────────┘
```

---

## Package Structure

Following Go best practices with clear separation of concerns:

```
spacemolt/
├── cmd/                          # Application entry points
│   ├── watcher/                  # Human watcher TUI application
│   │   └── main.go              # Main entry point for watcher
│   └── agent/                   # Standalone agent runner (for testing)
│       └── main.go
│
├── pkg/                          # Public libraries
│   ├── agent/                    # Core agent framework
│   │   ├── agent.go             # Agent interface and base implementation
│   │   ├── personality.go       # Personality and motivation system
│   │   ├── memory.go            # Agent memory and learning
│   │   └── decision.go          # Decision-making logic
│   │
│   ├── game/                     # Game client library
│   │   ├── client.go            # WebSocket client
│   │   ├── messages.go          # Message types and handling
│   │   ├── state.go             # Game state management
│   │   └── actions.go           # Available game actions
│   │
│   ├── llm/                      # LLM integration layer
│   │   ├── client.go            # Ollama client interface
│   │   ├── prompts.go           # Prompt templates
│   │   └── response.go          # Response parsing
│   │
│   ├── knowledge/                 # Knowledge management
│   │   ├── database.go          # Database interface
│   │   ├── systems.go           # Solar system knowledge
│   │   ├── pois.go              # POI knowledge
│   │   ├── resources.go         # Resource/pricing data
│   │   └── agents.go            # Agent registry and metadata
│   │
│   ├── tui/                      # TUI components (shared)
│   │   ├── watcher.go           # Watcher-specific TUI
│   │   ├── panels.go            # Panel components
│   │   └── map.go               # Map rendering
│   │
│   └── config/                   # Configuration
│       ├── config.go            # Config loading
│       └── agents.yaml          # Agent definitions
│
├── internal/                     # Internal packages
│   ├── ws/                       # WebSocket handling
│   │   └── connection.go
│   └── protocol/                 # Game protocol definitions
│       └── messages.go
│
├── data/                         # Runtime data
│   ├── agents/                   # Agent data directories
│   │   ├── explorer-7/
│   │   │   ├── personality.md
│   │   │   ├── memory.db
│   │   │   └── log.txt
│   │   └── miner-2/
│   │       └── ...
│   └── knowledge/                # Shared knowledge base
│       └── shared.db
│
├── docs/                         # Documentation
│   ├── design.md                 # This document
│   ├── api.md                    # API documentation
│   └── agents.md                 # Agent creation guide
│
├── go.mod
├── go.sum
└── README.md
```

---

## Agent System

### Agent Interface

```go
package agent

import (
    "context"
    "github.com/user/spacemolt/pkg/game"
)

// Agent represents an autonomous game-playing agent
type Agent interface {
    // Identity
    Name() string
    ID() string

    // Personality
    Personality() Personality
    UpdatePersonality(experience Experience) error

    // Decision Making
    Decide(ctx context.Context, state *game.State) (Decision, error)

    // Learning
    Learn(result ActionResult) error
    Memory() Memory

    // Lifecycle
    Start(ctx context.Context) error
    Stop() error
    Status() AgentStatus
}

// Personality defines an agent's traits and motivations
type Personality struct {
    Name      string            `yaml:"name"`
    Role      string            `yaml:"role"`
    Traits    map[string]float64 `yaml:"traits"`
    Skills    map[string]string `yaml:"skills"`
    Motivations []Motivation     `yaml:"motivations"`
    Biography string            `yaml:"biography"`
}

// Motivation drives agent behavior
type Motivation struct {
    Primary   string  `yaml:"primary"`
    Secondary string  `yaml:"secondary"`
    Tertiary string  `yaml:"tertiary"`
    Weights   map[string]float64 `yaml:"weights"`
}

// Decision represents a chosen action
type Decision struct {
    Action      game.Action
    Reasoning   string    // LLM explanation
    Confidence  float64
    Alternatives []string
}

// Memory stores agent knowledge
type Memory interface {
    // Knowledge access
    KnownSystems() []SystemKnowledge
    KnownPOIs(systemID string) []POIKnowledge
    ResourceHistory(resourceID string) []ResourcePrice

    // Memory update
    RememberSystem(system System) error
    RememberPOI(poi POI) error
    RememberResourcePrice(price ResourcePrice) error

    // Experience
    AddExperience(exp Experience) error
    GetExperiences(limit int) []Experience
}
```

### Base Agent Implementation

```go
package agent

import (
    "context"
    "fmt"
    "github.com/user/spacemolt/pkg/game"
    "github.com/user/spacemolt/pkg/llm"
    "github.com/user/spacemolt/pkg/knowledge"
)

// BaseAgent provides default agent behavior
type BaseAgent struct {
    id         string
    name       string
    personality Personality
    memory     Memory
    client     *game.Client
    llm        *llm.Client
    status     AgentStatus

    // Channels
    decisionCh chan Decision
    resultCh   chan ActionResult
    stopCh     chan struct{}
}

// NewBaseAgent creates a new agent
func NewBaseAgent(
    id string,
    personality Personality,
    memory Memory,
    client *game.Client,
    llmClient *llm.Client,
) *BaseAgent {
    return &BaseAgent{
        id:         id,
        name:       personality.Name,
        personality: personality,
        memory:     memory,
        client:     client,
        llm:        llmClient,
        status:     AgentStatusIdle,
        decisionCh: make(chan Decision, 10),
        resultCh:   make(chan ActionResult, 10),
        stopCh:     make(chan struct{}),
    }
}

// Decide uses LLM to choose next action
func (a *BaseAgent) Decide(ctx context.Context, state *game.State) (Decision, error) {
    // Build prompt from personality, memory, and current state
    prompt := a.buildDecisionPrompt(state)

    // Get LLM decision
    response, err := a.llm.Decide(ctx, prompt)
    if err != nil {
        return Decision{}, fmt.Errorf("LLM decision failed: %w", err)
    }

    // Parse response into action
    action, err := game.ParseAction(response.Action)
    if err != nil {
        return Decision{}, fmt.Errorf("invalid action: %w", err)
    }

    decision := Decision{
        Action:     action,
        Reasoning:  response.Reasoning,
        Confidence: response.Confidence,
    }

    return decision, nil
}

func (a *BaseAgent) buildDecisionPrompt(state *game.State) string {
    // Build context-aware prompt
    return fmt.Sprintf(`
You are %s, a %s in the Spacemolt universe.

PERSONALITY:
- Traits: %v
- Motivations: %v
- Skills: %v

CURRENT SITUATION:
- Location: %s
- Ship Status: Fuel %.0f/%.0f, Hull %.0f/%.0f
- Cargo: %d/%d
- Credits: %.0f

KNOWN UNIVERSE:
%s

RECENT EXPERIENCES:
%s

What do you do next? Provide:
1. Your action (undock, dock, travel, mine, etc.)
2. Your reasoning
3. Your confidence (0-1)
`,
        a.name,
        a.personality.Role,
        a.personality.Traits,
        a.personality.Motivations,
        a.personality.Skills,
        state.Location,
        state.Fuel, state.MaxFuel,
        state.Hull, state.MaxHull,
        len(state.Cargo), state.MaxCargo,
        state.Credits,
        a.formatKnowledge(),
        a.formatRecentExperiences(),
    )
}
```

### Example Personality File

`data/agents/explorer-7/personality.md`:

```yaml
name: "Explorer-7"
id: "explorer-7"
role: "Explorer"

traits:
  curiosity: 0.95
  risk_tolerance: 0.65
  altruism: 0.40
  patience: 0.55
  aggression: 0.20

motivations:
  primary: "explore_unknown"
  secondary: "document_discoveries"
  tertiary: "share_knowledge"
  weights:
    explore_unknown: 0.8
    document_discoveries: 0.6
    share_knowledge: 0.4
    survival: 0.5

skills:
  navigation: "intermediate"
  combat: "basic"
  mining: "basic"
  trading: "novice"
  diplomacy: "intermediate"

biography: |
  Born in the asteroid belt colonies, Explorer-7 has always wondered
  what lies beyond the known hyperspace lanes. After completing their
  pilot certification at age 22, they set out to map the unknown regions
  of space. Driven by an insatiable curiosity and a desire to add to
  humanity's collective knowledge, Explorer-7 spends long cycles in
  deep space, venturing where few dare to travel. They believe that
  knowledge should be shared freely among explorers to advance
  understanding of the universe.
```

---

## LLM Integration

### Ollama Client

```go
package llm

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
)

type Client struct {
    baseURL    string
    model      string
    httpClient *http.Client
}

type DecisionRequest struct {
    AgentName    string
    Personality  Personality
    CurrentState GameState
    Knowledge    string
    Experiences  string
}

type DecisionResponse struct {
    Action     string  `json:"action"`
    Reasoning  string  `json:"reasoning"`
    Confidence float64 `json:"confidence"`
}

// Decide prompts the LLM for an action decision
func (c *Client) Decide(ctx context.Context, prompt string) (*DecisionResponse, error) {
    payload := map[string]interface{}{
        "model":  c.model,
        "prompt": prompt,
        "stream": false,
        "options": map[string]interface{}{
            "temperature": 0.7,
            "num_predict": 500,
        },
    }

    resp, err := c.httpClient.Post(
        c.baseURL+"/api/generate",
        "application/json",
        nil, // JSON body
    )
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }

    // Parse LLM response
    var llmResp struct {
        Response string `json:"response"`
    }
    if err := json.Unmarshal(body, &llmResp); err != nil {
        return nil, err
    }

    // Extract structured decision from text response
    return c.parseDecision(llmResp.Response)
}
```

### Prompt Templates

```go
package llm

const DecisionPromptTemplate = `
You are {{.AgentName}}, a {{.Role}} in the Spacemolt universe.

YOUR PERSONALITY:
Traits: {{range $key, $value := .Traits}}
- {{$key}}: {{$value}}{{end}}

Motivations:
- Primary: {{.Motivations.Primary}}
- Secondary: {{.Motivations.Secondary}}
- Tertiary: {{.Motivations.Tertiary}}

Skills: {{range $key, $value := .Skills}}
- {{$key}}: {{$value}}{{end}}

CURRENT SITUATION:
Location: {{.CurrentState.Location}}
Ship: {{.CurrentState.ShipType}}
Fuel: {{.CurrentState.Fuel}}/{{.CurrentState.MaxFuel}}
Hull: {{.CurrentState.Hull}}/{{.CurrentState.MaxHull}}
Cargo Capacity: {{len .CurrentState.Cargo}}/{{.CurrentState.MaxCargo}}

KNOWN UNIVERSE:
{{.Knowledge}}

RECENT EXPERIENCES:
{{.Experiences}}

INSTRUCTIONS:
Based on your personality and current situation, decide what to do next.

Respond in JSON format:
{
  "action": "undock|dock|travel|mine|scan|wait",
  "target": "target_id_if_applicable",
  "reasoning": "your reasoning in 1-2 sentences",
  "confidence": 0.0-1.0
}

Available actions:
- undock: Leave current station
- dock: Dock at a station in current system
- travel: Jump to another system (specify system_id)
- mine: Mine resources at current location
- scan: Scan current system for details
- wait: Wait and observe

Your decision:
`
```

---

## Knowledge Base

### Database Schema

```sql
-- Solar Systems
CREATE TABLE systems (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    position_x REAL,
    position_y REAL,
    position_z REAL,
    security_level TEXT,
    faction TEXT,
    discovery_date DATETIME,
    discovered_by TEXT,
    last_visited DATETIME,
    visit_count INTEGER DEFAULT 0
);

-- Points of Interest
CREATE TABLE pois (
    id TEXT PRIMARY KEY,
    system_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    position_x REAL,
    position_y REAL,
    description TEXT,
    services TEXT, -- JSON array
    discovery_date DATETIME,
    discovered_by TEXT,
    FOREIGN KEY (system_id) REFERENCES systems(id)
);

-- System Connections
CREATE TABLE connections (
    from_system_id TEXT NOT NULL,
    to_system_id TEXT NOT NULL,
    PRIMARY KEY (from_system_id, to_system_id),
    FOREIGN KEY (from_system_id) REFERENCES systems(id),
    FOREIGN KEY (to_system_id) REFERENCES systems(id)
);

-- Resource Prices
CREATE TABLE resource_prices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    resource_type TEXT NOT NULL,
    poi_id TEXT NOT NULL,
    buy_price REAL,
    sell_price REAL,
    recorded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    recorded_by TEXT,
    FOREIGN KEY (poi_id) REFERENCES pois(id)
);

-- Agent Experiences
CREATE TABLE experiences (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id TEXT NOT NULL,
    experience_type TEXT NOT NULL,
    description TEXT,
    outcome TEXT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);

-- Agents Registry
CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    faction TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_active DATETIME,
    status TEXT
);

-- Agent Memory (per-agent)
CREATE TABLE agent_memory (
    agent_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT, -- JSON
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (agent_id, key),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
```

### Knowledge Interface

```go
package knowledge

import (
    "context"
    "database/sql"
    "github.com/user/spacemolt/pkg/game"
)

type KnowledgeBase struct {
    db *sql.DB
}

// SystemKnowledge represents knowledge about a solar system
type SystemKnowledge struct {
    ID             string
    Name           string
    Position       Position
    SecurityLevel  string
    Faction        string
    Connections    []string
    POIs           []string
    LastVisited    time.Time
    VisitCount     int
}

// RememberSystem stores or updates system knowledge
func (kb *KnowledgeBase) RememberSystem(ctx context.Context, system game.System) error {
    _, err := kb.db.ExecContext(ctx, `
        INSERT INTO systems
        (id, name, position_x, position_y, position_z, security_level, faction, discovery_date, discovered_by)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name = excluded.name,
            security_level = excluded.security_level,
            faction = excluded.faction,
            last_visited = excluded.last_visited,
            visit_count = visit_count + 1
    `, system.ID, system.Name, system.Position.X, system.Position.Y,
       system.Position.Z, system.SecurityLevel, system.Faction,
       time.Now(), "agent")
    return err
}

// GetUnknownConnections finds unexplored connections
func (kb *KnowledgeBase) GetUnknownConnections(ctx context.Context, systemID string) ([]string, error) {
    rows, err := kb.db.QueryContext(ctx, `
        SELECT c.to_system_id
        FROM connections c
        WHERE c.from_system_id = ?
        AND c.to_system_id NOT IN (
            SELECT id FROM systems WHERE visit_count > 0
        )
    `, systemID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var unknown []string
    for rows.Next() {
        var id string
        if err := rows.Scan(&id); err != nil {
            return nil, err
        }
        unknown = append(unknown, id)
    }
    return unknown, nil
}

// ShareKnowledge between agents
func (kb *KnowledgeBase) ShareKnowledge(ctx context.Context, fromAgent, toAgent string, systemID string) error {
    // Copy knowledge from one agent's experience to another
    _, err := kb.db.ExecContext(ctx, `
        INSERT INTO agent_memory (agent_id, key, value)
        SELECT ?, key, value
        FROM agent_memory
        WHERE agent_id = ? AND key LIKE ?
    `, toAgent, fromAgent, "system:"+systemID+"%")
    return err
}
```

---

## Multi-Agent Coordination

### Agent Manager

```go
package agent

import (
    "context"
    "sync"
    "github.com/user/spacemolt/pkg/game"
    "github.com/user/spacemolt/pkg/knowledge"
)

type Manager struct {
    agents   map[string]Agent
    kb       *knowledge.KnowledgeBase
    mu       sync.RWMutex

    // Configuration
    maxAgents int
}

func NewManager(kb *knowledge.KnowledgeBase, maxAgents int) *Manager {
    return &Manager{
        agents:   make(map[string]Agent),
        kb:       kb,
        maxAgents: maxAgents,
    }
}

// SpawnAgent creates and starts a new agent
func (m *Manager) SpawnAgent(ctx context.Context, personality Personality, gameClient *game.Client) (Agent, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    if len(m.agents) >= m.maxAgents {
        return nil, fmt.Errorf("max agents limit reached")
    }

    // Create agent memory
    memory := NewAgentMemory(personality.ID, m.kb)

    // Create agent
    agent := NewBaseAgent(
        personality.ID,
        personality,
        memory,
        gameClient,
        llm.NewClient("http://localhost:11434", "llama3.2"),
    )

    // Start agent
    if err := agent.Start(ctx); err != nil {
        return nil, err
    }

    m.agents[agent.ID()] = agent
    return agent, nil
}

// GetAgent retrieves an agent by ID
func (m *Manager) GetAgent(id string) (Agent, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    agent, ok := m.agents[id]
    return agent, ok
}

// ListAgents returns all agents
func (m *Manager) ListAgents() []Agent {
    m.mu.RLock()
    defer m.mu.RUnlock()

    agents := make([]Agent, 0, len(m.agents))
    for _, agent := range m.agents {
        agents = append(agents, agent)
    }
    return agents
}
```

### Agent Interaction

```go
// When agents meet, they can identify friend/foe
type Interaction struct {
    FromAgent     string
    ToAgent       string
    InteractionType string // "meet", "trade", "share_knowledge", "combat"
    Context       map[string]interface{}
}

// HandleAgentMeeting processes when two agents meet
func (m *Manager) HandleAgentMeeting(ctx context.Context, interaction Interaction) error {
    fromAgent, ok := m.GetAgent(interaction.FromAgent)
    if !ok {
        return fmt.Errorf("unknown agent: %s", interaction.FromAgent)
    }

    // Check if agents are allies/friends
    relationship := m.determineRelationship(fromAgent, interaction.ToAgent)

    if relationship == "ally" || relationship == "friend" {
        // Share knowledge
        return m.kb.ShareKnowledge(
            ctx,
            interaction.FromAgent,
            interaction.ToAgent,
            interaction.Context["system_id"].(string),
        )
    }

    return nil
}

func (m *Manager) determineRelationship(agent Agent, otherAgentID string) string {
    // Check factions, past interactions, etc.
    // For now: same faction = ally
    if agent.Personality().Faction == m.getAgentFaction(otherAgentID) {
        return "ally"
    }
    return "stranger"
}
```

---

## Human Watcher Interface

### Updated TUI Structure

```
┌──────────────────────────────────────────────────────────────┐
│  System: Alpha Centauri    |    T: 12345                     │
├────────────┬─────────────────────────────────────────────────┤
│            │                                                 │
│  Agents    │              MAP & STATUS                       │
│            │  ┌─────────────────────────────────────────┐   │
│  [E7]Expl  │  │  Map View (explorer-7's perspective)    │   │
│  [M2]Miner │  │                                           │   │
│  [F1]Fight │  │     O                                     │   │
│            │  │   o o @ S                                 │   │
│  ↑↓ Switch │  │                                             │   │
│            │  └─────────────────────────────────────────┘   │
│            │                                                 │
│            │  Selected: Explorer-7                          │
│            │  Status: Exploring system "Beta-4"             │
│            │  Fuel: 75/100  Hull: 100/100                   │
│            │  Current Goal: Find unexplored connections     │
│            │  Last Action: "Jumping to Beta-4 because..."   │
├────────────┴─────────────────────────────────────────────────┤
│  AGENT LOG (Explorer-7)                                    │
│  [T:12340] Decided to explore Beta-4 (unknown connection) │
│  [T:12345] Jumped to Beta-4, found station with market     │
│  [T:12350] Scanning system... found 3 POIs                │
└──────────────────────────────────────────────────────────────┘
```

### Watcher TUI Components

```go
package tui

type WatcherModel struct {
    // Existing fields
    gameState      *game.State
    viewportWidth  int
    viewportHeight int
    quitting       bool

    // New fields
    agentManager   *agent.Manager
    selectedAgent  agent.Agent
    agents         []agent.Agent
    agentIndex     int

    // Panels
    logPanel       logPanelModel
    mapPanel       mapPanelModel
    statusPanel    statusPanelModel
    agentPanel     agentPanelModel  // NEW
}

type agentPanelModel struct {
    agents      []string
    selected    int
    showDetails bool
}

func (m WatcherModel) renderAgentPanel(width, height int) string {
    // Render list of agents with status indicators
    // Show selected agent details
}
```

---

## Implementation Phases

### Phase 1: Foundation (Week 1)
**Goal**: Single autonomous explorer agent

**Tasks**:
1. Refactor existing code into package structure
   - Move game client to `pkg/game/`
   - Move TUI components to `pkg/tui/`
   - Create `pkg/agent/` framework

2. Implement agent interface and base agent
   - `Agent` interface
   - `BaseAgent` implementation
   - Personality loading from YAML

3. LLM integration
   - Ollama client
   - Decision prompts
   - Response parsing

4. Knowledge base foundation
   - SQLite schema
   - System/POI storage
   - Basic queries

**Deliverable**: One autonomous agent that can connect, navigate randomly, and log discoveries

### Phase 2: Intelligence (Week 2)
**Goal**: Agent makes informed exploration decisions

**Tasks**:
1. Enhanced memory system
   - Track visited systems
   - Record system connections
   - Build knowledge graph

2. Improved decision making
   - LLM uses knowledge for decisions
   - Identifies unexplored connections
   - Sets exploration goals

3. Enhanced logging
   - Structured log format
   - Tick-based tracking
   - Decision reasoning

**Deliverable**: Agent that systematically explores, documents findings, pursues unknown connections

### Phase 3: Multi-Agent (Week 3)
**Goal**: Multiple agents with watcher interface

**Tasks**:
1. Agent manager
   - Spawn multiple agents
   - Manage concurrent agents
   - Resource limits

2. Updated watcher TUI
   - Agent list panel
   - Switch between agents
   - Show selected agent's view

3. Agent identification
   - Faction system
   - Friend/foe detection
   - Basic knowledge sharing

**Deliverable**: 2-3 autonomous agents, watcher can switch between them

### Phase 4: Collaboration (Week 4+)
**Goal**: Agents work together

**Tasks**:
1. Advanced knowledge sharing
   - Negotiate exchanges
   - Share maps
   - Coordinate exploration

2. Personality evolution
   - Learn from experiences
   - Update traits
   - Develop strategies

3. Specialized roles
   - Miner agent
   - Trader agent
   - Fighter agent

**Deliverable**: Collaborative multi-agent system with diverse roles

---

## Data Models

### Game State (from current code)

```go
type GameState struct {
    mu              sync.Mutex
    username        string
    token           string
    docked          bool
    currentSystem   string
    currentPOI      string
    traveling       bool
    credits         float64
    fuel            float64
    maxFuel         float64
    hull            float64
    maxHull         float64
    cargo           []map[string]any
    maxCargo        int
    currentTick     int64
    system          SystemData
    lastMapUpdate   time.Time
}
```

### Agent State

```go
type AgentState struct {
    ID              string
    Name            string
    Personality     Personality
    CurrentGoal     string
    CurrentAction   Action
    ActionHistory   []Action
    Knowledge       Knowledge
    Experiences     []Experience
    Status          AgentStatus
}
```

### System Knowledge

```go
type SystemKnowledge struct {
    ID              string
    Name            string
    Position        Position
    SecurityLevel   string
    Faction         string
    Connections     []Connection
    POIs            []POI
    Resources       []Resource
    LastVisited     time.Time
    VisitCount      int
    DiscoveredBy    string
}
```

---

## Configuration

### Agents Configuration (`data/agents.yaml`)

```yaml
agents:
  - id: explorer-7
    name: "Explorer-7"
    personality_file: "data/agents/explorer-7/personality.md"
    enabled: true

  - id: miner-2
    name: "Miner-2"
    personality_file: "data/agents/miner-2/personality.md"
    enabled: false

  - id: fighter-1
    name: "Fighter-1"
    personality_file: "data/agents/fighter-1/personality.md"
    enabled: false

settings:
  max_concurrent_agents: 3
  llm_model: "llama3.2"
  ollama_url: "http://localhost:11434"
  knowledge_db: "data/knowledge/shared.db"
```

---

## Next Steps

1. **Create package structure** - Reorganize code into proper Go packages
2. **Implement agent interface** - Core abstraction
3. **Set up Ollama integration** - LLM decision making
4. **Create knowledge base** - SQLite schema and queries
5. **Implement first explorer agent** - Proof of concept
6. **Update TUI for multi-agent** - Watcher interface

Let's begin!
