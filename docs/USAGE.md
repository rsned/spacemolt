# Spacemolt Usage Guide

How to build, configure, and run the Spacemolt unified server and frontends.

## Prerequisites

- Go 1.24+
- Node.js (for frontend development)
- An LLM server (e.g., [Ollama](https://ollama.com)) running locally or remotely

## Quick Start

```bash
# 1. Build the frontend
cd frontend
npm install
npm run build
cd ..

# 2. Run the unified server (uses defaults if no config file exists)
go run ./cmd/spacemolt-server

# 3. Open the UI
#    Main frontend:  http://localhost:8090/
#    Admin dashboard: http://localhost:8090/admin/  (if configured)
```

## Building

### Server

Build the unified server binary:

```bash
go build -o bin/spacemolt-server ./cmd/spacemolt-server
```

Or build with the race detector for development:

```bash
go build -race -o bin/spacemolt-server-race ./cmd/spacemolt-server
```

### Frontend

The React/TypeScript frontend must be built before the server can serve it:

```bash
cd frontend
npm install
npm run build    # Outputs to frontend/dist/
```

For frontend development with hot-reload:

```bash
cd frontend
npm run dev      # Starts Vite dev server with proxy to localhost:8090
```

The Vite dev server automatically proxies `/api` and `/ws` requests to the backend at `localhost:8090`, so you can develop the frontend and backend simultaneously.

## Configuration

The server reads configuration from a YAML file (default: `spacemolt-server.yaml` in the working directory). If the file is not found, sensible defaults are used.

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `spacemolt-server.yaml` | Path to configuration file |
| `-port` | (from config or `8090`) | Override HTTP port |

### Configuration File

```yaml
# spacemolt-server.yaml

server:
  http_port: 8090                    # HTTP server port
  static_dir: "frontend/dist"       # Path to built frontend files
  admin_static_dir: ""               # Path to admin UI files (optional)

game:
  server_url: "wss://game.spacemolt.com/ws"  # Game server WebSocket URL

agents:
  dir: "data/agents"                 # Agent personality definitions
  max: 50                            # Maximum concurrent agents
  decision_interval: "11s"           # Agent decision loop interval
  enabled:                           # Agents to start automatically
    - explorer7
    - miner2

llm:
  url: "http://localhost:11434"      # LLM server URL (Ollama default)
  model: "llama3.2"                  # LLM model name
  timeout: "60s"                     # LLM request timeout

database:
  backend: "sqlite"                  # "sqlite" or "memory"
  path: "data/spacemolt-knowledge.db"

credentials:
  backend: "file"                    # "file", "sqlite", or "env"
  path: "data/credentials"           # Credentials storage location

strategies:
  defaults: {}                       # Default strategy assignments per agent

teams:                               # Pre-configured teams
  - name: "Alpha Squad"
    leader: "explorer7"
    members:
      - agent_id: "miner2"
        role: "resource_gatherer"
```

### Minimal Configuration

To get started quickly, you only need a config with your agents enabled:

```yaml
agents:
  enabled:
    - explorer7
```

Everything else uses sensible defaults.

## Running the Server

```bash
# Using defaults (looks for spacemolt-server.yaml in current directory)
go run ./cmd/spacemolt-server

# With a specific config file
go run ./cmd/spacemolt-server -config /path/to/config.yaml

# Override port
go run ./cmd/spacemolt-server -port 9000

# Or run the compiled binary
./bin/spacemolt-server -config spacemolt-server.yaml
```

## URL Paths and Endpoints

### Frontend URLs

| URL | Description |
|-----|-------------|
| `http://localhost:8090/` | Main frontend (React SPA) |
| `http://localhost:8090/admin/` | Admin monitoring dashboard (requires `admin_static_dir` in config) |

During frontend development:

| URL | Description |
|-----|-------------|
| `http://localhost:5173/` | Vite dev server with hot-reload (proxies API calls to `:8090`) |

### WebSocket

| URL | Description |
|-----|-------------|
| `ws://localhost:8090/ws` | Real-time updates from agents and game state |

### REST API Endpoints

#### Agent Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/agents` | List all connected agents with status |
| `POST` | `/api/agents` | Connect a new agent (`{"username": "..."}`) |
| `DELETE` | `/api/agents/{username}` | Disconnect an agent |

#### Game Data

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/systems` | List game systems |
| `GET` | `/api/systems/{id}` | Get system details |
| `GET` | `/api/systems/{id}/pois` | Get points of interest in a system |
| `GET` | `/api/bases/{id}` | Get base details |
| `GET` | `/api/pois/{id}/base` | Get base by POI |
| `GET` | `/api/skills` | Get skill catalog |
| `GET` | `/api/skills/{id}` | Get skill details |
| `GET` | `/api/catalog/items` | Get item catalog |

#### Strategy Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/strategies` | List available strategies |
| `GET` | `/api/agents/{id}/strategy` | Get agent's current strategy |
| `PUT` | `/api/agents/{id}/strategy` | Assign strategy (`{"strategy": "...", "parameters": {...}}`) |

#### Team Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/teams` | List all teams |
| `POST` | `/api/teams` | Create a team |
| `GET` | `/api/teams/{id}` | Get team details with status |
| `PUT` | `/api/teams/{id}` | Update a team |
| `DELETE` | `/api/teams/{id}` | Disband a team |
| `POST` | `/api/teams/{id}/orders` | Issue order to team |
| `GET` | `/api/teams/{id}/log` | Get team action log (`?limit=100`) |

#### Admin / Monitoring

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/admin/health` | Server health status |
| `GET` | `/api/admin/metrics` | Comprehensive metrics (server, agents, LLM, KB) |
| `GET` | `/api/admin/agents` | Agent-specific metrics |
| `GET` | `/api/admin/llm` | LLM health status |
| `GET` | `/api/admin/knowledge` | Knowledge base statistics |

## Development Workflow

### Running Backend + Frontend Together

**Option A: Production-like (single server)**

```bash
cd frontend && npm run build && cd ..
go run ./cmd/spacemolt-server
# Visit http://localhost:8090/
```

**Option B: Development (with hot-reload)**

Terminal 1 — backend:
```bash
go run ./cmd/spacemolt-server
```

Terminal 2 — frontend dev server:
```bash
cd frontend
npm run dev
# Visit http://localhost:5173/ (auto-proxies API to :8090)
```

### Running Checks

```bash
make test          # Run tests
make test-race     # Run tests with race detector
make lint          # Run golangci-lint
make vet           # Run go vet
make check-all     # Run all checks
```

## Troubleshooting

**Frontend shows blank page at `localhost:8090/`**
- Ensure you've built the frontend: `cd frontend && npm run build`
- Check that `static_dir` in your config points to the correct `frontend/dist` path

**Cannot connect agents**
- Verify the game server URL in your config (`game.server_url`)
- Ensure agent credentials exist in the configured credentials path

**LLM-powered agents not making decisions**
- Confirm your LLM server is running (default: `http://localhost:11434`)
- Check the configured model is available: `ollama list`

**Admin dashboard returns 404**
- The admin UI requires `server.admin_static_dir` to be set in config, pointing to a directory with built admin frontend files
