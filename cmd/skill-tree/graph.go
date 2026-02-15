package main

import (
	"fmt"
)

// NodeInfo holds a skill node for DOT output.
type NodeInfo struct {
	ID         string
	Name       string
	Tier       int
	IsExternal bool // true if skill is from another category
}

// EdgeInfo holds a dependency edge for DOT output.
type EdgeInfo struct {
	From          string
	To            string
	RequiredLevel int
	CrossCategory bool // true if From is from another category
}

// CategoryGraph holds the dependency graph for one category.
type CategoryGraph struct {
	Category string
	Nodes    []NodeInfo
	Edges    []EdgeInfo
}

// BuildCategoryGraph builds the graph for the given category, including
// cross-category dependencies as nodes with gray edges.
func BuildCategoryGraph(payload *SkillsPayload, category string) (*CategoryGraph, error) {
	catalog := payload.Skills

	// Collect skills in this category and all their dependencies (including external).
	nodeSet := make(map[string]bool)
	var inCategory []string
	for id, s := range catalog {
		if s.Category != category {
			continue
		}
		inCategory = append(inCategory, id)
		nodeSet[id] = true
		for depID := range s.RequiredSkills {
			nodeSet[depID] = true
		}
	}
	if len(inCategory) == 0 {
		return nil, nil // empty category
	}

	// Build adjacency list for dependency direction: dep -> dependent
	// and collect all edges with metadata.
	var edges []edge
	for id, s := range catalog {
		if s.Category != category {
			continue
		}
		for depID, level := range s.RequiredSkills {
			if _, ok := catalog[depID]; !ok {
				continue // skip missing refs
			}
			depCat := catalog[depID].Category
			edges = append(edges, edge{depID, id, level, depCat})
		}
	}

	// Detect cycles via DFS.
	if hasCycle(catalog, nodeSet, edges) {
		return nil, fmt.Errorf("category %q: circular dependency detected", category)
	}

	// Compute tier for each node. Tier 0 = no deps, tier k = 1 + max(tier of deps).
	tier := make(map[string]int)
	for id := range nodeSet {
		tier[id] = -1
	}
	var computeTier func(string) int
	computeTier = func(id string) int {
		if t := tier[id]; t >= 0 {
			return t
		}
		maxDepTier := -1
		if s, ok := catalog[id]; ok {
			for depID := range s.RequiredSkills {
				if _, inCatalog := catalog[depID]; !inCatalog {
					continue
				}
				dt := computeTier(depID)
				if dt > maxDepTier {
					maxDepTier = dt
				}
			}
		}
		tier[id] = maxDepTier + 1
		return tier[id]
	}
	for id := range nodeSet {
		computeTier(id)
	}

	// Build tier -> nodes for ordering.
	maxTier := 0
	for _, t := range tier {
		if t > maxTier {
			maxTier = t
		}
	}

	nodes := make([]NodeInfo, 0, len(nodeSet))
	for id := range nodeSet {
		s, ok := catalog[id]
		name := id
		if ok {
			name = s.Name
		}
		nodes = append(nodes, NodeInfo{
			ID:         id,
			Name:       name,
			Tier:       tier[id],
			IsExternal: catalog[id].Category != category,
		})
	}

	edgeInfos := make([]EdgeInfo, 0, len(edges))
	for _, e := range edges {
		edgeInfos = append(edgeInfos, EdgeInfo{
			From:          e.from,
			To:            e.to,
			RequiredLevel: e.level,
			CrossCategory: e.fromCat != category,
		})
	}

	return &CategoryGraph{
		Category: category,
		Nodes:    nodes,
		Edges:    edgeInfos,
	}, nil
}

// hasCycle returns true if the subgraph (nodes in nodeSet, edges among them) has a cycle.
func hasCycle(catalog map[string]Skill, nodeSet map[string]bool, edges []edge) bool {
	// Build reverse adjacency: to -> from (dependents -> deps) for DFS from roots.
	// Actually cycle detection: we need to find a cycle in the directed graph.
	// Graph: edge from A to B means A is required by B (A -> B).
	// Cycle: A -> B -> C -> A. Use standard DFS cycle detection.
	adj := make(map[string][]string)
	for _, e := range edges {
		if nodeSet[e.from] && nodeSet[e.to] {
			adj[e.from] = append(adj[e.from], e.to)
		}
	}
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var visit func(string) bool
	visit = func(id string) bool {
		visited[id] = true
		inStack[id] = true
		for _, w := range adj[id] {
			if !visited[w] {
				if visit(w) {
					return true
				}
			} else if inStack[w] {
				return true
			}
		}
		inStack[id] = false
		return false
	}
	for id := range nodeSet {
		if !visited[id] && visit(id) {
			return true
		}
	}
	return false
}

// edge is an internal type for graph construction.
type edge struct {
	from, to string
	level    int
	fromCat  string
}
