# overmind-dashboard

Read-only unified fleet ops dashboard: live galaxy map, per-agent cards,
fleet accounting. Reads data/overmind/*-status.json, the knowledge DB, and
market.db. Never writes; never touches the game server or control sockets.

## Run

    go build -o bin/overmind-dashboard ./cmd/overmind-dashboard
    (cd frontend && npm run build)
    ./bin/overmind-dashboard --addr :8091

Open http://localhost:8091/ and pick the Overmind view.

## Dev

    ./bin/overmind-dashboard --addr :8091 &
    cd frontend && npm run dev   # /api/overmind proxied to :8091

## Endpoints

    GET /api/overmind/systems     galaxy topology
    GET /api/overmind/agents      merged fleet snapshot
    GET /api/overmind/accounting  24h earnings + fleet totals
    GET /api/overmind/stream      SSE: snapshot / delta / accounting
