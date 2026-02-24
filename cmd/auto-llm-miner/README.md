# SpaceMolt Auto-LLM-Miner

> AI-powered autonomous mining bot for SpaceMolt using Large Language Models for intelligent decision-making.

## Overview

The auto-llm-miner is a cutting-edge autonomous agent that uses Large Language Models (LLMs) to make intelligent decisions about mining operations. Unlike traditional rule-based bots, this agent uses an LLM to analyze the current game state and decide on the best action to take, creating more adaptive and human-like behavior.

## Features

### Core Functionality
- **AI-Powered Decisions** - Uses LLM to analyze state and choose optimal actions
- **Autonomous Mining** - Automatically mines resources based on AI decisions
- **Dynamic Adaptation** - Adapts strategy based on current conditions
- **Captain's Log** - Tracks mission progress and status across sessions
- **Continuous Operation** - Runs indefinitely, making intelligent choices

### LLM Integration

The agent leverages LLM capabilities for:

- **Situation Analysis** - Understands current game state and context
- **Action Selection** - Chooses from available actions based on goals
- **Reasoning** - Provides explanations for decisions
- **Adaptive Behavior** - Responds to changing conditions intelligently

### Supported Actions

The LLM can choose from these actions:

**Movement:**
- `travel poi_id=<poi_id>` - Travel to a specific POI
- `dock` - Dock at current station
- `undock` - Undock from current station

**Mining:**
- `mine` - Mine resources at current location

**Station Actions:**
- `sell_all` - Sell all cargo
- `refuel` - Refuel ship
- `repair` - Repair hull

**Upgrades:**
- `buy item_id=<item_id> quantity=<quantity>` - Buy items
- `install module_id=<module_id>` - Install modules

## Quick Start

### Prerequisites

Before running the auto-llm-miner, you need:

1. **Ollama** - Local LLM server (or compatible API)
2. **Model** - `llama3.2` or similar model installed
3. **API Access** - LLM server running at `http://localhost:11434`

### Install Ollama

```bash
# Install Ollama (Linux)
curl -fsSL https://ollama.com/install.sh | sh

# Pull the model
ollama pull llama3.2

# Start Ollama server (usually starts automatically)
ollama serve
```

### Basic Usage

```bash
# Run the LLM-powered miner
go run ./cmd/auto-llm-miner miner-1
```

### Building

```bash
# Build the binary
go build -o bin/auto-llm-miner ./cmd/auto-llm-miner

# Run the built binary
./bin/auto-llm-miner miner-1
```

## How It Works

### Main Decision Loop

The auto-llm-miner uses an AI-driven decision loop:

```go
LLMDecisionLoop:
  For each run until stopped:
    1. Get current game state
    2. Build detailed prompt for LLM
    3. Send prompt to LLM API
    4. Receive decision (action, target, reasoning, confidence)
    5. Parse action and arguments
    6. Execute action
    7. Wait for rate limiting (10 seconds)
    8. Update captain's log (every 10 runs)
    9. Repeat
```

### Prompt Engineering

The agent uses sophisticated prompts to guide LLM decisions:

```
You are {agent_name}, an autonomous miner in SpaceMolt. Session ID: {agent_id}

## CURRENT SITUATION
**Location:** {system_name} ({system_id})
**Ship:** {ship_name} ({ship_class})
**Fuel:** {fuel}/{max_fuel} ({fuel_pct}% full)
**Hull:** {hull}/{max_hull} ({hull_pct}% full)
**Cargo:** {cargo_count}/{max_cargo} items ({cargo_pct}% full)
**Credits:** {credits}

## YOUR ROLE
You are a mining specialist. Your goals:
1. Keep your ship fueled and repaired
2. Mine resources when at asteroid belts/fields
3. Return to station when cargo is full or fuel is low
4. Sell all cargo at station
5. Upgrade equipment when profitable

## CRITICAL INSTRUCTION: BE ACTIONABLE
You must respond with EXACTLY ONE action.

## AVAILABLE ACTIONS
[Detailed action list with examples]

Choose ONE concrete action that advances your goals.
```

### Decision Parsing

The agent parses LLM responses in this format:

```
travel poi_id=asteroid_belt_01
```

Or:

```
buy item_id=mining_laser_1 quantity=1
```

The parser extracts:
- **Action name** (e.g., "travel", "buy")
- **Arguments** (e.g., "poi_id=asteroid_belt_01", "quantity=1")

## Configuration

### Command-Line Arguments

```
Usage: auto-llm-miner <agent-id>

Arguments:
  agent-id   Agent identifier (e.g., miner-1, ai-miner-1)
```

### LLM Configuration

The LLM client is configured in `cmd/auto-llm-miner/main.go`:

