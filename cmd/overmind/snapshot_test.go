package main

import (
	"testing"
	"time"
)

func TestCrossedQuarterBoundary(t *testing.T) {
	q := func(s string) time.Time {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return tm
	}
	// last snapshot 10:14:59, now 10:15:01 -> crossed the :15 boundary
	if !crossedQuarterBoundary(q("2026-06-26T10:14:59Z"), q("2026-06-26T10:15:01Z")) {
		t.Fatal("want crossed at :15")
	}
	// same quarter -> not crossed
	if crossedQuarterBoundary(q("2026-06-26T10:15:01Z"), q("2026-06-26T10:16:00Z")) {
		t.Fatal("want not crossed within the same quarter")
	}
	// zero last (startup) -> snapshot once
	if !crossedQuarterBoundary(time.Time{}, q("2026-06-26T10:16:00Z")) {
		t.Fatal("want crossed on startup (zero last)")
	}
	// later quarter, different hour -> crossed
	if !crossedQuarterBoundary(q("2026-06-26T10:59:00Z"), q("2026-06-26T11:00:01Z")) {
		t.Fatal("want crossed across the hour")
	}
}
