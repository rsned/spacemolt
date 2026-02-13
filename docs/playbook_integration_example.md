# Playbook Integration Example

This document shows how to integrate the career playbooks into LLM-powered agents.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                                                          │
│  ┌──────────────┐  Playbooks        ┌──────────────┐ │
│  │   Miner.md   │                  │   Explorer.md │ │
│  └──────────────┘                  └──────────────┘ │
│                    │                            │          │
│                    ▼                            ▼          │
│  ┌─────────────────────────────────────────────────────┐  │
│  │         Playbook Loader & Formatter             │  │
│  └─────────────────────────────────────────────────────┘  │
│                    │                            │          │
│                    ▼                            ▼          │
│  ┌─────────────────────────────────────────────────────┐  │
│  │           LLM Client (llm.Client)               │  │
│  │  - Loads playbook content                    │  │
│  │  - Formats with game state                  │  │
│  │  - Calls LLM for decisions                  │  │
│  └─────────────────────────────────────────────────────┘  │
│                    │                            │          │
│                    ▼                            ▼          │
│  ┌─────────────────────────────────────────────────────┐  │
│  │         auto-miner (Agent)                   │  │
│  │  - Gets game state                           │  │
│  │  - Formats playbook context               │  │
│  │  - Executes LLM decision                     │  │
│  │  - Parses action from LLM                    │  │
│  └─────────────────────────────────────────────────────┘  │
│                    │                            │          │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Pattern

### 1. Playbook Content Structure

Playbooks contain structured strategic knowledge:

```markdown
# SpaceMolt Career Playbook: Miner

## Core Strategy Loop

### 1. Initial Assessment
- Check current status: `get_status()`
- Verify ship class, cargo capacity, fuel level, hull integrity
- Note available credits and current system

### 2. Upgrade Planning
Before each mining run, assess upgrade opportunities:

**Credit Thresholds:**
- Tier 1 (300+ credits): Basic mining laser
- Tier 2 (800+ credits): Better mining laser or cargo expansion
- Tier 3 (2000+ credits): Upgrade to mining_enhanced (Drillship)
...
```

### 2. Playbook Loader

```go
package playbook

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PlaybookLoader handles loading career-specific playbooks
type PlaybookLoader struct {
	playbookDir string
}

// New creates a new playbook loader
func New(dir string) *PlaybookLoader {
	return &PlaybookLoader{
		playbookDir: dir,
	}
}

// Load reads playbook content for specified career
func (p *PlaybookLoader) Load(career string) (string, error) {
	// Normalize career name
	career = strings.ToLower(strings.ReplaceAll(career, "-", "_"))

	// Try .md file first
	path := filepath.Join(p.playbookDir, career+".md")
	content, err := os.ReadFile(path)
	if err == nil {
		return string(content), nil
	}

	// Fallback to embedded content (for production builds)
	return p.loadEmbedded(career)
}

// loadEmbedded loads playbook from embedded FS
func (p *PlaybookLoader) loadEmbedded(career string) (string, error) {
	// Would use embed.FS for production
	return "", fmt.Errorf("playbook not found: %s", career)
}

// FormatContext creates the formatted prompt for LLM
func (p *PlaybookLoader) FormatContext(career string, gameState GameState) string {
	playbook, _ := p.Load(career)

	return fmt.Sprintf(`You are a SpaceMolt autonomous agent following the %s career path.

%s

Current Game State:
- Credits: %.2f
- Ship: %s (%s)
- Cargo: %.1f/%.1f
- Fuel: %.0f/%.0f
- Hull: %.0f/%.0f
- System: %s
- Docked: %v

Make decisions based on playbook strategies and your current situation.
Provide your next action as a structured command.
`,
		strings.Title(career),
		playbook,
		gameState.Credits,
		gameState.Ship.Name,
		gameState.Ship.ClassID,
		gameState.Ship.CargoUsed,
		gameState.Ship.CargoCapacity,
		gameState.Fuel,
		gameState.MaxFuel,
		gameState.Hull,
		gameState.MaxHull,
		gameState.System.Name,
		gameState.Docked,
	)
}
```

### 3. LLM Integration with Playbook

