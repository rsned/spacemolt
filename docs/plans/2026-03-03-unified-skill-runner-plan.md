# Unified Skill Runner Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace all `auto-*` binaries with a single `skill-runner` binary that resolves what to run from agent personality configs or CLI flags, supporting both YAML skills and Go strategy plugins.

**Architecture:** A `UnifiedRegistry` checks Go strategy registrations first, then falls back to YAML skill files via a `SkillStrategy` adapter. A `ChainStrategy` composes multiple skills into a sequence. The main loop handles error recovery, looping, and `CompositeStrategy` integration for background skills.

**Tech Stack:** Go 1.24+, existing `pkg/strategy` (Strategy interface, CompositeStrategy, BackgroundRunner) and `pkg/skills` (Executor, ClientDispatcher, Registry)

---

### Task 1: SkillStrategy Adapter

Wraps `skills.Executor` to implement the `strategy.Strategy` interface, bridging YAML skills into the strategy system.

**Files:**
- Create: `pkg/strategy/skill_strategy.go`
- Create: `pkg/strategy/skill_strategy_test.go`

**Step 1: Write the failing test**

Create `pkg/strategy/skill_strategy_test.go`:

```go
package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestSkillStrategyName(t *testing.T) {
	ss := &SkillStrategy{
		skillName: "mine",
	}
	if got := ss.Name(); got != "mine" {
		t.Errorf("Name() = %q, want %q", got, "mine")
	}
}

func TestSkillStrategyDescription(t *testing.T) {
	ss := &SkillStrategy{
		skillName:   "mine",
		description: "Mine resources from asteroid belt",
	}
	if got := ss.Description(); got != "Mine resources from asteroid belt" {
		t.Errorf("Description() = %q, want %q", got, "Mine resources from asteroid belt")
	}
}

func TestSkillStrategyCurrentStatus(t *testing.T) {
	ss := &SkillStrategy{
		skillName: "mine",
	}
	status := ss.CurrentStatus()
	if status == "" {
		t.Error("expected non-empty status")
	}
}

func TestSkillStrategyImplementsInterface(t *testing.T) {
	var _ Strategy = &SkillStrategy{}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/strategy/ -run TestSkillStrategy -v`
Expected: FAIL — `SkillStrategy` type not defined.

**Step 3: Write minimal implementation**

Create `pkg/strategy/skill_strategy.go`:

```go
package strategy

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/skills"
)

// SkillStrategy adapts a YAML skill into the Strategy interface. It wraps
// a skills.Executor to run a named skill from the YAML skill registry.
type SkillStrategy struct {
	skillName   string
	description string
	registry    *skills.Registry
	dispatcher  skills.ActionDispatcher
	logger      *log.Logger
	params      map[string]string

	mu     sync.RWMutex
	status string
}

// SkillStrategyConfig holds the configuration for creating a SkillStrategy.
type SkillStrategyConfig struct {
	SkillName   string
	Description string
	Registry    *skills.Registry
	Dispatcher  skills.ActionDispatcher
	Logger      *log.Logger
	Params      map[string]string
}

// NewSkillStrategy creates a Strategy that runs a YAML skill.
func NewSkillStrategy(cfg SkillStrategyConfig) *SkillStrategy {
	return &SkillStrategy{
		skillName:   cfg.SkillName,
		description: cfg.Description,
		registry:    cfg.Registry,
		dispatcher:  cfg.Dispatcher,
		logger:      cfg.Logger,
		params:      cfg.Params,
	}
}

func (s *SkillStrategy) Name() string { return s.skillName }

func (s *SkillStrategy) Description() string {
	if s.description != "" {
		return s.description
	}
	return fmt.Sprintf("YAML skill: %s", s.skillName)
}

func (s *SkillStrategy) CurrentStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status != "" {
		return s.status
	}
	return fmt.Sprintf("skill:%s", s.skillName)
}

func (s *SkillStrategy) Run(ctx context.Context, _ *game.Client, _ Config) error {
	s.setStatus(fmt.Sprintf("running:%s", s.skillName))
	defer s.setStatus(fmt.Sprintf("idle:%s", s.skillName))

	executor := skills.NewExecutor(s.registry, s.dispatcher, s.logger)
	return executor.RunWithParams(ctx, s.skillName, s.params)
}

func (s *SkillStrategy) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/strategy/ -run TestSkillStrategy -v`
Expected: PASS (all 4 tests)

