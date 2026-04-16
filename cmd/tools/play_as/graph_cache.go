package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

type graphCache struct {
	graph     *galaxy.GalaxyGraph
	kb        knowledge.Base
	builtOnce bool
	mu        sync.RWMutex
}

func newGraphCache(kb knowledge.Base) *graphCache {
	return &graphCache{kb: kb}
}

func (c *graphCache) GetOrCreate(ctx context.Context) (*galaxy.GalaxyGraph, error) {
	c.mu.RLock()
	if c.builtOnce && c.graph != nil {
		g := c.graph
		c.mu.RUnlock()
		return g, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.builtOnce && c.graph != nil {
		return c.graph, nil
	}

	if c.kb == nil {
		return nil, fmt.Errorf("knowledge base not available")
	}

	g := &galaxy.GalaxyGraph{}
	if err := g.BuildFromDB(ctx, c.kb); err != nil {
		return nil, fmt.Errorf("failed to build galaxy graph: %w", err)
	}

	c.graph = g
	c.builtOnce = true

	stats := g.Stats()
	fmt.Printf("\n[Galaxy graph built: %d systems, %d edges in %v]\n",
		stats.NodeCount, stats.EdgeCount, stats.BuildTime)

	return g, nil
}

func (c *graphCache) Stats() galaxy.GraphStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.graph != nil {
		return c.graph.Stats()
	}
	return galaxy.GraphStats{}
}
