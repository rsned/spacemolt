package galaxy

import (
	"container/heap"
	"context"
	"fmt"
	"math"
	"slices"
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
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("failed to load systems: %w", err)
	}

	for _, s := range systems {
		g.nodes[s.ID] = &SystemNode{
			ID:             s.ID,
			Name:           s.Name,
			Position:       Position{X: s.Position.X, Y: s.Position.Y},
			Empire:         s.Empire,
			IsStronghold:   s.IsStronghold,
			PoliceLevel:    s.PoliceLevel,
			SecurityStatus: s.SecurityStatus,
			LastUpdated:    s.LastUpdatedTick,
		}
		g.adj[s.ID] = []Edge{} // Initialize adjacency list
	}

	// Load connections
	connections, err := kb.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("failed to load connections: %w", err)
	}

	for _, conn := range connections {
		// Validate both endpoints exist
		if _, ok := g.nodes[conn.FromSystem]; !ok {
			continue
		}
		if _, ok := g.nodes[conn.ToSystem]; !ok {
			continue
		}

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

			// Also update the reverse edge
			for i, edge := range g.adj[m.ToSystem] {
				if edge.To == m.FromSystem {
					g.adj[m.ToSystem][i].FuelCost = m.AvgFuelCost
					g.adj[m.ToSystem][i].TravelTime = m.AvgTravelTime
					if m.LastTraveled != "" {
						g.adj[m.ToSystem][i].LastTraveled = m.LastTraveled
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

// queueItem represents an item in the priority queue for Dijkstra's algorithm.
type queueItem struct {
	system    string
	priority  float64 // distance or fuel cost
	index     int     // index in the heap
}

// priorityQueue implements heap.Interface for Dijkstra's algorithm.
type priorityQueue []*queueItem

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].priority < pq[j].priority
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*queueItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

// FindPath finds the shortest path between two systems using Dijkstra's algorithm.
// If weighted is true, uses fuel cost as the edge weight.
// If weighted is false, uses hop count (each edge has weight 1).
// Returns Route with the path, hop count, total fuel, and total travel time.
// Returns error if either system doesn't exist or no path exists.
func (g *GalaxyGraph) FindPath(from, to string, weighted bool) (Route, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Validate both systems exist
	if _, ok := g.nodes[from]; !ok {
		return Route{}, fmt.Errorf("system '%s' not found in graph", from)
	}
	if _, ok := g.nodes[to]; !ok {
		return Route{}, fmt.Errorf("system '%s' not found in graph", to)
	}

	// Special case: same system
	if from == to {
		return Route{
			Path:       []string{from},
			Hops:       0,
			TotalFuel:  0,
			TotalTicks: 0,
		}, nil
	}

	// Dijkstra's algorithm with priority queue
	dist := make(map[string]float64)      // best known distance to each system
	prev := make(map[string]string)       // previous system in shortest path
	visited := make(map[string]bool)      // visited systems

	// Initialize distances
	for system := range g.nodes {
		dist[system] = math.Inf(1)
	}
	dist[from] = 0

	// Create priority queue and push start node
	pq := make(priorityQueue, 1)
	pq[0] = &queueItem{system: from, priority: 0}
	heap.Init(&pq)

	for pq.Len() > 0 {
		// Get system with minimum distance
		current := heap.Pop(&pq).(*queueItem)

		// Skip if already visited
		if visited[current.system] {
			continue
		}
		visited[current.system] = true

		// Found destination
		if current.system == to {
			break
		}

		// Explore neighbors
		for _, edge := range g.adj[current.system] {
			if visited[edge.To] {
				continue
			}

			// Calculate weight: fuel cost if weighted, else 1
			weight := 1.0
			if weighted {
				if edge.FuelCost > 0 {
					weight = edge.FuelCost
				}
			}

			newDist := dist[current.system] + weight

			// If we found a better path to this neighbor
			if newDist < dist[edge.To] {
				dist[edge.To] = newDist
				prev[edge.To] = current.system

				// Push updated distance to queue
				heap.Push(&pq, &queueItem{
					system:   edge.To,
					priority: newDist,
				})
			}
		}
	}

	// Check if destination is reachable
	if _, ok := prev[to]; !ok && from != to {
		return Route{}, fmt.Errorf("no path found from %s to %s", from, to)
	}

	// Reconstruct path by walking backwards from destination
	path := []string{}
	current := to
	for current != "" {
		path = append([]string{current}, path...)
		if current == from {
			break
		}
		current = prev[current]
	}

	// Calculate total fuel and travel time
	totalFuel := 0.0
	totalTicks := 0
	for i := 0; i < len(path)-1; i++ {
		fromSystem := path[i]
		toSystem := path[i+1]

		// Find the edge and accumulate costs
		for _, edge := range g.adj[fromSystem] {
			if edge.To == toSystem {
				totalFuel += edge.FuelCost
				if edge.TravelTime > 0 {
					totalTicks += int(edge.TravelTime)
				}
				break
			}
		}
	}

	return Route{
		Path:       path,
		Hops:       len(path) - 1,
		TotalFuel:  totalFuel,
		TotalTicks: totalTicks,
	}, nil
}

// FindNearest finds the closest systems from 'from' to any of 'targets'.
// Returns up to 'limit' results sorted by hop count (ascending).
func (g *GalaxyGraph) FindNearest(from string, targets []string, limit int) ([]NearestResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, exists := g.nodes[from]; !exists {
		return nil, fmt.Errorf("from system %s not found in graph", from)
	}

	if len(targets) == 0 {
		return []NearestResult{}, nil
	}

	// Build target set for fast lookup
	targetSet := make(map[string]bool)
	for _, t := range targets {
		targetSet[t] = true
	}

	// Dijkstra from source to all systems
	dist := make(map[string]int)
	visited := make(map[string]bool)

	for nodeID := range g.nodes {
		dist[nodeID] = -1 // Unreachable
	}
	dist[from] = 0

	// BFS queue for unweighted shortest paths
	queue := []string{from}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		for _, edge := range g.adj[current] {
			if dist[edge.To] == -1 {
				dist[edge.To] = dist[current] + 1
				queue = append(queue, edge.To)
			}
		}
	}

	// Collect results that are in target set and reachable
	var results []NearestResult
	for _, targetID := range targets {
		if dist[targetID] == -1 {
			continue // Unreachable
		}

		node, exists := g.nodes[targetID]
		if !exists {
			continue
		}

		results = append(results, NearestResult{
			SystemID:       node.ID,
			SystemName:     node.Name,
			Hops:           dist[targetID],
			Security:       node.PoliceLevel,
			SecurityStatus: node.SecurityStatus,
			LastUpdated:    node.LastUpdated,
		})
	}

	// Sort by hop count
	slices.SortFunc(results, func(a, b NearestResult) int {
		if a.Hops < b.Hops {
			return -1
		}
		if a.Hops > b.Hops {
			return 1
		}
		return 0
	})

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// Stats returns graph statistics.
func (g *GalaxyGraph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.stats
}