**Step 5: Run full test suite**

Run: `go test ./pkg/strategy/ -v`
Expected: All tests PASS

**Step 6: Commit**

```bash
git add pkg/strategy/skill_strategy.go pkg/strategy/skill_strategy_test.go
git commit -m "feat(strategy): add SkillStrategy adapter for YAML skills"
```

---

### Task 2: ChainStrategy

Runs multiple strategies in sequence. Used when `--skill mine,sell,refuel_repair` specifies a chain.

**Files:**
- Create: `pkg/strategy/chain.go`
- Create: `pkg/strategy/chain_test.go`

**Step 1: Write the failing test**

Create `pkg/strategy/chain_test.go`:

```go
package strategy

import (
	"context"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestChainStrategyName(t *testing.T) {
	a := &mockStrategy{name: "mine"}
	b := &mockStrategy{name: "sell"}
	chain := NewChainStrategy("mine+sell", a, b)

	if got := chain.Name(); got != "mine+sell" {
		t.Errorf("Name() = %q, want %q", got, "mine+sell")
	}
}

func TestChainStrategyRunsAllSteps(t *testing.T) {
	a := &mockStrategy{name: "step-a", runDelay: 10 * time.Millisecond}
	b := &mockStrategy{name: "step-b", runDelay: 10 * time.Millisecond}
	c := &mockStrategy{name: "step-c", runDelay: 10 * time.Millisecond}

	chain := NewChainStrategy("test-chain", a, b, c)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := chain.Run(ctx, nil, Config{AgentID: "test"})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, s := range []*mockStrategy{a, b, c} {
		s.mu.Lock()
		called := s.runCalled
		s.mu.Unlock()
		if !called {
			t.Errorf("expected %s to have run", s.name)
		}
	}
}

func TestChainStrategyCancellation(t *testing.T) {
	// First step takes a long time; chain should abort when context is canceled.
	slow := &mockStrategy{name: "slow", runDelay: 5 * time.Second}
	fast := &mockStrategy{name: "fast", runDelay: 10 * time.Millisecond}

	chain := NewChainStrategy("cancel-test", slow, fast)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = chain.Run(ctx, nil, Config{AgentID: "test"})

	fast.mu.Lock()
	fastCalled := fast.runCalled
	fast.mu.Unlock()

	if fastCalled {
		t.Error("fast step should NOT have run after cancellation")
	}
}

func TestChainStrategyImplementsInterface(t *testing.T) {
	var _ Strategy = &ChainStrategy{}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/strategy/ -run TestChainStrategy -v`
Expected: FAIL — `ChainStrategy` type not defined.

**Step 3: Write minimal implementation**

Create `pkg/strategy/chain.go`:

```go
package strategy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
)

// ChainStrategy runs multiple strategies in sequence.
type ChainStrategy struct {
	name  string
	steps []Strategy

	mu     sync.RWMutex
	status string
}

// NewChainStrategy creates a chain that runs the given strategies in order.
func NewChainStrategy(name string, steps ...Strategy) *ChainStrategy {
	return &ChainStrategy{
		name:  name,
		steps: steps,
	}
}

func (c *ChainStrategy) Name() string { return c.name }

func (c *ChainStrategy) Description() string {
	names := make([]string, len(c.steps))
	for i, s := range c.steps {
		names[i] = s.Name()
	}
	return fmt.Sprintf("Chain: %s", strings.Join(names, " → "))
}

func (c *ChainStrategy) CurrentStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.status != "" {
		return c.status
	}
	return "chain:idle"
}

func (c *ChainStrategy) Run(ctx context.Context, client *game.Client, cfg Config) error {
	for i, step := range c.steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		c.setStatus(fmt.Sprintf("chain[%d/%d]: %s", i+1, len(c.steps), step.Name()))

		if err := step.Run(ctx, client, cfg); err != nil {
			c.setStatus(fmt.Sprintf("chain error at %s: %v", step.Name(), err))
			return fmt.Errorf("chain step %q: %w", step.Name(), err)
		}
	}

	c.setStatus("chain:completed")
	return nil
}

func (c *ChainStrategy) setStatus(status string) {
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/strategy/ -run TestChainStrategy -v`
Expected: PASS (all 4 tests)

