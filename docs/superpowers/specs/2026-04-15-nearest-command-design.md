# "nearest" Command Design Spec

**Date:** 2026-04-15
**Author:** Brainstorming session with user
**Status:** Approved

## Overview

Build a shared `pkg/galaxy` package with an in-memory `GalaxyGraph` that enables the `play_as` tool to find the nearest system matching specific criteria (stations, resources, POI types). The graph is built from the knowledge base, supports both unweighted (jumps) and weighted (fuel cost, travel time) pathfinding, and is fully unit-tested for reuse across all tools.

## Goals

### Phase 1 (Initial Implementation)
- `nearest station` finds the nearest accessible station from current location
- Shows top 3 options with hop count and metadata
- Warns about stale data
- Uses in-memory graph built on-demand and cached for session

### Phase 2 (Future Enhancements)
- `nearest <poi_type>` (asteroid_belt, gas_giant, etc.)
- `nearest <resource>` (palladium_ore, etc.)
- Filter flags: `--within=<hops>`, `--fresh`, `--empire=<empire>`
- Ship capability filtering (fuel capacity, jump speed)
- Lazy background goroutine graph building

## Architecture

### Package Structure

```
pkg/galaxy/
├── graph.go          # GalaxyGraph type, pathfinding algorithms
├── graph_test.go     # Unit tests for graph building and pathfinding
└── types.go          # SystemNode, Edge, Route, etc.

cmd/tools/play_as/
├── nearest.go        # 'nearest' command handler (new file)
└── graph_cache.go    # Session-scoped graph cache (lazy build)
```

### Key Components

#### 1. `pkg/galaxy/graph.go` — `GalaxyGraph` type
- Holds systems (nodes), connections (edges), and connection metrics
- Methods: `BuildFromDB()`, `FindNearest(from, targets []string)`, `FindPath(from, to)`
- Supports both unweighted (hops) and weighted (fuel, time) searches
- Thread-safe for concurrent reads

#### 2. `cmd/tools/play_as/nearest.go` — command handler
- Parses command args (`nearest station`)
- Queries knowledge base for candidate systems (filters by accessibility)
- Calls `graph.FindNearest()` to get top 3 results
- Formats output with warnings for stale data

#### 3. `cmd/tools/play_as/graph_cache.go` — session cache
- Lazy-initializes `GalaxyGraph` on first `nearest` call
- Tracks build time and memory usage for instrumentation
- Optional: refreshes graph if session runs long

### Data Flow

```
play_as session
    |
    v
User types: "nearest station"
    |
    v
nearest.go handler
    |
    +---> graph_cache.go: get or build GalaxyGraph
    |       |
    |       +---> pkg/galaxy: BuildFromDB(knowledge.Base)
    |               |
    |               +---> Query systems, connections, connection_metrics
    |               +---> Build adjacency list with metadata
    |               +---> Return *GalaxyGraph
    |
    +---> Query KB: "SELECT DISTINCT system_id FROM pois WHERE type='station'"
    |       +---> Filter by accessibility (NOT is_stronghold, public_access=1)
    |
    +---> graph.FindNearest(currentSystem, candidateSystemIDs)
    |       |
    |       +---> Run Dijkstra/BFS from current system
    |       +---> Filter to candidate systems
    |       +-----+---> Return top 3 by hop count
    |
    v
Format and display results with warnings
```

## Data Structures

### Core Types (`pkg/galaxy/types.go`)

```go
// SystemNode represents a system in the galaxy graph.
type SystemNode struct {
    ID           string
    Name         string
    Position     knowledge.Position  // {X, Y}
    Empire       string
    IsStronghold bool
    PoliceLevel  int
    LastUpdated  int64  // tick
}

// Edge represents a connection between two systems.
type Edge struct {
    To            string
    Distance      int      // jumps (always 1 for now)
    FuelCost      float64  // avg fuel cost from connection_metrics
    TravelTime    float64  // avg ticks from connection_metrics
    LastTraveled  string   // ISO timestamp
}

// Route is a path from one system to another.
type Route struct {
    Path       []string // system IDs in order
    Hops       int      // total jumps
    TotalFuel  float64  // if using weighted search
    TotalTicks int      // if using weighted search
}

// NearestResult is a candidate system with metadata.
type NearestResult struct {
    SystemID     string
    SystemName   string
    Hops         int
    LastUpdated  int64   // tick
    IsHomeBase   bool
    StaleWarning string  // if data is old
}
```

### GalaxyGraph (`pkg/galaxy/graph.go`)

