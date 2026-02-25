package observe

import (
	"encoding/json"
	"sync"
)

// CacheEntry holds a cached response and the tick at which it was stored.
type CacheEntry struct {
	data json.RawMessage
	tick int64
}

// CachePolicy defines how long a cached entry remains valid.
type CachePolicy struct {
	MaxAgeTicks int64
}

// CachePolicies maps command names to their caching policy.
// Commands not listed here are never cached.
var CachePolicies = map[string]CachePolicy{
	"get_system":           {MaxAgeTicks: 30},
	"get_ship":             {MaxAgeTicks: 0},
	"get_ships":            {MaxAgeTicks: 200},
	"get_base":             {MaxAgeTicks: 60},
	"get_poi":              {MaxAgeTicks: 60},
	"get_skills":           {MaxAgeTicks: 100},
	"get_cargo":            {MaxAgeTicks: 0},
	"get_recipes":          {MaxAgeTicks: 1000},
	"get_map":              {MaxAgeTicks: 3600}, // Map data is static; cache for ~1 hour
	"view_storage":         {MaxAgeTicks: 0},
	"view_faction_storage": {MaxAgeTicks: 0},
}

// InvalidationMap maps mutation commands to the cache keys they invalidate.
var InvalidationMap = map[string][]string{
	"switch_ship":               {"get_ship", "get_cargo"},
	"install_mod":               {"get_ship"},
	"install":                   {"get_ship"},
	"uninstall_mod":             {"get_ship"},
	"uninstall":                 {"get_ship"},
	"buy":                       {"get_ship", "get_cargo"},
	"sell":                      {"get_ship", "get_cargo"},
	"create_sell_order":         {"get_ship", "get_cargo"},
	"mine":                      {"get_ship", "get_cargo"},
	"craft":                     {"get_ship", "get_cargo"},
	"jettison":                  {"get_ship", "get_cargo"},
	"loot_wreck":                {"get_ship", "get_cargo"},
	"salvage_wreck":             {"get_ship", "get_cargo"},
	"use_item":                  {"get_ship", "get_cargo"},
	"refuel":                    {"get_ship"},
	"repair":                    {"get_ship"},
	"deposit_items":             {"get_ship", "get_cargo", "view_storage"},
	"withdraw_items":            {"get_ship", "get_cargo", "view_storage"},
	"deposit_credits":           {"view_storage"},
	"withdraw_credits":          {"view_storage"},
	"faction_deposit_items":     {"get_ship", "get_cargo", "view_faction_storage"},
	"faction_withdraw_items":    {"get_ship", "get_cargo", "view_faction_storage"},
	"faction_deposit_credits":   {"view_faction_storage"},
	"faction_withdraw_credits":  {"view_faction_storage"},
	"jump":                      {"get_system", "get_poi", "get_base"},
	"travel":                    {"get_poi"},
	"dock":                      {"get_base"},
	"undock":                    {"get_base"},
	"buy_ship":                  {"get_ship", "get_cargo"},
	"sell_ship":                 {"get_ship", "get_cargo"},
}

// AgentCache provides per-agent response caching keyed by command name.
type AgentCache struct {
	entries map[string]*CacheEntry
	mu      sync.RWMutex
}

// NewAgentCache creates an empty agent cache.
func NewAgentCache() *AgentCache {
	return &AgentCache{
		entries: make(map[string]*CacheEntry),
	}
}

// Get returns cached data for the given command if the entry exists and is
// still fresh relative to currentTick. Returns nil on miss or stale entry.
func (c *AgentCache) Get(command string, currentTick int64) json.RawMessage {
	policy, ok := CachePolicies[command]
	if !ok {
		return nil
	}

	c.mu.RLock()
	entry, exists := c.entries[command]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// For invalidation-only policies (MaxAgeTicks == 0), the entry is valid
	// until explicitly invalidated — no tick-based expiry.
	if policy.MaxAgeTicks > 0 && currentTick-entry.tick > policy.MaxAgeTicks {
		return nil
	}

	return entry.data
}

// Set stores a response for the given command at the specified tick.
func (c *AgentCache) Set(command string, data json.RawMessage, tick int64) {
	c.mu.Lock()
	c.entries[command] = &CacheEntry{data: data, tick: tick}
	c.mu.Unlock()
}

// Invalidate removes the specified commands from the cache.
func (c *AgentCache) Invalidate(commands ...string) {
	c.mu.Lock()
	for _, cmd := range commands {
		delete(c.entries, cmd)
	}
	c.mu.Unlock()
}

// Clear removes all cached entries.
func (c *AgentCache) Clear() {
	c.mu.Lock()
	c.entries = make(map[string]*CacheEntry)
	c.mu.Unlock()
}