**Step 5: Run full test suite**

Run: `go test ./pkg/strategy/ -v`
Expected: All tests PASS

**Step 6: Commit**

```bash
git add pkg/strategy/chain.go pkg/strategy/chain_test.go
git commit -m "feat(strategy): add ChainStrategy for sequential skill execution"
```

---

### Task 3: UnifiedRegistry

Resolves skill/strategy names by checking Go strategy factories first, then falling back to YAML skills (via SkillStrategy adapter).

**Files:**
- Create: `pkg/strategy/unified_registry.go`
- Create: `pkg/strategy/unified_registry_test.go`

**Step 1: Write the failing test**

Create `pkg/strategy/unified_registry_test.go`:

```go
package strategy

import (
	"testing"
)

func TestUnifiedRegistryResolveGoStrategy(t *testing.T) {
	ur := NewUnifiedRegistry(nil)
	ur.RegisterGoStrategy("explore", func() Strategy {
		return &mockStrategy{name: "explore"}
	})

	s, err := ur.Resolve("explore")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if s.Name() != "explore" {
		t.Errorf("Name() = %q, want %q", s.Name(), "explore")
	}
}

func TestUnifiedRegistryResolveNotFound(t *testing.T) {
	ur := NewUnifiedRegistry(nil)
	_, err := ur.Resolve("nonexistent")
	if err == nil {
		t.Error("expected error for unknown strategy")
	}
}

func TestUnifiedRegistryHas(t *testing.T) {
	ur := NewUnifiedRegistry(nil)
	ur.RegisterGoStrategy("fight", func() Strategy {
		return &mockStrategy{name: "fight"}
	})

	if !ur.Has("fight") {
		t.Error("expected Has(fight) = true")
	}
	if ur.Has("nonexistent") {
		t.Error("expected Has(nonexistent) = false")
	}
}

func TestUnifiedRegistryNames(t *testing.T) {
	ur := NewUnifiedRegistry(nil)
	ur.RegisterGoStrategy("alpha", func() Strategy {
		return &mockStrategy{name: "alpha"}
	})
	ur.RegisterGoStrategy("beta", func() Strategy {
		return &mockStrategy{name: "beta"}
	})

	names := ur.Names()
	if len(names) < 2 {
		t.Fatalf("expected at least 2 names, got %d", len(names))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/strategy/ -run TestUnifiedRegistry -v`
Expected: FAIL — `UnifiedRegistry` type not defined.

**Step 3: Write minimal implementation**

Create `pkg/strategy/unified_registry.go`:

