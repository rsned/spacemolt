package main

import (
	"strings"
	"testing"
)

// TestFormatStorage_ErrorFrameRendersNothing guards the regression where an
// error frame (or empty slot) routed to formatStorage printed
// "Storage at  — 0 types". A frame with no base_id/items/ships must render "".
func TestFormatStorage_ErrorFrameRendersNothing(t *testing.T) {
	cases := map[string]string{
		"error frame": `{"error":"not_docked","message":"You must be docked or provide a station_id to view storage."}`,
		"empty object": `{}`,
		"null items":   `{"items":null,"ships":null}`,
	}
	for name, raw := range cases {
		if out := formatStorage([]byte(raw)); out != "" {
			t.Errorf("%s: expected empty string, got:\n%s", name, out)
		}
	}
}

// TestFormatStorage_EmptyButValidStillRenders confirms a genuine empty storage
// (base_id present, no items) still renders the header + "(no items)".
func TestFormatStorage_EmptyButValidStillRenders(t *testing.T) {
	out := formatStorage([]byte(`{"base_id":"central_nexus","items":[]}`))
	if !strings.Contains(out, "Storage at central_nexus") || !strings.Contains(out, "(no items)") {
		t.Errorf("valid empty storage should still render, got:\n%s", out)
	}
}

// TestFormatStorage_HintShown surfaces the server's "hint" field so the agent
// knows which other station holds items when the current base is empty.
func TestFormatStorage_HintShown(t *testing.T) {
	raw := `{"base_id":"unknown_edge_waystation","hint":"6,632 items in storage at frontier_station","items":[],"ships":[]}`
	out := formatStorage([]byte(raw))
	if !strings.Contains(out, "6,632 items in storage at frontier_station") {
		t.Errorf("expected hint to be rendered, got:\n%s", out)
	}
}

// TestFormatStorage_Filter limits the table to items whose item_id or name
// contains the substring (case-insensitive) and annotates the header.
func TestFormatStorage_Filter(t *testing.T) {
	raw := `{"base_id":"central_nexus","items":[
		{"item_id":"nickel_ore","name":"Nickel Ore","quantity":25554,"size":1},
		{"item_id":"copper_wiring","name":"Copper Wiring","quantity":1868,"size":1},
		{"item_id":"iron_ore","name":"Iron Ore","quantity":10,"size":1}
	]}`

	storageFmtOpts = storageFmtOptions{filter: "ORE"}
	t.Cleanup(func() { storageFmtOpts = storageFmtOptions{} })

	out := formatStorage([]byte(raw))
	if !strings.Contains(out, "nickel_ore") || !strings.Contains(out, "iron_ore") {
		t.Errorf("filtered output should keep ore items, got:\n%s", out)
	}
	if strings.Contains(out, "copper_wiring") {
		t.Errorf("filtered output should drop non-matching items, got:\n%s", out)
	}
	if !strings.Contains(out, "2/3 types match") {
		t.Errorf("header should report match count, got:\n%s", out)
	}
}

// TestFormatStorage_FilterByName matches the display name, not just item_id.
func TestFormatStorage_FilterByName(t *testing.T) {
	raw := `{"base_id":"central_nexus","items":[
		{"item_id":"steel_plate","name":"Steel Plate","quantity":470,"size":1},
		{"item_id":"nickel_ore","name":"Nickel Ore","quantity":25554,"size":1}
	]}`

	storageFmtOpts = storageFmtOptions{filter: "plate"}
	t.Cleanup(func() { storageFmtOpts = storageFmtOptions{} })

	out := formatStorage([]byte(raw))
	if !strings.Contains(out, "steel_plate") || strings.Contains(out, "nickel_ore") {
		t.Errorf("name filter should keep only Steel Plate, got:\n%s", out)
	}
}

