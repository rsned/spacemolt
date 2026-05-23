// Package galaxy provides in-memory galaxy graph representation and pathfinding.
package galaxy

import "time"

// Position represents 2D coordinates in space. This is distinct from
// game.Position which includes Z for 3D space - graph pathfinding only
// needs X/Y coordinates for distance calculations.
type Position struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}

// SystemNode is a lightweight subset of game.SystemData for graph operations.
// Does not include POIs, online players, or discovery status.
type SystemNode struct {
    ID           string  `json:"id"`
    Name         string  `json:"name"`
    Position     Position `json:"position"`
    Empire       string  `json:"empire"`
    IsStronghold bool    `json:"is_stronghold"`
    PoliceLevel    int     `json:"police_level"`
    SecurityStatus string  `json:"security_status"`
    LastUpdated    int64   `json:"last_updated"` // game tick
}

// Edge represents a connection between two systems.
type Edge struct {
    To           string  `json:"to"`
    Distance     int     `json:"distance"` // jumps (always 1 for direct connections)
    FuelCost     float64 `json:"fuel_cost"`
    TravelTime   float64 `json:"travel_time"`
    LastTraveled string  `json:"last_traveled"`
}

// Route is a path from one system to another.
type Route struct {
    Path       []string `json:"path"`
    Hops       int      `json:"hops"`
    TotalFuel  float64  `json:"total_fuel"`
    TotalTicks int      `json:"total_ticks"`
}

// NearestResult is a candidate system with metadata for "nearest" queries.
type NearestResult struct {
    SystemID     string  `json:"system_id"`
    SystemName   string  `json:"system_name"`
    Hops         int     `json:"hops"`
    // Security is the numeric police level of the system; SecurityStatus is
    // the human-readable status string (e.g. "55 Medium", "0 Lawless").
    Security       int    `json:"security"`
    SecurityStatus string `json:"security_status"`
    LastUpdated  int64   `json:"last_updated"` // tick
    IsHomeBase   bool    `json:"is_home_base"`
    StaleWarning string  `json:"stale_warning"`
    // POIs lists the specific POIs within SystemID that satisfied the
    // query (e.g., the asteroid belts that hold a requested resource).
    // Populated for resource lookups; left nil for POI-type lookups.
    POIs []NearestPOI `json:"pois,omitempty"`
}

// NearestPOI identifies one POI inside a NearestResult system that
// matched the query, with optional Remaining quantity for resource
// lookups.
type NearestPOI struct {
    ID        string  `json:"id"`
    Name      string  `json:"name"`
    Remaining float64 `json:"remaining,omitempty"`
}

// GraphStats captures instrumentation data from graph building.
type GraphStats struct {
    NodeCount    int           `json:"node_count"`
    EdgeCount    int           `json:"edge_count"`
    BuildTime    time.Duration `json:"build_time"`
    BuiltAt      time.Time     `json:"built_at"`
}