```go
package strategy

import (
	"fmt"
	"sort"
	"sync"

	"github.com/rsned/spacemolt/pkg/skills"
)

// StrategyFactory creates a new Strategy instance.
type StrategyFactory func() Strategy

// UnifiedRegistry resolves skill/strategy names to runnable Strategy instances.
// It checks Go strategy factories first, then falls back to YAML skills.
type UnifiedRegistry struct {
	yamlRegistry *skills.Registry
	goStrategies map[string]StrategyFactory
	mu           sync.RWMutex
}

// NewUnifiedRegistry creates a registry. Pass nil for yamlRegistry if not using YAML skills.
func NewUnifiedRegistry(yamlRegistry *skills.Registry) *UnifiedRegistry {
	return &UnifiedRegistry{
		yamlRegistry: yamlRegistry,
		goStrategies: make(map[string]StrategyFactory),
	}
}

// RegisterGoStrategy adds a Go strategy factory.
func (r *UnifiedRegistry) RegisterGoStrategy(name string, factory StrategyFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.goStrategies[name] = factory
}

// Resolve returns a Strategy for the given name.
// Priority: Go strategies first, then YAML skills.
func (r *UnifiedRegistry) Resolve(name string) (Strategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check Go strategies first.
	if factory, ok := r.goStrategies[name]; ok {
		return factory(), nil
	}

	// Check YAML skills.
	if r.yamlRegistry != nil && r.yamlRegistry.Has(name) {
		// Return a placeholder — the caller must set up the SkillStrategy
		// with a dispatcher. We return a marker so they know it's YAML.
		return nil, fmt.Errorf("yaml:%s (use ResolveSkill for YAML skills)", name)
	}

	return nil, fmt.Errorf("unknown strategy or skill: %q", name)
}

// Has returns true if the name can be resolved (Go or YAML).
func (r *UnifiedRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, ok := r.goStrategies[name]; ok {
		return true
	}
	if r.yamlRegistry != nil && r.yamlRegistry.Has(name) {
		return true
	}
	return false
}

// IsGoStrategy returns true if the name is a registered Go strategy.
func (r *UnifiedRegistry) IsGoStrategy(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.goStrategies[name]
	return ok
}

// Names returns all available strategy and skill names, sorted.
func (r *UnifiedRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var names []string

	for name := range r.goStrategies {
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}

	if r.yamlRegistry != nil {
		for _, name := range r.yamlRegistry.Names() {
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}

	sort.Strings(names)
	return names
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/strategy/ -run TestUnifiedRegistry -v`
Expected: PASS (all 4 tests)

**Step 5: Run full test suite**

Run: `go test ./pkg/strategy/ -v`
Expected: All tests PASS

**Step 6: Commit**

```bash
git add pkg/strategy/unified_registry.go pkg/strategy/unified_registry_test.go
git commit -m "feat(strategy): add UnifiedRegistry for YAML + Go strategy resolution"
```

---

### Task 4: Add PrimarySkill to Personality

Add a `PrimarySkill` field to the `agent.Personality` struct so personality configs can declare what skill/strategy to run.

**Files:**
- Modify: `pkg/agent/agent.go:44-56`
- Test: `go test ./pkg/agent/ -v` (existing tests should still pass)

**Step 1: Add the field**

In `pkg/agent/agent.go`, add `PrimarySkill` to the `Personality` struct (line 54, after `ServiceName`):

```go
// In the Personality struct, add after ServiceName:
PrimarySkill    string             `yaml:"primary_skill,omitempty" json:"primary_skill,omitempty"`
```

The struct should now have these fields (relevant excerpt):
```go
ServiceName     string             `yaml:"service_name,omitempty" json:"service_name,omitempty"`
PrimarySkill    string             `yaml:"primary_skill,omitempty" json:"primary_skill,omitempty"`
GameSkills      []string           `yaml:"game_skills,omitempty" json:"game_skills,omitempty"`
BackgroundSkill string             `yaml:"background_skill,omitempty" json:"background_skill,omitempty"`
```

**Step 2: Run existing tests**

Run: `go test ./pkg/agent/ -v`
Expected: All existing tests PASS (adding a field is backward-compatible)

**Step 3: Run build**

Run: `go build ./...`
Expected: BUILD SUCCESS

**Step 4: Commit**

```bash
git add pkg/agent/agent.go
git commit -m "feat(agent): add PrimarySkill field to Personality struct"
```

---

### Task 5: Create mine_and_sell Compound Skill

A compound YAML skill for miners: mine until cargo full → dock → sell all → refuel/repair. This is what miner agents will use as their `primary_skill`.

**Files:**
- Create: `data/skills/mine_and_sell.yaml`