```go
package agent

import (
	"context"
	"fmt"
	"log"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/llm"
	"github.com/rsned/spacemolt/pkg/playbook"
)

// LLMMinerAgent uses LLM + playbooks for autonomous mining
type LLMMinerAgent struct {
	client      *game.Client
	llmClient   *llm.Client
	playbook    *playbook.PlaybookLoader
	logger      *log.Logger
	ctx         context.Context
	career     string
}

// New creates a new LLM-powered miner agent
func NewLLMMiner(agentID string, logger *log.Logger, ctx context.Context) (*LLMMinerAgent, error) {
	// Initialize game client
	client, _, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize game client: %w", err)
	}

	// Initialize LLM client
	llmClient := llm.New(llm.Config{
		BaseURL: "http://localhost:11434",
		Model:   "llama3.2",
		Timeout: 60 * time.Second,
	})

	// Initialize playbook loader
	playbookLoader := playbook.New("playbook/")

	return &LLMMinerAgent{
		client:    client,
		llmClient: llmClient,
		playbook:  playbookLoader,
		logger:    logger,
		ctx:       ctx,
		career:    "miner",
	}, nil
}

// Run executes the main agent loop
func (a *LLMMinerAgent) Run() error {
	a.logger.Printf("🤖 Starting LLM-powered autonomous miner...")

	// Wait for connection and login
	<-a.client.Ready()
	time.Sleep(2 * time.Second)

	// Main decision loop
	for {
		select {
		case <-a.ctx.Done():
			return nil
		default:
		}

		// Get current game state
		state := a.client.GetState()

		// Format playbook context with current state
		prompt := a.playbook.FormatContext(a.career, GameState{
			Credits:     state.Credits,
			Ship:        state.Ship,
			Fuel:        state.Fuel,
			MaxFuel:     state.MaxFuel,
			Hull:         state.Hull,
			MaxHull:     state.MaxHull,
			System:       state.System,
			Docked:       state.Docked,
			Cargo:        state.Ship.Cargo,
		})

		// Query LLM for decision
		response, err := a.llmClient.Decision(a.ctx, llm.DecisionRequest{
			Prompt:      prompt,
			Temperature: 0.7, // Balance creativity vs consistency
			MaxTokens:    2048,
		})
		if err != nil {
			a.logger.Printf("❌ LLM query failed: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		a.logger.Printf("🤖 LLM Decision: %s", response.Content)

		// Parse and execute action
		if err := a.executeAction(response.Content); err != nil {
			a.logger.Printf("❌ Action execution failed: %v", err)
		}

		// Wait for action to complete (rate limiting)
		time.Sleep(12 * time.Second)
	}
}

// executeAction parses and executes the action from LLM
func (a *LLMMinerAgent) executeAction(content string) error {
	// Parse structured command from LLM response
	// Expected format: "COMMAND arg1=value1 arg2=value2"

	parts := strings.Fields(content)
	if len(parts) == 0 {
		return fmt.Errorf("empty action")
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "mine":
		return a.client.Mine(a.ctx)

	case "travel":
		if len(parts) < 2 {
			return fmt.Errorf("travel requires POI ID")
		}
		poiID := parts[1]
		return a.client.Travel(a.ctx, poiID)

	case "dock":
		return a.client.Dock(a.ctx)

	case "undock":
		return a.client.Undock(a.ctx)

	case "sell_all":
		return a.client.SellAllBulk(a.ctx, nil)

	case "refuel":
		return a.client.Refuel(a.ctx)

	case "repair":
		return a.client.Repair(a.ctx)

	case "buy":
		if len(parts) < 3 {
			return fmt.Errorf("buy requires item_id and quantity")
		}
		itemID := parts[1]
		var quantity float64
		_, err := fmt.Sscanf(parts[2], "%f", &quantity)
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return a.client.Buy(a.ctx, itemID, quantity)

	case "install":
		if len(parts) < 2 {
			return fmt.Errorf("install requires module_id")
		}
		moduleID := parts[1]
		return a.client.Install(a.ctx, moduleID)

	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}
```

## LLM Response Format

The LLM should respond with structured commands:

```
# Example LLM Response

Based on the Miner playbook and current state (Credits: 450, Cargo: 45/50, Fuel: 85/100):

## Current Situation Analysis
- Cargo nearly full (97%)
- Sufficient fuel for return trip
- Should return to station and sell

## Next Action
dock poi_id="station_sol_01"
```

The agent parses this and executes: `client.Dock(ctx, "station_sol_01")`

## Decision Flow Example

### Scenario 1: Beginning New Run

```
Game State:
- Credits: 450
- Cargo: 0/50 (empty)
- Fuel: 100/100 (full)
- Hull: 100/100 (full)
- Docked: true

Playbook Context + Game State → LLM Decision:
"undock" → Wait 12s → "travel asteroid_belt_sol_01" → Wait 20s → "mine"
```

