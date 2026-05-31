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

func TestFormatFactionStorage_ErrorFrameRendersNothing(t *testing.T) {
	if out := formatFactionStorage([]byte(`{"error":"not_member","message":"You are not in a faction."}`)); out != "" {
		t.Errorf("expected empty string for error frame, got:\n%s", out)
	}
	if out := formatFactionStorage([]byte(`{}`)); out != "" {
		t.Errorf("expected empty string for empty object, got:\n%s", out)
	}
}