**Step 1: Create the skill YAML**

Reference existing skills for patterns:
- `data/skills/mine_and_deposit.yaml` — similar compound (mine → deposit)
- `data/skills/mine.yaml` — the mining sub-skill
- `data/skills/sell.yaml` — the sell sub-skill
- `data/skills/refuel_repair.yaml` — maintenance sub-skill

Create `data/skills/mine_and_sell.yaml`:

```yaml
name: mine_and_sell
description: >
  Mine resources from a nearby asteroid belt until cargo is full or fuel is low,
  return to station, sell all cargo, and refuel/repair. Designed as a continuous
  mining loop for miner agents.

prerequisites:
  - docked OR at_poi_type(asteroid_belt, asteroid_field)
  - has_module_type(mining)

targets:
  mining_site:
    poi_type: [asteroid_belt, asteroid_field]
    description: Resource extraction location
  home_station:
    poi_type: [station]
    description: Station for selling and refueling

outputs:
  - docked

steps:
  - id: run_mine
    skill: mine
    next: sell

  - id: sell
    skill: sell
    next: refuel

  - id: refuel
    skill: refuel_repair
    next: done

  - id: done
    terminal: true
```

**Step 2: Validate the skill loads**

Run: `go run cmd/tools/run-skill/main.go miner-1 mine_and_sell --once 2>&1 | head -5`

(This requires a live game server. If not available, validate by loading the registry:)

Run: `go test ./pkg/skills/ -run TestLoadRegistry -v`
Expected: PASS (registry loads all YAMLs including the new one)

If there is no existing registry loading test, add a quick validation:

```bash
go run -e - <<'EOF'
package main
import (
    "fmt"
    "github.com/rsned/spacemolt/pkg/skills"
)
func main() {
    r, err := skills.LoadRegistry("data/skills")
    if err != nil { panic(err) }
    if !r.Has("mine_and_sell") { panic("mine_and_sell not found") }
    fmt.Println("OK: mine_and_sell loaded")
}
EOF
```

**Step 3: Commit**

```bash
git add data/skills/mine_and_sell.yaml
git commit -m "feat(skills): add mine_and_sell compound skill for miner agents"
```

---

### Task 6: Update Agent Personality Configs

Add `primary_skill` to agent personality files. For Phase 1, update the agents whose behavior maps cleanly to existing YAML skills.

**Files:**
- Modify: `data/agents/miner-*/personality.json` (10 files) — add `"primary_skill": "mine_and_sell"`
- Modify: `data/agents/assist-*/personality.json` (5 files) — add `"primary_skill": "scan_for_distress"` (already have `background_skill`)
- Modify: `data/agents/salvager-*/personality.json` (10 files) — add `"primary_skill": "ensure_docked"` (stub)
- Modify: `data/agents/pirate-*/personality.json` (15 files) — add `"primary_skill": "ensure_docked"` (stub)

**Step 1: Update miner personalities**

For each `data/agents/miner-{1..10}/personality.json`, add after `"role": "Miner",`:

```json
"primary_skill": "mine_and_sell",
"game_skills": ["mine", "sell", "refuel_repair", "mine_and_sell"],
```

Use a script:
```bash
for i in $(seq 1 10); do
    file="data/agents/miner-$i/personality.json"
    # Add primary_skill and game_skills using jq
    jq '. + {"primary_skill": "mine_and_sell", "game_skills": ["mine", "sell", "refuel_repair", "mine_and_sell"]}' "$file" > "$file.tmp" && mv "$file.tmp" "$file"
done
```

**Step 2: Update assist personalities**

The assist agents already have `background_skill` and `game_skills`. Add `primary_skill`:

```bash
for agent in assist-frontier assist-haven assist-krynn assist-nexus assist-sol; do
    file="data/agents/$agent/personality.json"
    jq '. + {"primary_skill": "scan_for_distress"}' "$file" > "$file.tmp" && mv "$file.tmp" "$file"
done
```

