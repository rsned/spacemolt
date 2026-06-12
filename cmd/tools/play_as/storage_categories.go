package main

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// catalogItemsPath is the cached catalog scrape used to map item_id → category
// for grouped storage rendering. It is refreshed out-of-band by the
// data-scraper (cmd/data/data-scraper); items added after the last scrape
// bucket as "unknown" until the file is regenerated.
const catalogItemsPath = "data/game-api/latest/catalog_items.json"

var (
	itemCategoriesOnce sync.Once
	itemCategories     map[string]string
)

// itemCategory returns the catalog category for an item_id, or "unknown" if the
// item is absent from the catalog scrape (or the scrape could not be read).
//
// The lookup is built once per process from catalogItemsPath; a catalog refresh
// mid-session requires restarting play_as to take effect.
func itemCategory(itemID string) string {
	itemCategoriesOnce.Do(loadItemCategories)
	if cat, ok := itemCategories[strings.ToLower(itemID)]; ok && cat != "" {
		return cat
	}
	return "unknown"
}

func loadItemCategories() {
	itemCategories = map[string]string{}
	data, err := os.ReadFile(catalogItemsPath)
	if err != nil {
		return
	}
	var f struct {
		Items []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	for _, it := range f.Items {
		if it.ID != "" {
			itemCategories[strings.ToLower(it.ID)] = it.Category
		}
	}
}
