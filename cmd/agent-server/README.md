# Spacemolt Agent Server

The Spacemolt Agent Server is the main entry point for running multiple autonomous game-playing agents. It handles agent discovery, spawning, lifecycle management, and graceful shutdown.

## Features

- **Flexible Agent Selection**: Multiple methods to specify which agents to start
- **Priority-Based Configuration**: CLI flags > Environment > Config file > Auto-discover
- **Robust Error Handling**: Retries connection/login up to 3 times with exponential backoff
- **Credential Management**: Supports multiple credential backends (file, SQLite, keyring)
- **Knowledge Base**: SQLite or in-memory storage for agent knowledge
- **LLM Integration**: Connects to Ollama for agent decision-making
- **Graceful Shutdown**: Handles SIGINT/SIGTERM to stop all agents cleanly

## Usage

### Basic Usage

Start all agents found in `data/agents/`:

```bash
./agent-server
```

### Specify Agents

**Option 1: CLI Flag (Highest Priority)**
```bash
./agent-server --agents=miner-2,explorer-7,trader-1
```

**Option 2: Environment Variable**
```bash
export SPACEMOLT_AGENTS=miner-2,explorer-7
./agent-server
```

**Option 3: Configuration File**

Create `agents_config.yaml`:
```yaml
agents:
  enabled:
    - miner-2
    - explorer-7
    - trader-1
```

Then run:
```bash
./agent-server --config=agents_config.yaml
```

**Option 4: Auto-Discovery (Lowest Priority)**

Automatically starts all agents with valid `personality.json` files in the agents directory:
```bash
./agent-server --agents-dir=data/agents
```

## Configuration Options

### Server Settings

```bash
--server-url string         Game server WebSocket URL (default "wss://game.spacemolt.com/ws")
--agents-dir string         Directory containing agent personalities (default "data/agents")
--config string            Path to configuration file (default "agents_config.yaml")
--max-agents int           Maximum concurrent agents (default 10)
--decision-interval duration  Decision interval for agents (default 5s)
```

### Knowledge Base

```bash
--db-backend string        Backend: sqlite or memory (default "sqlite")
--db-path string          Path to SQLite database (default "data/spacemolt-knowledge.db")
```

### LLM Configuration

```bash
--llm-url string          LLM server URL (default "http://localhost:11434")
--llm-model string        LLM model name (default "llama3.2")
```

### Credentials

```bash
--creds-backend string    Backend: file, sqlite, or keyring (default "file")
--creds-path string       Path for credentials storage (default "data/credentials")
```

For SQLite credentials backend, set the passphrase:
```bash
export SPACEMOLT_PASSPHRASE=your-secure-passphrase
./agent-server --creds-backend=sqlite --creds-path=data/creds.db
```

## Agent Directory Structure

Each agent must have a directory under `data/agents/` with a `personality.json` file:

```
data/agents/
├── miner-2/
│   ├── personality.json
│   └── credentials.json (optional, created on registration)
├── explorer-7/
│   ├── personality.json
│   └── credentials.json
└── trader-1/
    ├── personality.json
    └── credentials.json
```

## Exit Behavior

- **Partial Failures**: If some agents fail to start but at least one succeeds, the server continues running
- **Total Failure**: If ALL agents fail to start, the server exits with error code 1
- **Graceful Shutdown**: Press Ctrl+C to stop all agents and exit cleanly

## Error Handling

The agent server implements robust retry logic:

- **Connection Retries**: Up to 3 attempts with exponential backoff (2s, 4s, 8s)
- **Login Retries**: Up to 3 attempts with exponential backoff
- **Credential Fallback**: Tries primary provider first, falls back to file storage

Failed agents are logged with details, and the server continues attempting to start other agents.

## Examples

### Run specific agents with custom LLM:

```bash
./agent-server \
  --agents=miner-2,explorer-7 \
  --llm-url=http://localhost:11434 \
  --llm-model=mistral
```

### Use SQLite for everything:

```bash
export SPACEMOLT_PASSPHRASE=my-secure-key
./agent-server \
  --db-backend=sqlite \
  --db-path=data/game-kb.db \
  --creds-backend=sqlite \
  --creds-path=data/creds.db
```

### Memory-only mode (for testing):

```bash
./agent-server \
  --agents=test-agent \
  --db-backend=memory \
  --creds-backend=file
```

## Building

```bash
go build -o agent-server ./cmd/agent-server/
```

## Testing

```bash
go test ./cmd/agent-server/
```

## See Also

- [Agent Startup Architecture](../../docs/agent_startup.md) - Complete architecture documentation
- [API Commands Reference](../../docs/api_commands_reference.md) - Game API documentation
- Example configuration: [agents_config.yaml.example](../../agents_config.yaml.example)