**Step 3: Update salvager and pirate personalities (stubs)**

```bash
for i in $(seq 1 10); do
    file="data/agents/salvager-$i/personality.json"
    jq '. + {"primary_skill": "ensure_docked", "game_skills": ["ensure_docked"]}' "$file" > "$file.tmp" && mv "$file.tmp" "$file"
done

for i in $(seq 1 15); do
    file="data/agents/pirate-$i/personality.json"
    jq '. + {"primary_skill": "ensure_docked", "game_skills": ["ensure_docked"]}' "$file" > "$file.tmp" && mv "$file.tmp" "$file"
done
```

**Step 4: Verify JSON is valid**

```bash
for f in data/agents/*/personality.json; do
    jq . "$f" > /dev/null || echo "INVALID: $f"
done
```

**Step 5: Commit**

```bash
git add data/agents/*/personality.json
git commit -m "feat(agents): add primary_skill to miner, assist, salvager, pirate personalities"
```

---

### Task 7: Skill Runner Binary

The main unified binary. This is the core deliverable.

**Files:**
- Create: `cmd/skill-runner/main.go`

**Step 1: Create the binary**

Create `cmd/skill-runner/main.go`:

```go
// Command skill-runner is the unified agent binary that replaces all auto-*
// binaries. It connects an agent to the game server and runs skills defined
// in YAML or registered as Go strategies.
//
// Usage:
//
//	skill-runner --agent <agent-id> [--skill <skills>] [--once] [--debug]
//
// Examples:
//
//	skill-runner --agent miner-1                          # Run from personality config
//	skill-runner --agent miner-1 --skill mine,sell        # Override with specific skills
//	skill-runner --agent assist-sol                       # Primary + background from personality
//	skill-runner --agent miner-1 --skill mine --once      # Single run, no loop
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/skills"
	"github.com/rsned/spacemolt/pkg/strategy"
)

func main() {
	agentID := flag.String("agent", "", "Agent ID (required)")
	skillFlag := flag.String("skill", "", "Comma-separated skill/strategy names (overrides personality)")
	once := flag.Bool("once", false, "Run skill chain once instead of looping")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *agentID == "" {
		fmt.Fprintln(os.Stderr, "Usage: skill-runner --agent <agent-id> [--skill <skills>] [--once] [--debug]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  skill-runner --agent miner-1")
		fmt.Fprintln(os.Stderr, "  skill-runner --agent miner-1 --skill mine,sell")
		fmt.Fprintln(os.Stderr, "  skill-runner --agent miner-1 --skill mine --once")
		os.Exit(1)
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", *agentID), log.LstdFlags)

	// Load personality config
	personality, err := loadPersonality(*agentID)
	if err != nil {
		logger.Fatalf("Failed to load personality: %v", err)
	}
	logger.Printf("Agent: %s | Role: %s", personality.Name, personality.Role)

	// Determine which skills to run
	var skillNames []string
	if *skillFlag != "" {
		skillNames = strings.Split(*skillFlag, ",")
	} else if personality.PrimarySkill != "" {
		skillNames = []string{personality.PrimarySkill}
	} else {
		logger.Fatalf("No --skill flag and no primary_skill in personality config")
	}

	// Load YAML skill registry
	yamlRegistry, err := skills.LoadRegistry("data/skills")
	if err != nil {
		logger.Fatalf("Failed to load skill registry: %v", err)
	}

	// Create unified registry
	registry := strategy.NewUnifiedRegistry(yamlRegistry)
	// TODO: Register Go strategies here as they are extracted from auto-* binaries
	// e.g., registry.RegisterGoStrategy("explore", strategy.NewExplorerStrategyFactory)

	// Validate all skill names
	for _, name := range skillNames {
		if !registry.Has(name) {
			logger.Fatalf("Unknown skill/strategy %q. Available: %v", name, registry.Names())
		}
	}
	logger.Printf("Skills: %s", strings.Join(skillNames, " → "))

	// Set up context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("Received %v, shutting down...", sig)
		cancel()
	}()

	// Connect to game server
	logger.Printf("Initializing agent %s...", *agentID)
	client, _, err := game.InitializeAgent(*agentID, logger, ctx, *debug)
	if err != nil {
		logger.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() { _ = client.Close() }()

	time.Sleep(game.SleepQuick)

	// Create dispatcher for YAML skills
	dispatcher := skills.NewClientDispatcher(client, "data", *agentID, logger)
	dispatcher.EnsureSystemData(ctx)

	// Extract agent params from personality
	agentParams := make(map[string]string)
	if personality.ServiceName != "" {
		agentParams["service_name"] = personality.ServiceName
	}

	// Build the primary strategy
	primary, err := buildStrategy(skillNames, registry, yamlRegistry, dispatcher, logger, agentParams)
	if err != nil {
		logger.Fatalf("Failed to build strategy: %v", err)
	}

	// Wrap with CompositeStrategy if background skill is configured
	var runnable strategy.Strategy = primary
	if personality.BackgroundSkill != "" && registry.Has(personality.BackgroundSkill) {
		bgSkill := strategy.NewSkillStrategy(strategy.SkillStrategyConfig{
			SkillName:  personality.BackgroundSkill,
			Registry:   yamlRegistry,
			Dispatcher: dispatcher,
			Logger:     logger,
			Params:     agentParams,
		})
		composite := strategy.NewCompositeStrategy(primary, bgSkill, strategy.CompositeConfig{
			CleanupTimeout: 30 * time.Second,
			Logger:         logger,
		})
		runnable = composite
		logger.Printf("Background skill: %s", personality.BackgroundSkill)
	}

	// Captain's log
	game.WriteCaptainsLog("data", *agentID, fmt.Sprintf("Skill runner started: %s", strings.Join(skillNames, " → ")))

	// Main loop
	cfg := strategy.Config{
		AgentID:    *agentID,
		Logger:     logger,
		Parameters: agentParams,
	}

	failCount := 0
	failWindow := time.Now()

	for {
		if ctx.Err() != nil {
			break
		}

		logger.Printf("═══ Starting skill chain ═══")
		err := runnable.Run(ctx, client, cfg)

		if ctx.Err() != nil {
			break
		}

		if err != nil {
			logger.Printf("Strategy error: %v", err)
			failCount++

			if time.Since(failWindow) > 5*time.Minute {
				failCount = 1
				failWindow = time.Now()
			}

			if failCount >= 3 {
				logger.Printf("Multiple failures, backing off...")
				time.Sleep(game.SleepLong)
			} else {
				time.Sleep(game.SleepReconnect)
			}
			continue
		}

		if *once {
			logger.Printf("═══ Chain completed (--once mode) ═══")
			break
		}

		logger.Printf("═══ Chain completed, restarting ═══")
		time.Sleep(game.SleepShort)
	}

	game.WriteCaptainsLog("data", *agentID, "Skill runner stopped")
	logger.Printf("Shutdown complete")
}

func loadPersonality(agentID string) (*agent.Personality, error) {
	path := filepath.Join("data", "agents", agentID, "personality.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var p agent.Personality
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return &p, nil
}

func buildStrategy(
	skillNames []string,
	registry *strategy.UnifiedRegistry,
	yamlRegistry *skills.Registry,
	dispatcher skills.ActionDispatcher,
	logger *log.Logger,
	params map[string]string,
) (strategy.Strategy, error) {
	var strategies []strategy.Strategy

	for _, name := range skillNames {
		if registry.IsGoStrategy(name) {
			s, err := registry.Resolve(name)
			if err != nil {
				return nil, fmt.Errorf("resolving Go strategy %q: %w", name, err)
			}
			strategies = append(strategies, s)
		} else {
			// YAML skill — get description from skill definition
			desc := fmt.Sprintf("YAML skill: %s", name)
			if skill := yamlRegistry.Get(name); skill != nil {
				desc = skill.Description
			}
			s := strategy.NewSkillStrategy(strategy.SkillStrategyConfig{
				SkillName:   name,
				Description: desc,
				Registry:    yamlRegistry,
				Dispatcher:  dispatcher,
				Logger:      logger,
				Params:      params,
			})
			strategies = append(strategies, s)
		}
	}

	if len(strategies) == 1 {
		return strategies[0], nil
	}

	chainName := strings.Join(skillNames, "+")
	return strategy.NewChainStrategy(chainName, strategies...), nil
}
```

