# Market Dashboard — Ships Tab

**Date:** 2026-07-04 · **Status:** approved (user, in-session) · **Scope:** cmd/market-dashboard only

Add a fifth tab, **Ships**, to the market-dashboard (`:8090`) showing the
fleet-captured ship inventory the same way the Matrix shows item listings —
e.g. `Archimedes · 17 listings · from 10,834`. Data source is the v2
`ship_listings` table in `data/spacemolt-knowledge.db` (hourly marketbot
captures, replace-per-station snapshots; see
2026-07-04-ship-listings-capture-revival-design.md).

## Design

- **`--kb-path`** flag (default `data/spacemolt-knowledge.db`). The dashboard
  opens it with a plain **read-only** `database/sql` connection
  (`file:<path>?mode=ro`, modernc driver) — NOT `knowledge.NewSQLiteKB`,
  whose constructor runs migrations; a read-only dashboard must never write
  to the fleet's live KB.
- **`GET /api/ships`** — one row per hull class, sorted by listing count
  desc: `class_id, ship_name, category, tier, listing_count, min_price,
  max_price, station_count, cheapest_station_id, cheapest_station_name,
  captured_at` (correlated subquery for the cheapest station; table is a few
  hundred rows, cost is nil).
- **`GET /api/ships/{id}`** — per-listing drill-down for one class, sorted by
  price: `station_id, station_name, system_id, system_name, price, hull,
  max_hull, shield, modules_count, tier, seller, listed_at, captured_at`.
- **UI:** tab button `data-view="ships"`; aggregate table rendered like the
  other views; clicking a row opens the existing `cell-dialog` with the
  drill-down table (mirrors Matrix cell → order book). Hull `-1` (not
  reported) renders as `—`.
- New code lives in `cmd/market-dashboard/ships.go` (types + queries +
  handlers); routes wired in main.go; README flag row.

## Testing

`ships_test.go`: build a temp KB via `knowledge.NewSQLiteKB` +
`StoreShipListings` fixtures (two classes across two stations), reopen
read-only, assert aggregate ordering/fields and drill-down price sort; 404 on
missing class param; clean error when the KB path doesn't exist.
