package main

import "testing"

func TestTrendDirection(t *testing.T) {
	if got := trendDirection(10, 12); got != "↑" {
		t.Errorf("rising: want ↑, got %s", got)
	}
	if got := trendDirection(12, 10); got != "↓" {
		t.Errorf("falling: want ↓, got %s", got)
	}
	if got := trendDirection(10, 10); got != "→" {
		t.Errorf("flat: want →, got %s", got)
	}
}

func TestParseDemandHistoryOptions(t *testing.T) {
	opts, err := parseDemandHistoryOptions([]string{"iron_ore", "--station", "stn1", "--limit", "10"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.item != "iron_ore" || opts.station != "stn1" || opts.limit != 10 {
		t.Errorf("parsed wrong: %+v", opts)
	}

	// Default limit when omitted.
	opts, err = parseDemandHistoryOptions([]string{"copper"})
	if err != nil {
		t.Fatalf("parse default: %v", err)
	}
	if opts.limit != demandHistoryDefaultLimit {
		t.Errorf("default limit: want %d, got %d", demandHistoryDefaultLimit, opts.limit)
	}

	// Missing item is an error.
	if _, err := parseDemandHistoryOptions([]string{"--station", "stn1"}); err == nil {
		t.Error("missing item should error")
	}
}
