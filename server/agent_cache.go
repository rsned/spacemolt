package main

import (
	"encoding/json"
	"sync"
)

// cacheEntry holds a cached response and the tick at which it was stored.
type cacheEntry struct {
	data json.RawMessage
	tick int64
}

// cachePolicy defines how long a cached entry remains valid.
type cachePolicy struct {
	maxAgeTicks int64
}

// cachePolicies maps command names to their caching policy.
// Commands not listed here are never cached.
var cachePolicies = map[string]cachePolicy{
	"get_system":            {maxAgeTicks: 30},
	"get_ship":              {maxAgeTicks: 0},
	"get_ships":             {maxAgeTicks: 200},
	"get_base":              {maxAgeTicks: 60},
	"get_poi":               {maxAgeTicks: 60},
	"get_skills":            {maxAgeTicks: 100},
	"get_cargo":             {maxAgeTicks: 0},
	"get_recipes":           {maxAgeTicks: 1000},
	"get_map":               {maxAgeTicks: 1000},
	"view_storage":          {maxAgeTicks: 0},
	"view_faction_storage":  {maxAgeTicks: 0},
}

// invalidationMap maps mutation commands to the cache keys they invalidate.
var invalidationMap = map[string][]string{
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

// agentCache provides per-agent response caching keyed by command name.
type agentCache struct {
	entries map[string]*cacheEntry
	mu      sync.RWMutex
}

// newAgentCache creates an empty agent cache.
func newAgentCache() *agentCache {
	return &agentCache{
		entries: make(map[string]*cacheEntry),
	}
}

// get returns cached data for the given command if the entry exists and is
// still fresh relative to currentTick. Returns nil on miss or stale entry.
func (c *agentCache) get(command string, currentTick int64) json.RawMessage {
	policy, ok := cachePolicies[command]
	if !ok {
		return nil
	}

	c.mu.RLock()
	entry, exists := c.entries[command]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// For invalidation-only policies (maxAgeTicks == 0), the entry is valid
	// until explicitly invalidated — no tick-based expiry.
	if policy.maxAgeTicks > 0 && currentTick-entry.tick > policy.maxAgeTicks {
		return nil
	}

	return entry.data
}

// set stores a response for the given command at the specified tick.
func (c *agentCache) set(command string, data json.RawMessage, tick int64) {
	c.mu.Lock()
	c.entries[command] = &cacheEntry{data: data, tick: tick}
	c.mu.Unlock()
}

// invalidate removes the specified commands from the cache.
func (c *agentCache) invalidate(commands ...string) {
	c.mu.Lock()
	for _, cmd := range commands {
		delete(c.entries, cmd)
	}
	c.mu.Unlock()
}

// clear removes all cached entries.
func (c *agentCache) clear() {
	c.mu.Lock()
	c.entries = make(map[string]*cacheEntry)
	c.mu.Unlock()
}
