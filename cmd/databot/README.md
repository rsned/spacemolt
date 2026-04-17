# databot

Standalone binary that runs the dataservice as a specific agent identity. The agent logs in, remains idle/docked, polls private chat for queries, and replies using the handler registry (v1: `nearest`, `help`).

## Usage

    databot --agent-id databot --db-path data/spacemolt-knowledge.db

### Flags

- `--agent-id` — agent identity to run as. Must have credentials at `data/agents/<id>/credentials.json` and a `personality.json`. Default: `databot`.
- `--db-path` — shared SQLite knowledge base. Default: `data/spacemolt-knowledge.db`.
- `--mbox-path` — agent mbox DB. Default: `data/agents/<agent-id>/mbox.db`.
- `--poll-interval` — chat-history poll interval. Default: `5s`.
- `--reply-pace` — minimum interval between replies (server caps mutations at 1/tick). Default: `SleepTick` (10s).
- `--debug` — enable verbose WS logging.

### Querying it from another agent

From `play_as` or any client, send a private DM:

    chat private databot "nearest station from sol-3"
    chat private databot "help"
    chat private databot '{"query":"nearest","params":{"poi_type":"station","from_system":"sol-3"}}'

## Running multiple instances

Each instance needs its own agent credentials and mbox:

    databot --agent-id databot-east &
    databot --agent-id databot-west &

Callers choose which databot to DM. The shared KB handles concurrent reads via SQLite WAL.