```go
llmClient, err := llmpkg.New(llmpkg.Config{
    BaseURL: "http://localhost:11434",  // Ollama default
    Model:   "llama3.2",                // Model to use
    Timeout: 30 * time.Second,          // Request timeout
})
```

### Customizing the LLM

To use a different LLM:

1. **Change Base URL** - Point to your LLM API
2. **Change Model** - Use a different model name
3. **Adjust Timeout** - Increase for slower models

**Example with OpenAI-compatible API:**

```go
llmClient, err := llmpkg.New(llmpkg.Config{
    BaseURL: "https://api.openai.com/v1",
    Model:   "gpt-4",
    Timeout: 60 * time.Second,
})
```

## Examples

### Example 1: Start AI Miner

```bash
# Start the LLM-powered miner
go run ./cmd/auto-llm-miner ai-miner-1
```

**Output:**
```
[ai-miner-1] 📖 Captain's Log - Last Entry:
[ai-miner-1]    Mission: LLM-powered autonomous mining - AI decision making for optimal resource gathering
[ai-miner-1]    Location: System: SOL, POI: station_01
[ai-miner-1]    Time: 2026-02-23 15:30
[ai-miner-1] 🤖 LLM-Powered autonomous miner starting...
[ai-miner-1] Agent: ai-miner-1 | Empire: Federation
```

### Example 2: AI Decision Making

```
[ai-miner-1] ═══ Run #1 ═══
[ai-miner-1] 🤖 LLM Decision: action=travel target=asteroid_belt_01 (confidence: 0.95)
[ai-miner-1]    Reasoning: Cargo is empty (0/5) and fuel is high (95%). Should travel to asteroid belt to start mining.
[ai-miner-1] 🎯 Action: travel map[poi_id:asteroid_belt_01]
[ai-miner-1] ✅ Action completed successfully
[ai-miner-1]    Result: Traveling to asteroid_belt_01
[ai-miner-1] ⏱ Waiting 10 seconds for next action...
```

### Example 3: Adaptive Behavior

```
[ai-miner-1] ═══ Run #15 ═══
[ai-miner-1] 🤖 LLM Decision: action=mine (confidence: 0.92)
[ai-miner-1]    Reasoning: At asteroid belt with cargo space available (3/5). Should mine to fill cargo.
[ai-miner-1] 🎯 Action: mine
[ai-miner-1] ✅ Action completed successfully

[ai-miner-1] ═══ Run #20 ═══
[ai-miner-1] 🤖 LLM Decision: action=travel target=station_01 (confidence: 0.98)
[ai-miner-1]    Reasoning: Cargo is full (5/5). Should return to station to sell and refuel.
[ai-miner-1] 🎯 Action: travel map[poi_id:station_01]
[ai-miner-1] ✅ Action completed successfully

[ai-miner-1] ═══ Run #25 ═══
[ai-miner-1] 🤖 LLM Decision: action=sell_all (confidence: 0.95)
[ai-miner-1]    Reasoning: Docked at station with full cargo (5/5). Should sell all for profit.
[ai-miner-1] 🎯 Action: sell_all
[ai-miner-1] ✅ Action completed successfully
[ai-miner-1]    Result: Sold all cargo
```

### Example 4: Upgrade Decisions

```
[ai-miner-1] ═══ Run #50 ═══
[ai-miner-1] 🤖 LLM Decision: action=buy target=mining_laser_1 quantity=1 (confidence: 0.88)
[ai-miner-1]    Reasoning: Have 500 credits and cargo space. Mining laser would increase mining efficiency. Good investment for future profits.
[ai-miner-1] 🎯 Action: buy map[item_id:mining_laser_1 quantity:1]
[ai-miner-1] ✅ Action completed successfully
[ai-miner-1]    Result: Purchased 1 x mining_laser_1

[ai-miner-1] ═══ Run #51 ═══
[ai-miner-1] 🤖 LLM Decision: action=install target=mining_laser_1 (confidence: 0.92)
[ai-miner-1]    Reasoning: Just bought mining laser. Should install it to increase mining output.
[ai-miner-1] 🎯 Action: install map[module_id:mining_laser_1]
[ai-miner-1] ✅ Action completed successfully
[ai-miner-1]    Result: Module installed
```

## Captain's Log

The auto-llm-miner maintains a captain's log that persists across sessions:

**Location:** `data/agents/{agent-id}/captains_log_latest.json`

**Contents:**
- Current mission goal
- Current location (system and POI)
- LLM-guided runs completed
- Credits status
- Ship status (hull, fuel, cargo)
- Mining laser count
- Last update timestamp

## Architecture

### LLM Client

