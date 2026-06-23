# market-dashboard

A standalone, **read-only** web dashboard over `data/market.db` for validating
market data collection. It renders a matrix of best buy/sell prices across
stations, a clickable cell-detail order book, a per-item price-over-time view,
and a capture-health view — all backed by direct reads from the market
collector.

No write path, no game connection: point it at a market SQLite DB and read.

## Run

```bash
# from the repo root
go build -o bin/market-dashboard ./cmd/market-dashboard
./bin/market-dashboard
```

Then open <http://localhost:8090>.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8090` | HTTP listen address. |
| `--market-db-path` | `data/market.db` | Path to the market SQLite database. |

Example against a specific DB on a custom port:

```bash
./bin/market-dashboard --market-db-path /path/to/market.db --addr :8099
```

The UI is embedded in the binary (`//go:embed web`), so there is **no separate
frontend build step** and no external asset files to serve — one binary is the
whole app.

## Views

- **Matrix** — items × stations. Each cell shows the station's best sell (green,
  cheapest ask), best buy (orange, highest bid), sell volume, and freshness
  ("5 min ago") from that station's latest capture. Sparse cells (an item not
  traded at a station) render `—`. Filter by category or search by item id/name;
  click any cell to open its order book.
- **Cell detail** — the full order book (side / price / qty / source) for the
  clicked item×station, from the latest capture.
- **Price over time** — per-item OHLCV buckets (VWAP / high / low / volume /
  trade count) across stations, newest first. VWAP is the robust series; high/low
  reveal thin-item noise. Enter an item id (e.g. `iron_ore`) and click **Show**.
- **Capture health** — per-station capture timestamps (newest first), count, and
  earliest/latest, to spot cadence gaps in collection.

## HTTP API

All endpoints return JSON. Detail endpoints return `200` with an empty array
(`[]`) when the id is unknown or has no data — the UI renders the empty state in
both cases.

| Method & path | Returns |
|---------------|---------|
| `GET /api/stats` | DB-wide counts. |
| `GET /api/matrix` | Paginated items×stations matrix. Query: `category`, `q` (search), `page`, `limit`. |
| `GET /api/station/{id}/orders` | Latest-capture order book for a station. Query: `item` (optional filter). |
| `GET /api/item/{id}/history` | OHLCV buckets for an item. Query: `limit`. |
| `GET /api/captures` | Per-station capture cadence. |

Example:

```bash
curl 'http://localhost:8090/api/stats'
```
```json
{"station_count":4,"item_count":560,"order_count":24120,"ohlcv_count":7905,"latest_capture":"2026-06-22T16:33:25Z"}
```

## Cell semantics

Each matrix cell aggregates the station's **latest capture** of that item (the
most recent `captured_at` per station×item):

- **Best sell** — minimum price over sell orders (cheapest ask).
- **Best buy** — maximum price over buy orders (highest bid).
- **VWAP / volume** — computed over **sell** orders only.
- **Order count** — count over both sides.
- **Sparse cell** — an item×station pair with no orders; rendered as `—`.

Timestamps are RFC3339 UTC strings throughout.

## Implementation

- Backend: `pkg/market` read methods (`GetMatrix`, `GetStationOrders`,
  `GetItemPriceHistory`, `GetCaptureHealth`) queried directly by the handlers.
- Frontend: vanilla HTML/CSS/JS embedded via `//go:embed` — no framework, no
  build step.
- Design spec: `docs/superpowers/specs/2026-06-22-market-dashboard-design.md`.