// TestFormatStorage_Group renders catalog-derived category sections, with
// uncatalogued items bucketed under "unknown" and listed last.
func TestFormatStorage_Group(t *testing.T) {
	// Override the catalog lookup deterministically and prevent the lazy
	// loader from clobbering it.
	itemCategoriesOnce.Do(func() {})
	itemCategories = map[string]string{
		"nickel_ore":  "mineral",
		"steel_plate": "material",
	}

	raw := `{"base_id":"central_nexus","items":[
		{"item_id":"nickel_ore","name":"Nickel Ore","quantity":25554,"size":1},
		{"item_id":"steel_plate","name":"Steel Plate","quantity":470,"size":1},
		{"item_id":"mystery_widget","name":"Mystery Widget","quantity":3,"size":1}
	]}`

	storageFmtOpts = storageFmtOptions{group: true}
	t.Cleanup(func() { storageFmtOpts = storageFmtOptions{} })

	out := formatStorage([]byte(raw))
	for _, want := range []string{"material", "mineral", "unknown", "in 3 categories"} {
		if !strings.Contains(out, want) {
			t.Errorf("grouped output missing %q, got:\n%s", want, out)
		}
	}
	// "unknown" bucket must come after named categories.
	if i, j := strings.Index(out, "material"), strings.Index(out, "unknown"); i < 0 || j < 0 || i > j {
		t.Errorf("named categories should precede 'unknown', got:\n%s", out)
	}
}

func TestFormatFactionStorage_ErrorFrameRendersNothing(t *testing.T) {
	if out := formatFactionStorage([]byte(`{"error":"not_member","message":"You are not in a faction."}`)); out != "" {
		t.Errorf("expected empty string for error frame, got:\n%s", out)
	}
	if out := formatFactionStorage([]byte(`{}`)); out != "" {
		t.Errorf("expected empty string for empty object, got:\n%s", out)
	}
}

// TestFormatFactionStorage_Filter limits the faction items table to matching
// item_id/name and reports the match count.
func TestFormatFactionStorage_Filter(t *testing.T) {
	raw := `{"faction_id":"f1","faction_name":"Cult","faction_tag":"CULT","base_id":"central_nexus","credits":1000,"items":[
		{"item_id":"nickel_ore","name":"Nickel Ore","quantity":25554,"size":1},
		{"item_id":"copper_wiring","name":"Copper Wiring","quantity":1868,"size":1}
	]}`

	storageFmtOpts = storageFmtOptions{filter: "ore"}
	t.Cleanup(func() { storageFmtOpts = storageFmtOptions{} })

	out := formatFactionStorage([]byte(raw))
	if !strings.Contains(out, "nickel_ore") || strings.Contains(out, "copper_wiring") {
		t.Errorf("filter should keep only ore items, got:\n%s", out)
	}
	if !strings.Contains(out, "1/2 types match") {
		t.Errorf("header should report match count, got:\n%s", out)
	}
}

// TestFormatFactionStorage_Group renders catalog-derived category sections with
// uncatalogued items under "unknown", listed last.
func TestFormatFactionStorage_Group(t *testing.T) {
	itemCategoriesOnce.Do(func() {})
	itemCategories = map[string]string{"nickel_ore": "mineral"}

	raw := `{"faction_id":"f1","faction_name":"Cult","faction_tag":"CULT","base_id":"central_nexus","credits":0,"items":[
		{"item_id":"nickel_ore","name":"Nickel Ore","quantity":25554,"size":1},
		{"item_id":"mystery_widget","name":"Mystery Widget","quantity":3,"size":1}
	]}`

	storageFmtOpts = storageFmtOptions{group: true}
	t.Cleanup(func() { storageFmtOpts = storageFmtOptions{} })

	out := formatFactionStorage([]byte(raw))
	for _, want := range []string{"mineral", "unknown", "in 2 categories"} {
		if !strings.Contains(out, want) {
			t.Errorf("grouped output missing %q, got:\n%s", want, out)
		}
	}
	if i, j := strings.Index(out, "mineral"), strings.Index(out, "unknown"); i < 0 || j < 0 || i > j {
		t.Errorf("named categories should precede 'unknown', got:\n%s", out)
	}
}
