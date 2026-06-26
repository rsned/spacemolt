package main

import (
	"testing"
	"time"
)

func TestNextScanAt(t *testing.T) {
	const offset = 5 * time.Minute
	const interval = 30 * time.Minute
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			// Before this boundary's offset: fire at :00 + 5m = :05.
			name: "before offset",
			now:  time.Date(2026, 6, 26, 14, 2, 0, 0, time.UTC),
			want: time.Date(2026, 6, 26, 14, 5, 0, 0, time.UTC),
		},
		{
			// Past :05 but before :30: next is :30 + 5m = :35.
			name: "past offset rolls to next boundary",
			now:  time.Date(2026, 6, 26, 14, 12, 0, 0, time.UTC),
			want: time.Date(2026, 6, 26, 14, 35, 0, 0, time.UTC),
		},
		{
			// Exactly on the offset instant counts as passed → next boundary.
			name: "exactly on target rolls forward",
			now:  time.Date(2026, 6, 26, 14, 5, 0, 0, time.UTC),
			want: time.Date(2026, 6, 26, 14, 35, 0, 0, time.UTC),
		},
		{
			// Late in the hour rolls across the hour boundary: 15:00 + 5m.
			name: "crosses the hour",
			now:  time.Date(2026, 6, 26, 14, 48, 0, 0, time.UTC),
			want: time.Date(2026, 6, 26, 15, 5, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextScanAt(tc.now, interval, offset); !got.Equal(tc.want) {
				t.Errorf("nextScanAt(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestNextScanAtAlwaysFuture(t *testing.T) {
	now := time.Date(2026, 6, 26, 14, 5, 0, 1, time.UTC)
	got := nextScanAt(now, 15*time.Minute, time.Minute)
	if !got.After(now) {
		t.Errorf("nextScanAt must be strictly after now: got %v, now %v", got, now)
	}
}
