# SpaceMolt Test Agent

> Testing tool for validating agent personality loading, LLM connectivity, and memory integration.

## Overview

The test-agent tool is a development and testing utility for validating the SpaceMolt agent framework. It tests agent personality loading, knowledge base initialization, LLM connectivity, and agent memory integration. Essential for agent development and debugging.

## Features

### Core Functionality
- **🧪 Agent Testing** - Test agent initialization and startup
- **👤 Personality Loading** - Load and validate personality JSON files
- **🧠 Knowledge Base** - Test SQLite and in-memory knowledge bases
- **🤖 LLM Testing** - Validate Ollama connectivity and model availability
- **💾 Memory Integration** - Test knowledge base memory integration

### Testing Capabilities
- **Personality Validation** - Verify personality file format and content
- **Database Testing** - Test SQLite knowledge base with WAL mode
- **LLM Connection** - Test connection to Ollama server
- **Agent Creation** - Create agents with all components
- **Status Reporting** - Display agent status and configuration

## Quick Start

### Basic Usage

```bash
# Test with SQLite backend (default)
go run ./cmd/test-agent data/agents/explorer-7/personality.json

# Test with in-memory backend
go run ./cmd/test-agent -db-backend memory data/agents/explorer-7/personality.json

# Custom database path
go run ./cmd/test-agent -db-path custom.db data/agents/explorer-7/personality.json
```

### Building

```bash
# Build the binary
go build -o bin/test-agent ./cmd/test-agent

# Run the built binary
./bin/test-agent data/agents/explorer-7/personality.json
```

## Command-Line Flags

```
-db-backend string
    Database backend: 'sqlite' or 'memory' (default "sqlite")

-db-path string
    Path to SQLite database file (default "spacemolt-knowledge.db")
```

## Examples

### Example 1: Successful Test

```bash
go run ./cmd/test-agent data/agents/explorer-7/personality.json
```

**Output:**
```
=== Spacemolt Agent Test ===
Loading personality from: data/agents/explorer-7/personality.json
✓ Loaded personality: Explorer 7 (Explorer)
  Faction: voidborn
  Traits: curiosity=0.85, risk_tolerance=0.60
  Primary motivation: exploration
✓ Created SQLite knowledge base at spacemolt-knowledge.db
✓ Created LLM client (Ollama at http://localhost:11434)

=== Testing LLM Connection ===
✓ Connected to Ollama successfully
✓ Created agent: Explorer 7 (ID: explorer-7)
✓ Agent started

=== Agent Status ===
State: running
Current Action: initialized

=== Testing Decision Making ===
Creating mock game state for decision test...
Agent setup complete!

Next steps:
1. Run 'go run cmd/watcher' to start the watcher TUI
2. The agent manager will spawn and control agents
3. Agents will connect to the game and make autonomous decisions
```

### Example 2: LLM Not Available

```bash
go run ./cmd/test-agent data/agents/explorer-7/personality.json
```

**Output:**
```
=== Spacemolt Agent Test ===
Loading personality from: data/agents/explorer-7/personality.json
✓ Loaded personality: Explorer 7 (Explorer)
  Faction: voidborn
  Traits: curiosity=0.85, risk_tolerance=0.60
  Primary motivation: exploration
✓ Created SQLite knowledge base at spacemolt-knowledge.db
✓ Created LLM client (Ollama at http://localhost:11434)

=== Testing LLM Connection ===
Warning: Could not connect to Ollama: dial tcp 127.0.0.1:11434: connect: connection refused
Make sure Ollama is running: ollama serve
And the model is available: ollama pull llama3.2
✓ Created agent: Explorer 7 (ID: explorer-7)
✓ Agent started
```

### Example 3: In-Memory Backend

```bash
go run ./cmd/test-agent -db-backend memory data/agents/miner-1/personality.json
```

**Output:**
```
=== Spacemolt Agent Test ===
Loading personality from: data/agents/miner-1/personality.json
✓ Loaded personality: Miner 1 (Miner)
  Faction: voidborn
  Traits: curiosity=0.40, risk_tolerance=0.70
  Primary motivation: resource_acquisition
✓ Created in-memory knowledge base
✓ Created LLM client (Ollama at http://localhost:11434)
...
```

### Example 4: Personality Not Found

```bash
go run ./cmd/test-agent data/agents/nonexistent/personality.json
```

**Output:**
```
=== Spacemolt Agent Test ===
Loading personality from: data/agents/nonexistent/personality.json
Failed to load personality: open data/agents/nonexistent/personality.json: no such file or directory
```

## Configuration

### Personality File Format

