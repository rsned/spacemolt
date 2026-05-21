package main

import (
	"strings"
	"testing"
	"time"
)

func TestRenderIndexHTML(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	cards := []indexCard{
		{Tag: "CRFT", Name: "Crafters Union", Treasury: 1240500, Members: 12, CapturedAt: now},
		{Tag: "XPLR", Name: "Explorers", Treasury: 50000, Members: 4, CapturedAt: now},
	}
	html, err := renderIndexHTML(cards)
	if err != nil {
		t.Fatalf("renderIndexHTML: %v", err)
	}
	for _, want := range []string{"CRFT", "Crafters Union", "XPLR", "faction-CRFT.html", "faction-XPLR.html"} {
		if !strings.Contains(html, want) {
			t.Errorf("index HTML missing %q", want)
		}
	}
}
