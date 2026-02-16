package main

import (
	"encoding/json"
	"sync"
)

// Cache stores shared game data that doesn't change often.
type Cache struct {
	galaxyMap   json.RawMessage
	shipClasses json.RawMessage
	recipes     json.RawMessage
	systemCache sync.Map // system_id -> json.RawMessage
	mu          sync.RWMutex
}

// NewCache creates an empty cache.
func NewCache() *Cache {
	return &Cache{}
}

func (c *Cache) SetGalaxyMap(data json.RawMessage) {
	c.mu.Lock()
	c.galaxyMap = data
	c.mu.Unlock()
}

func (c *Cache) GetGalaxyMap() json.RawMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.galaxyMap
}

func (c *Cache) SetShipClasses(data json.RawMessage) {
	c.mu.Lock()
	c.shipClasses = data
	c.mu.Unlock()
}

func (c *Cache) GetShipClasses() json.RawMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.shipClasses
}

func (c *Cache) SetRecipes(data json.RawMessage) {
	c.mu.Lock()
	c.recipes = data
	c.mu.Unlock()
}

func (c *Cache) GetRecipes() json.RawMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.recipes
}

func (c *Cache) SetSystem(systemID string, data json.RawMessage) {
	c.systemCache.Store(systemID, data)
}

func (c *Cache) GetSystem(systemID string) (json.RawMessage, bool) {
	val, ok := c.systemCache.Load(systemID)
	if !ok {
		return nil, false
	}
	return val.(json.RawMessage), true
}

// GetCacheData returns cached data by key name.
func (c *Cache) GetCacheData(key string) json.RawMessage {
	switch key {
	case "galaxy_map":
		return c.GetGalaxyMap()
	case "ship_classes":
		return c.GetShipClasses()
	case "recipes":
		return c.GetRecipes()
	default:
		return nil
	}
}
