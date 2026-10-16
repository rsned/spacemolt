package main

import (
	"strings"
	"testing"
)

// undockedStorageFixture is the real shape view_storage returns when NOT
// docked, trimmed to four locations. The distinguishing features are an EMPTY
// base_id with empty items and ships -- which is also what a "you must be
// docked" error looks like -- plus a locations array that is the whole point of
// the response.
const undockedStorageFixture = `{
  "action": "view_storage",
  "base_id": "",
  "hint": "3,537,819 items in storage at 59b102279f508c17831e557d3df6ad88, gold_run_extraction_hub\nDock, or pass station_id, to read one station's contents.",
  "items": [],
  "ships": [],
  "locations": [
    {"base_id":"the_rampart_checkpoint","base_name":"The Rampart Checkpoint","item_count":150,"ship_count":0,"system":"the_rampart","system_name":"The Rampart"},
    {"base_id":"gold_run_extraction_hub","base_name":"Gold Run Extraction Hub","item_count":1748724,"ship_count":0,"system":"gold_run","system_name":"Gold Run"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","item_count":570688,"ship_count":13,"system":"haven","system_name":"Haven"},
    {"base_id":"59b102279f508c17831e557d3df6ad88","base_name":"The Veil Anchor","item_count":2500,"ship_count":0,"system":"bd20_2457","system_name":"BD+20 2457"}
  ]
}`

// TestFormatStorage_UndockedRendersLocationIndex covers the undocked variant.
//
// It used to fall through to raw JSON: the guard against non-storage frames
// keys on "no base_id and no items and no ships", which this response matches
// exactly, so a legitimate galaxy-wide storage index was indistinguishable
// from a must-be-docked error.
func TestFormatStorage_UndockedRendersLocationIndex(t *testing.T) {
	out := formatStorage([]byte(undockedStorageFixture))
	if out == "" {
		t.Fatal("formatStorage returned empty; undocked response fell through to raw JSON")
	}

	// Totals come from the locations, summed -- not from the hint, which is
	// prose and covers stations this page may not list.
	for _, want := range []string{
		"4 locations",
		"2,322,062", // 150 + 1748724 + 570688 + 2500
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// Human-readable name and system, with counts grouped for legibility.
	for _, want := range []string{
		"Gold Run Extraction Hub", "Gold Run", "1,748,724",
		"Grand Exchange Station", "Haven", "570,688",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// Ships are worth surfacing: 13 hulls parked at Grand Exchange is the kind
	// of thing this index exists to reveal.
	if !strings.Contains(out, "13") {
		t.Errorf("ship count not rendered in:\n%s", out)
	}

	// Biggest holding first -- the index is for deciding where to go.
	if a, b := strings.Index(out, "Gold Run Extraction Hub"), strings.Index(out, "The Rampart Checkpoint"); a > b {
		t.Errorf("locations not sorted by item_count desc:\n%s", out)
	}

	// The base_id must be present: it is what you pass to station_id.
	if !strings.Contains(out, "gold_run_extraction_hub") {
		t.Errorf("base_id not rendered; it is the station_id argument:\n%s", out)
	}
}

// TestFormatStorage_StillRejectsNonStorageFrames guards the regression risk of
// the fix above: a must-be-docked error has no locations either, and must keep
// falling through rather than printing an empty table.
func TestFormatStorage_StillRejectsNonStorageFrames(t *testing.T) {
	if got := formatStorage([]byte(`{"error":"You must be docked to view storage"}`)); got != "" {
		t.Errorf("non-storage frame rendered %q, want empty so the caller falls through", got)
	}
}