**Step 2: Verify it builds**

Run: `go build ./cmd/skill-runner/`
Expected: BUILD SUCCESS

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: All tests PASS

**Step 4: Commit**

```bash
git add cmd/skill-runner/main.go
git commit -m "feat: add unified skill-runner binary replacing auto-* binaries"
```

---

### Task 8: Update Launch Script

Update `scripts/launch-agents.sh` to use `skill-runner` for agents that have been migrated (miner, assist, salvager, pirate). Keep existing `auto-*` binaries for complex agents (trader, explorer, fighter, prophet, random, craftsman, engineer, recall).

**Files:**
- Modify: `scripts/launch-agents.sh`

**Step 1: Update the binary resolution function**

In `scripts/launch-agents.sh`, replace the `get_binary_for_role()` function (lines 56-59) with:

```bash
# Roles that have been migrated to skill-runner
SKILL_RUNNER_ROLES="miner assist salvager pirate"

# Get binary name for a role
get_binary_for_role() {
    local role=$1
    if echo "$SKILL_RUNNER_ROLES" | grep -qw "$role"; then
        echo "skill-runner"
    else
        echo "auto-${role}"
    fi
}

# Get args for launching an agent
get_agent_args() {
    local agent=$1
    local role=$(get_agent_role "$agent")
    if echo "$SKILL_RUNNER_ROLES" | grep -qw "$role"; then
        echo "--agent $agent"
    else
        echo "$agent"
    fi
}
```