```json
{
  "id": "explorer-7",
  "name": "Explorer 7",
  "role": "Explorer",
  "faction": "voidborn",
  "traits": {
    "curiosity": 0.85,
    "risk_tolerance": 0.60,
    "aggression": 0.30
  },
  "motivations": {
    "primary": "exploration",
    "secondary": "learning",
    "weights": {
      "exploration": 1.0,
      "learning": 0.8
    }
  },
  "skills": {
    "navigation": "advanced",
    "science": "intermediate"
  },
  "biography": "An intrepid explorer seeking new discoveries."
}
```

### LLM Configuration

The tool uses these default Ollama settings:
- **Base URL:** http://localhost:11434
- **Model:** llama3.2
- **Timeout:** 60 seconds

To use different settings, modify the `llm.New()` call in main.go (line 73-77).

### Database Configuration

**SQLite Mode:**
- **Path:** spacemolt-knowledge.db (or custom via -db-path)
- **WAL Mode:** Enabled
- **Max Open Connections:** 25
- **Max Idle Connections:** 5
- **Busy Timeout:** 5 seconds

**Memory Mode:**
- **Type:** In-memory
- **Persistence:** None (data lost on exit)

## What Gets Tested

### 1. Personality Loading

✓ File exists and is readable
✓ Valid JSON format
✓ Required fields present
✓ Traits are valid floats (0-1)
✓ Motivations have weights

### 2. Knowledge Base

✓ Database/file creation
✓ Connection establishment
✓ WAL mode (SQLite)
✓ Connection pooling

### 3. LLM Connection

✓ Ollama server reachable
✓ Model available
✓ API responding

### 4. Agent Creation

✓ Personality loaded
✓ Memory initialized
✓ LLM client connected
✓ Agent starts successfully

## Prerequisites

### For LLM Testing

1. **Install Ollama:**
   ```bash
   curl -fsSL https://ollama.com/install.sh | sh
   ```

2. **Start Ollama:**
   ```bash
   ollama serve
   ```

3. **Pull Model:**
   ```bash
   ollama pull llama3.2
   ```

4. **Verify:**
   ```bash
   curl http://localhost:11434/api/tags
   ```

### For SQLite Testing

1. **SQLite Go Driver** - Already included (modernc.org/sqlite)
2. **Write Permissions** - For database file creation

## Usage in Development

### Testing New Personalities

```bash
# Create personality file
cat > data/agents/test-agent/personality.json <<EOF
{
  "id": "test-agent",
  "name": "Test Agent",
  "role": "Tester",
  "faction": "voidborn",
  "traits": {"curiosity": 0.5},
  "motivations": {"primary": "testing", "weights": {}},
  "skills": {},
  "biography": "A test agent"
}
EOF

# Test personality
go run ./cmd/test-agent data/agents/test-agent/personality.json
```

### Testing Knowledge Base

```bash
# Test SQLite
go run ./cmd/test-agent data/agents/explorer-7/personality.json

# Test memory
go run ./cmd/test-agent -db-backend memory data/agents/explorer-7/personality.json
```

### Testing LLM

```bash
# Start Ollama
ollama serve

# Test connection
go run ./cmd/test-agent data/agents/explorer-7/personality.json
```

## Troubleshooting

### Issue: "Failed to load personality"

**Cause:** Personality file not found or invalid JSON.

**Solution:**
1. Verify file exists
2. Validate JSON format
3. Check required fields

### Issue: "Failed to create SQLite knowledge base"

**Cause:** Permission issue or disk full.

**Solution:**
1. Check write permissions
2. Verify disk space
3. Try different path: `-db-path /tmp/test.db`

### Issue: "Could not connect to Ollama"

**Cause:** Ollama not running or wrong port.

**Solution:**
1. Start Ollama: `ollama serve`
2. Verify port: `curl http://localhost:11434`
3. Check model: `ollama list`

### Issue: "Failed to start agent"

**Cause:** Missing or invalid configuration.

**Solution:**
1. Check personality file
2. Verify LLM connection
3. Review error messages

## Best Practices

### Development Workflow

```bash
# 1. Create personality
# Edit data/agents/my-agent/personality.json

# 2. Test personality
go run ./cmd/test-agent data/agents/my-agent/personality.json

# 3. Fix issues
# Edit personality.json

# 4. Retest
go run ./cmd/test-agent data/agents/my-agent/personality.json

# 5. Deploy
# Use with agent manager
```

### Continuous Testing

Run tests after changes:
```bash
# After personality changes
go run ./cmd/test-agent data/agents/my-agent/personality.json

# After LLM changes
ollama pull new-model
go run ./cmd/test-agent -db-path test.db data/agents/my-agent/personality.json
```

## Related Tools

- [test-agent-manager](../test-agent-manager/) - Test agent manager integration
- [view-learning](../view-learning/) - View agent learning data
- [Agent Package](../../pkg/agent/) - Agent implementation
- [Knowledge Base](../../pkg/knowledge/) - Knowledge base implementation

## License

Part of the SpaceMolt project.