```go
// GalaxyGraph is an in-memory representation of the galaxy.
type GalaxyGraph struct {
    mu        sync.RWMutex
    nodes     map[string]*SystemNode      // system_id -> node
    adj       map[string][]Edge           // system_id -> outgoing edges
    builtAt   time.Time
    buildTime time.Duration               // for instrumentation
}

// BuildFromDB constructs the graph from the knowledge base.
func (g *GalaxyGraph) BuildFromDB(ctx context.Context, kb knowledge.Base) error

// FindNearest finds the closest systems from 'from' to any of 'targets'.
// Returns up to 'limit' results sorted by hop count.
func (g *GalaxyGraph) FindNearest(from string, targets []string, limit int) ([]NearestResult, error)

// FindPath computes the shortest path between two systems.
func (g *GalaxyGraph) FindPath(from, to string, weighted bool) (Route, error)
```

### Station Accessibility Filter

```go
// In cmd/tools/play_as/nearest.go

type StationFilter struct {
    ExcludeStrongholds  bool
    RequirePublicAccess bool
    Empires             []string // agent's empire + neutral
    MaxAgeTicks         int64    // 0 = no limit
}
```

## Algorithms

### Graph Building (`GalaxyGraph.BuildFromDB`)

1. Query all systems from KB:
   ```sql
   SELECT id, name, position_x, position_y, empire,
          is_stronghold, police_level, last_updated_tick
   FROM systems
   ```

2. Query all connections from KB:
   ```sql
   SELECT from_system, to_system, distance
   FROM connections
   ```

3. Query connection metrics (optional, for weighted searches):
   ```sql
   SELECT from_system, to_system, avg_fuel_cost, avg_travel_time
   FROM connection_metrics
   WHERE avg_fuel_cost IS NOT NULL
   ```

4. Build adjacency list:
   - Create `SystemNode` for each system
   - For each connection, create bidirectional `Edge` entries
   - Merge in metrics where available

### Finding Nearest (`FindNearest`)

**Input:** `from` system ID, `[]targets` (candidate system IDs), `limit`

**Algorithm:** Modified Dijkstra (unweighted = BFS)

1. Run Dijkstra from `from` to all reachable systems
   - Track `distance[systemID] = hops`
   - Stop when all targets are found

2. Filter results to only include systems in `targets`

3. Sort by hop count (ascending)

4. Return top `limit` results

**Complexity:** O(V + E) for the graph traversal

### Station Accessibility Query

```sql
SELECT DISTINCT pois.system_id, pois.last_updated_tick
FROM pois
LEFT JOIN bases ON bases.poi_id = pois.id
WHERE pois.type = 'station'
  AND bases.public_access = 1
  -- Exclude strongholds via systems table:
  AND pois.system_id NOT IN (
    SELECT id FROM systems WHERE is_stronghold = 1
  )
```

### Staleness Detection

For each result, compute staleness:
```go
ageTicks := currentTick - result.LastUpdated
if ageTicks > staleThreshold {  // e.g., 1 day = ~8640 ticks
    result.StaleWarning = fmt.Sprintf("⚠ Data from %d ticks ago", ageTicks)
}
```

## Command Interface

### Syntax

```
nearest station
```

**Phase 1:**
- Single argument: POI type (`station`, `asteroid_belt`, `gas_giant`, etc.)
- Returns top 3 accessible systems with that POI type

**Phase 2 (future):**
- `nearest <resource>` (e.g., `nearest palladium_ore`)
- `nearest --within=20 station` (max hops filter)
- `nearest --fresh station` (exclude stale data)

### Output Format

**Styled output:**
```
Nearest accessible stations from Gamma Orionis:

  1. Sol (sol) — 15 hops (your home base)
     Last updated: 2 hours ago

  2. Rigel (rigel) — 15 hops
     ⚠ Station data from 3 weeks ago

  3. Haven (haven) — 17 hops
     Last updated: 6 hours ago
```

**Raw/JSON output:**
```json
{
  "from_system": "gamma_orionis",
  "from_system_name": "Gamma Orionis",
  "query_type": "station",
  "results": [
    {"system_id": "sol", "system_name": "Sol", "hops": 15, "is_home_base": true, "last_updated_tick": 12345678},
    {"system_id": "rigel", "system_name": "Rigel", "hops": 15, "is_home_base": false, "last_updated_tick": 12300000, "stale_warning": "3 weeks old"},
    {"system_id": "haven", "system_name": "Haven", "hops": 17, "is_home_base": false, "last_updated_tick": 12345500}
  ]
}
```

### Integration with play_as REPL

- Add `"nearest"` to the command completer
- Access to `globalClient` (for current system)
- Access to `globalKB` (for graph building and queries)
- Access to `globalClock` (for current tick)
- Respects `outputFormat` setting (raw/styled)

## Testing Strategy

### Unit Tests (`pkg/galaxy/graph_test.go`)

1. **Graph Building Tests**
   - Test building from mock KB data
   - Verify all nodes and edges are loaded correctly
   - Test metrics merging (where available)
   - Benchmark graph build time and memory allocations