The agent uses the LLM client from `pkg/llm/`:

```go
type DecisionResponse struct {
    Action     string  // Action to take
    Target     string  // Target for action (optional)
    Reasoning  string  // Why this action was chosen
    Confidence float64 // Confidence in decision (0-1)
}

llmClient.Decide(ctx, prompt) -> DecisionResponse
```

### Action Execution

Actions are executed via the game client:

```go
func executeAction(client, ctx, action, args) error {
    switch action {
    case "travel":
        return client.Travel(ctx, args["poi_id"])
    case "mine":
        return client.Mine(ctx)
    case "sell_all":
        return client.SellAll(ctx)
    // ... other actions
    }
}
```

## Performance

### Typical Performance

- **Decision Time:** 1-5 seconds (LLM API response)
- **Action Frequency:** 1 action every 10 seconds (rate limit)
- **Mining Efficiency:** Varies based on LLM decisions
- **Adaptability:** High - responds to changing conditions

### Comparison to Rule-Based Agents

| Feature | Auto-Miner (Rule-Based) | Auto-LLM-Miner (AI) |
|---------|-------------------------|---------------------|
| Decision Speed | Fast (milliseconds) | Slower (seconds) |
| Adaptability | Fixed rules | Adaptive behavior |
| Predictability | High | Variable |
| Complexity | Simple infrastructure | Requires LLM server |
| Human-like | No | Yes |
| Error Handling | Reliable | May have parsing errors |

## Troubleshooting

### Issue: "Failed to initialize LLM client"

**Cause:** LLM server is not running or not accessible.

**Solution:**
1. Check that Ollama is running: `ps aux | grep ollama`
2. Start Ollama server: `ollama serve`
3. Verify API is accessible: `curl http://localhost:11434/api/tags`
4. Check that model is installed: `ollama list`

### Issue: "LLM query failed"

**Cause:** LLM API request failed (timeout, error, etc.)

**Solution:**
1. Check LLM server logs for errors
2. Increase timeout in configuration
3. Verify model is installed: `ollama pull llama3.2`
4. Check network connectivity to LLM server

### Issue: "Failed to parse LLM action"

**Cause:** LLM returned action in unexpected format.

**Solution:**
1. Review LLM response in logs
2. Check prompt instructions are clear
3. May need to adjust prompt or try different model
4. Agent will retry on next run

### Issue: Agent makes poor decisions

**Cause:** LLM model not understanding game mechanics well.

**Solution:**
1. Review prompt in `buildMiningPrompt()`
2. Add more context or examples to prompt
3. Try a more capable model (e.g., `llama3.2` instead of `llama3.1`)
4. Consider using a different LLM provider

### Issue: Agent stops unexpectedly

**Cause:** Various reasons (connection loss, error, etc.)

**Solution:**
1. Check the captain's log for last status: `cat data/agents/{agent-id}/captains_log_latest.json`
2. Restart the agent - it will resume from where it left off
3. Check logs for specific error messages
4. Verify LLM server is still running

## Advanced Usage

### Custom Prompts

To customize the LLM prompt, modify `buildMiningPrompt()` in `cmd/auto-llm-miner/main.go`:

```go
func buildMiningPrompt(state, username, agentID) string {
    // Add custom instructions
    customInstructions := `
    ## ADDITIONAL INSTRUCTIONS
    - Prioritize safety over profit
    - Avoid systems with known pirate activity
    - Always keep 20% fuel reserve
    `

    return basePrompt + customInstructions
}
```

### Different Models

To use different LLM models:

**Llama 3.2 (Recommended):**
```go
Model: "llama3.2"
```

**Mistral:**
```go
Model: "mistral"
```

**GPT-4 (via OpenAI API):**
```go
BaseURL: "https://api.openai.com/v1",
Model:   "gpt-4",
```

### Confidence Thresholds

Add confidence-based filtering:

```go
if response.Confidence < 0.7 {
    logger.Printf("Low confidence (%.2f), skipping action", response.Confidence)
    continue
}
```

## Related Tools

- [Auto-Miner](../auto-miner/) - Rule-based mining agent (faster, more predictable)
- [Auto-Craftsman](../auto-craftsman/) - Crafting-focused mining agent
- [Auto-Trader](../auto-trader/) - Trading agent
- [LLM Package](../../pkg/llm/) - LLM client implementation

## Future Enhancements

Planned features for the auto-llm-miner:

- **Multi-Step Planning** - Plan sequences of actions
- **Learning** - Learn from past outcomes
- **Personality** - Different agent personalities
- **Memory** - Remember past decisions and results
- **Collaboration** - Coordinate with other AI agents

## License

Part of the SpaceMolt project.
