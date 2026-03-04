# Unified Skill Runner Design

## Goal

Replace all `auto-*` binaries with a single `skill-runner` binary that resolves what to run from agent personality configs or CLI flags, supporting both YAML skills and Go strategy plugins.

## Architecture

The unified runner uses a **hybrid resolution model**: a `UnifiedRegistry` that checks Go strategy registrations first, then falls back to YAML skill files. This allows complex agents (trader, explorer, fighter) to keep their Go logic while simple agents (miner, assist, salvager) run entirely from YAML skill definitions. Over time, Go strategies can be replaced with YAML as the skill system matures.

**Tech Stack:** Go, existing `pkg/strategy` and `pkg/skills` packages, `data/skills/*.yaml`, `data/agents/*/personality.json`

---

## 1. Binary Interface

```
# Personality-driven (reads agent config for primary + background skills)
skill-runner --agent assist-sol

# Explicit skill chain override
skill-runner --agent miner-1 --skill mine,deposit_cargo

# Single-run mode (no loop)
skill-runner --agent miner-1 --skill mine --once

# Debug logging
skill-runner --agent miner-1 --debug
```

### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--agent` | Yes | — | Agent ID for credentials and personality |
| `--skill` | No | From personality | Comma-separated skill/strategy names |
| `--once` | No | `false` | Run chain once instead of looping |
| `--debug` | No | `false` | Enable verbose WS debug logging |

### Resolution Order

1. If `--skill` provided → use those skills
2. Otherwise → read `personality.json` → use `primary_skill` field
3. If personality has `background_skill` → wrap primary with `CompositeStrategy`

## 2. Unified Strategy Resolution

### UnifiedRegistry

Resolves a skill/strategy name to a runnable `Strategy`:

```go
type UnifiedRegistry struct {
    yamlRegistry *skills.Registry
    goStrategies map[string]StrategyFactory
}

type StrategyFactory func(client *game.Client, logger *log.Logger) Strategy

func (r *UnifiedRegistry) Resolve(name string, client *game.Client, logger *log.Logger) (Strategy, error)
```

Resolution priority: Go strategies first (they may internally use YAML skills), then YAML skills.

### SkillStrategy Adapter

Wraps `skills.Executor` to implement the `Strategy` interface:

```go
type SkillStrategy struct {
    name     string
    executor *skills.Executor
    params   map[string]string
}

func (s *SkillStrategy) Name() string { return s.name }
func (s *SkillStrategy) Run(ctx context.Context, client *game.Client, cfg Config) error {
    return s.executor.RunWithParams(ctx, s.name, s.params)
}
```

### Go Strategy Registration

Complex behaviors register at package init time (same pattern as `database/sql` drivers):

```go
func init() {
    strategy.RegisterGoStrategy("explore", NewExplorerStrategy)
    strategy.RegisterGoStrategy("trade", NewTraderStrategy)
}
```

## 3. Chain & Loop Strategies

### ChainStrategy

Runs multiple strategies in sequence:

```go
type ChainStrategy struct {
    name  string
    steps []Strategy
}
```

When `--skill mine,sell,refuel_repair` is passed, the runner creates a `ChainStrategy` with three `SkillStrategy` steps.

### Main Loop

```
Initialize agent (connect, auth, load personality)
  → Resolve primary strategy (chain or single)
  → Optionally wrap with CompositeStrategy (if background_skill)
  → Loop:
      Run strategy
      If --once: exit
      If error: log, sleep SleepReconnect, retry
      If clean completion: sleep SleepShort, restart
      If 3 failures in 5 minutes: back off to SleepLong
  → SIGINT/SIGTERM: cancel context → graceful shutdown
```

### Captain's Log

The runner writes captain's log entries at startup, on each skill/strategy transition, and on shutdown.

## 4. Personality Config Changes

Add `primary_skill` field to personality JSON:

```json
{
  "id": "miner-1",
  "role": "Miner",
  "primary_skill": "mine_and_sell",
  "background_skill": "",
  "game_skills": ["mine", "sell", "refuel_repair"]
}
```

- `primary_skill`: Name resolved via UnifiedRegistry (YAML or Go)
- `background_skill`: Optional, creates CompositeStrategy wrapper (already exists for assist agents)
- `game_skills`: Full list of capabilities (informational, used by LLM agents)

## 5. Launch Script Update

```bash
# Before (per-role binary)
binary="bin/auto-${role}"
"$binary" "$agent_id" &

# After (unified binary)
binary="bin/skill-runner"
"$binary" --agent "$agent_id" &
```

Build simplification: one `go build` instead of 12+.

During migration, the script supports both modes via a flag or per-agent override to allow gradual rollout.

## 6. Migration Phases

### Phase 1 — Core Runner (this implementation)

Build the unified binary with:
- `UnifiedRegistry` (YAML + Go resolution)
- `SkillStrategy` adapter
- `ChainStrategy`
- Main loop with error recovery
- Personality-driven skill resolution
- `CompositeStrategy` integration (from concurrent skills work)

Port simple agents:
- Miner → `primary_skill: "mine_and_sell"` (new compound YAML skill)
- Salvager, Pirate → stubs, just need personality config
- Assist agents → already configured with `background_skill`

Update launch script for ported agents.

### Phase 2 — Extract Go Strategies (follow-up)

- Extract `auto-trader` → `pkg/strategy/trader.go`
- Extract `auto-explorer` → `pkg/strategy/explorer.go`
- Extract `auto-fighter` → `pkg/strategy/fighter.go`
- Extract `auto-prophet` → `pkg/strategy/prophet.go`
- Register in Go strategy registry
- Update personality configs

### Phase 3 — Full Migration (follow-up)

- All agents through `skill-runner`
- Remove `cmd/auto-*` directories
- Simplify build scripts

## 7. New Files

```
cmd/skill-runner/main.go              # Unified binary
pkg/strategy/unified_registry.go      # UnifiedRegistry
pkg/strategy/skill_strategy.go        # SkillStrategy (YAML → Strategy adapter)
pkg/strategy/chain.go                 # ChainStrategy
data/skills/mine_and_sell.yaml        # Compound skill for miners
```

## 8. Modified Files

```
pkg/agent/agent.go                    # Add PrimarySkill field to Personality
data/agents/*/personality.json        # Add primary_skill to all agent configs
scripts/launch-agents.sh              # Use skill-runner binary
```
