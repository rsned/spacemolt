package galaxy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// GalaxyGraph is an in-memory representation of the galaxy.
type GalaxyGraph struct {
	mu    sync.RWMutex
	nodes map[string]*SystemNode
	adj   map[string][]Edge
	stats GraphStats
}

// BuildFromDB constructs the graph from the knowledge base.
// Queries systems, connections, and connection_metrics to build
// an adjacency list with metadata.
func (g *GalaxyGraph) BuildFromDB(ctx context.Context, kb knowledge.Base) error {
	start := time.Now()

	g.mu.Lock()
	defer g.mu.Unlock()

	// Initialize maps
	g.nodes = make(map[string]*SystemNode)
	g.adj = make(map[string][]Edge)

	// Load systems
	systems, err := kb.GetSystemsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to load systems: %w", err)
	}

	for _, s := range systems {
		g.nodes[s.ID] = &SystemNode{
			ID:           s.ID,
			Name:         s.Name,
			Position:     Position{X: s.Position.X, Y: s.Position.Y},
			Empire:       s.Empire,
			IsStronghold: s.IsStronghold,
			PoliceLevel:  s.PoliceLevel,
			LastUpdated:  s.LastUpdatedTick,
		}
		g.adj[s.ID] = []Edge{} // Initialize adjacency list
	}

	// Load connections
	connections, err := kb.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("failed to load connections: %w", err)
	}

	for _, conn := range connections {
		// Create bidirectional edges
		edge := Edge{
			To:         conn.ToSystem,
			Distance:   conn.Distance,
			FuelCost:   0, // Will be filled from metrics if available
			TravelTime: 0,
		}

		g.adj[conn.FromSystem] = append(g.adj[conn.FromSystem], edge)

		// Reverse edge
		reverseEdge := Edge{
			To:       conn.FromSystem,
			Distance: conn.Distance,
		}
		g.adj[conn.ToSystem] = append(g.adj[conn.ToSystem], reverseEdge)
	}

	// Load connection metrics (optional, for weighted searches)
	metrics, err := kb.GetConnectionMetrics(ctx)
	if err == nil {
		for _, m := range metrics {
			// Find and update the edge
			for i, edge := range g.adj[m.FromSystem] {
				if edge.To == m.ToSystem {
					g.adj[m.FromSystem][i].FuelCost = m.AvgFuelCost
					g.adj[m.FromSystem][i].TravelTime = m.AvgTravelTime
					if m.LastTraveled != "" {
						g.adj[m.FromSystem][i].LastTraveled = m.LastTraveled
					}
					break
				}
			}
		}
	}

	// Record stats
	g.stats = GraphStats{
		NodeCount: len(g.nodes),
		EdgeCount: len(connections) * 2, // Bidirectional
		BuildTime: time.Since(start),
		BuiltAt:   time.Now(),
	}

	return nil
}