### Scenario 2: Full Cargo

```
Game State:
- Credits: 450
- Cargo: 48/50 (97% full)
- Fuel: 60/100
- Ore iron: 40 units
- Ore copper: 8 units

Playbook Context + Game State → LLM Decision:
"travel station_sol_01" → Wait 20s → "dock" → Wait 15s → "sell_all ore_iron 40" → "sell_all ore_copper 8"
```

### Scenario 3: Upgrade Available

```
Game State:
- Credits: 2150 (above TIER2_THRESHOLD)
- Cargo: 0/50 (empty)
- Ship: starter_mining (2 utility slots)
- Docked: true
- Mining lasers installed: 1

Playbook Context + Game State → LLM Decision:
"buy mining_laser_1 1" → Wait 3s → "install mining_laser_1" → Wait 10s → "buy mining_laser_1 1" → Wait 3s → "install mining_laser_1"
```

## Template System

For production builds, playbooks can be embedded:

```go
//go:embed all *.md files in playbook directory
//go:embed playbook/*.md

type embedFS struct {
	playbookMiner    embed.FS
	playbookExplorer embed.FS
	playbookFighter  embed.FS
	// ... others
}

func (e *embedFS) Load(career string) (string, error) {
	switch career {
	case "miner":
		data, _ := e.playbookMiner.ReadFile("miner.md")
		return string(data), nil
	case "explorer":
		data, _ := e.playbookExplorer.ReadFile("explorer.md")
		return string(data), nil
	// ... etc
	default:
		return "", fmt.Errorf("unknown career: %s", career)
	}
}
```

## Advanced: Multi-Turn Reasoning

For complex decisions, LLM can plan multiple actions:

```go
type ActionPlan struct {
	Reasoning   string
	Actions     []Action
	NextAction  Action
}

type Action struct {
	Command   string
	Arguments map[string]interface{}
	Priority  int
}

func (a *LLMMinerAgent) planActions() (*ActionPlan, error) {
	state := a.client.GetState()
	prompt := a.playbook.FormatContext(a.career, GameState{...})

	// Ask LLM for full plan (not just next action)
	response, err := a.llmClient.Decision(a.ctx, llm.DecisionRequest{
		Prompt: fmt.Sprintf("%s\n\nProvide a complete action plan for the next 3-5 actions. Format as JSON.", prompt),
		Format:   "json",
	})

	if err != nil {
		return nil, err
	}

	// Parse JSON response
	var plan ActionPlan
	if err := json.Unmarshal([]byte(response.Content), &plan); err != nil {
		return nil, err
	}

	a.logger.Printf("📋 Action Plan: %s", plan.Reasoning)
	for i, action := range plan.Actions {
		a.logger.Printf("  %d. %s", i+1, action.Command)
	}

	return &plan, nil
}
```

LLM JSON Response Example:
```json
{
  "reasoning": "Cargo is 97% full with high-value ore. Fuel sufficient for return trip. Should sell and then continue mining.",
  "actions": [
    {"command": "travel", "arguments": {"poi_id": "station_sol_01"}, "priority": 1},
    {"command": "dock", "arguments": {}, "priority": 2},
    {"command": "sell_all", "arguments": {}, "priority": 3},
    {"command": "refuel", "arguments": {}, "priority": 4},
    {"command": "undock", "arguments": {}, "priority": 5},
    {"command": "travel", "arguments": {"poi_id": "asteroid_belt_sol_01"}, "priority": 6}
  ],
  "next_action": {
    "command": "travel",
    "arguments": {"poi_id": "station_sol_01"}
  }
}
```

## Benefits of Playbook Integration

1. **Strategic Consistency**: Agent follows proven strategies from playbooks
2. **Adaptive Decision-Making**: LLM adapts playbook strategies to current game state
3. **Rapid Prototyping**: Update playbooks without recompiling agent code
4. **Career Switching**: Same agent framework, different playbook
5. **Knowledge Accumulation**: Playbooks capture player/community knowledge
6. **LLM Grounding**: Playbooks reduce hallucinations by providing concrete strategies

## Testing Playbook Integration

```bash
# Test playbook loading
go test ./pkg/playbook/...

# Run LLM agent with playbook
go run ./cmd/auto-llm-miner/ miner-1

# Compare with traditional agent
go run ./cmd/auto-miner/ miner-1
```