2. **Pathfinding Tests**
   - Test known routes from `game/trade_routes_test.go` data
   - Verify hop counts match expected values
   - Test unreachable systems (disconnected graphs)
   - Test weighted vs unweighted searches (when metrics added)

3. **FindNearest Tests**
   - Test finding stations from various starting points
   - Verify top N results are returned
   - Test tie-breaking (equal hop counts)
   - Test empty candidate lists

### Integration Tests (`cmd/tools/play_as/explore_test.go`)

1. **End-to-End Tests**
   - Use test DB with known galaxy data
   - Run `nearest station` and verify output format
   - Test stale data warnings
   - Test with no accessible stations (graceful error)

### Benchmark Tests

```go
func BenchmarkGalaxyGraphBuild(b *testing.B) {
    kb := setupTestKB()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        g := &GalaxyGraph{}
        g.BuildFromDB(context.Background(), kb)
    }
}

func BenchmarkFindNearest(b *testing.B) {
    g := setupTestGraph()
    targets := loadAllStationSystems()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        g.FindNearest("sol", targets, 3)
    }
}
```

### Test Data

- Use subset of real galaxy data from existing KB dumps
- Or create synthetic test galaxy with ~50 systems for faster tests

## Future Enhancements

### Resource-Based Queries (`nearest palladium_ore`)

**Algorithm:**
1. Query KB for POIs with the resource:
   ```sql
   SELECT DISTINCT pois.system_id, poi_resources.resource_id, poi_resources.richness
   FROM pois
   JOIN poi_resources ON pois.id = poi_resources.poi_id
   WHERE poi_resources.resource_id = 'palladium_ore'
   ```
2. Extract unique system_ids
3. Pass to `FindNearest()` as candidate targets
4. Rank by hops, then by richness (as tiebreaker)

**Syntax:** `nearest palladium_ore` (no keyword needed if ID matches resource table)

### POI Type Queries (`nearest asteroid_belt`)

**Algorithm:**
1. Query KB for POIs by type:
   ```sql
   SELECT DISTINCT system_id, type, last_updated_tick
   FROM pois
   WHERE type = 'asteroid_belt'
   ```
2. Extract unique system_ids
3. Pass to `FindNearest()` as candidate targets

**Syntax:** `nearest asteroid_belt`, `nearest gas_giant`, `nearest moon`

### Advanced Filters

- `--within=<hops>` - Max hop count filter
- `--fresh` - Exclude data older than threshold
- `--rich` - For resources, only show high-richness POIs
- `--empire=<empire>` - Filter by empire alignment

### Ship-Aware Routing (Weighted Graph)

Use connection metrics and ship stats:
```go
type ShipCapabilities struct {
    MaxFuel      float64
    FuelPerJump  float64  // from connection_metrics.avg_fuel_cost
    JumpSpeed    float64  // ticks per jump
}

// FindNearest can accept optional ShipCapabilities
// to weight routes by fuel cost or travel time
```

### Lazy Background Building

```go
// In graph_cache.go
func StartBackgroundBuilder(ctx context.Context, kb knowledge.Base) {
    go func() {
        ticker := time.NewTicker(30 * time.Minute)
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                refreshGraph()
            }
        }
    }()
}
```

## Performance Considerations

- **Galaxy size:** ~500 systems, ~2-4 connections each → ~1000-2000 edges
- **Graph build:** Expected <100ms for full graph from SQLite
- **FindNearest query:** O(V + E) → ~2500 operations, expected <1ms
- **Memory footprint:** Estimated ~500KB for full graph with metadata
- **Session caching:** Build once, reuse for session lifetime

## Success Criteria

### Phase 1

- [ ] `nearest station` returns top 3 accessible stations
- [ ] Results show system name, ID, hop count, and staleness warnings
- [ ] Graph builds in <100ms (benchmark validated)
- [ ] Unit tests achieve >80% coverage on `pkg/galaxy`
- [ ] Integration tests pass with real KB data
- [ ] Command integrates cleanly into play_as REPL

### Phase 2 (Future)

- [ ] `nearest <resource>` works for all resource types
- [ ] `nearest <poi_type>` works for all POI types
- [ ] Filter flags (`--within`, `--fresh`) implemented
- [ ] Ship-aware routing with weighted graphs
- [ ] Lazy background building if needed

## Notes

- **Resource types are POI-specific:** Gas clouds won't have ore types, asteroid belts won't have gas types
- **Agent data is primary:** Knowledge base built from agent exploration is the primary data source
- **Freshness signals:** `last_updated_tick` used for staleness detection, not for filtering (show with warning)
- **Distance metric:** Primary metric is hop count (edges), not Euclidean distance
- **Accessibility filter:** Combination of `NOT is_stronghold` AND `public_access=1`
