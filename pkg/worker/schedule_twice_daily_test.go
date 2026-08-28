package worker

import (
	"testing"
	"time"
)

func TestTwiceDaily_IsAValidFrequency(t *testing.T) {
	if !ValidFrequencies["twice_daily"] {
		t.Fatal("twice_daily is not in ValidFrequencies")
	}
	if got := FrequencyPeriod("twice_daily"); got != 12*time.Hour {
		t.Errorf("FrequencyPeriod(twice_daily) = %v, want 12h", got)
	}
}

// Boundaries are midnight and noon UTC, so twice_daily's anchor is still a
// multiple of every shorter period -- the property Covers relies on.
func TestTwiceDaily_BoundariesAreMidnightAndNoon(t *testing.T) {
	cases := []struct {
		now, want string
	}{
		{"2026-08-28T00:00:00Z", "2026-08-28T00:00:00Z"},
		{"2026-08-28T11:59:59Z", "2026-08-28T00:00:00Z"},
		{"2026-08-28T12:00:00Z", "2026-08-28T12:00:00Z"},
		{"2026-08-28T23:59:59Z", "2026-08-28T12:00:00Z"},
	}
	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339, c.now)
		want, _ := time.Parse(time.RFC3339, c.want)
		if got := CurrentBoundary("twice_daily", now); !got.Equal(want) {
			t.Errorf("CurrentBoundary(twice_daily, %s) = %s, want %s", c.now, got, want)
		}
	}
	now, _ := time.Parse(time.RFC3339, "2026-08-28T09:30:00Z")
	want, _ := time.Parse(time.RFC3339, "2026-08-28T12:00:00Z")
	if got := NextBoundary("twice_daily", now); !got.Equal(want) {
		t.Errorf("NextBoundary = %s, want %s", got, want)
	}
}

// Ordering must slot cleanly between hourly and daily in both directions, or
// the seed/RetireCovered logic will duplicate or wrongly retire entries.
func TestTwiceDaily_CoversOrdering(t *testing.T) {
	if !Covers("hourly", "twice_daily") {
		t.Error("hourly should cover twice_daily (12h is a multiple of 1h)")
	}
	if !Covers("twice_daily", "daily") {
		t.Error("twice_daily should cover daily (24h is a multiple of 12h)")
	}
	if Covers("daily", "twice_daily") {
		t.Error("daily must NOT cover twice_daily -- daily fires once, twice_daily twice")
	}
	if Covers("twice_daily", "hourly") {
		t.Error("twice_daily must NOT cover hourly")
	}
}

// FrequencyPeriod must stay consistent with the boundary grid for every
// frequency, including the new one -- one source of truth, not two.
func TestFrequencyPeriod_MatchesGridForAll(t *testing.T) {
	base := time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)
	for freq := range ValidFrequencies {
		want := NextBoundary(freq, base).Sub(CurrentBoundary(freq, base))
		if got := FrequencyPeriod(freq); got != want {
			t.Errorf("FrequencyPeriod(%q) = %v, but grid spacing is %v", freq, got, want)
		}
	}
}
