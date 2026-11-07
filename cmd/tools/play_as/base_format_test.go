package main

import (
	"strings"
	"testing"
)

// Crix Stronghold Station's reply, 2026-11-07. It sends no dining_points.
func TestFormatBaseShowsTourism(t *testing.T) {
	raw := []byte(`{"base":{"name":"Crix Stronghold Station"},"leisure_points":2,"tourism_revenue":88951}`)

	got := formatBase(raw)
	if !strings.Contains(got, "Tourism:  88,951 cr lifetime, leisure 2") {
		t.Fatalf("tourism line missing:\n%s", got)
	}
	if strings.Contains(got, "dining") {
		t.Fatalf("an absent score must not print:\n%s", got)
	}
	if !strings.Contains(got, `"name": "Crix Stronghold Station"`) {
		t.Fatalf("the full reply must still print:\n%s", got)
	}
}

// With no tourism data the formatter declines, so the plain JSON view stands.
func TestFormatBaseDeclinesWithoutTourism(t *testing.T) {
	if got := formatBase([]byte(`{"base":{"name":"Conjunction Junction"}}`)); got != "" {
		t.Fatalf("want no styled view, got:\n%s", got)
	}
}

func TestFormatBaseShowsDiningOnly(t *testing.T) {
	got := formatBase([]byte(`{"base":{"name":"Hub"},"dining_points":10}`))
	if !strings.Contains(got, "Tourism:  dining 10") {
		t.Fatalf("want the dining score alone:\n%s", got)
	}
}