**Step 2: Update the launch command**

In the `start)` case (around line 123), change:

```bash
(cd "$START_DIR" && ./bin/$binary $agent > logs/$agent.log 2>&1 &)
```

to:

```bash
args=$(get_agent_args "$agent")
(cd "$START_DIR" && ./bin/$binary $args > logs/$agent.log 2>&1 &)
```

**Step 3: Update the process detection**

Update the `pgrep` patterns (lines 118, 134, 140, 144) to also match `skill-runner`:

Line 118 (already running check):
```bash
if pgrep -f "(auto-|skill-runner).* $agent" > /dev/null; then
```

Lines 134, 140, 144 (stop/status):
```bash
pgrep -f "bin/(auto-|skill-runner)" ...
pkill -f "bin/(auto-|skill-runner)" ...
```

**Step 4: Update the build section**

If the script has a `rebuild` case, add `skill-runner` to the build list:

```bash
go build -o bin/skill-runner cmd/skill-runner/main.go
```

**Step 5: Test the script**

```bash
bash scripts/launch-agents.sh status
```

**Step 6: Commit**

```bash
git add scripts/launch-agents.sh
git commit -m "feat(scripts): update launch-agents.sh to use skill-runner for migrated agents"
```

---

### Task 9: Build, Test, Lint

Final verification that everything compiles, tests pass, and lint is clean.

**Files:**
- None (verification only)

**Step 1: Build all binaries**

Run: `go build ./...`
Expected: BUILD SUCCESS

**Step 2: Run all tests**

Run: `go test ./...`
Expected: All tests PASS

**Step 3: Run linter**

Run: `golangci-lint run ./...`
Expected: No new findings

**Step 4: Verify skill-runner builds standalone**

Run: `go build -o bin/skill-runner cmd/skill-runner/main.go`
Expected: Binary created at `bin/skill-runner`

**Step 5: Commit any lint fixes if needed**

```bash
git add -A
git commit -m "chore: fix lint issues from unified skill runner"
```
