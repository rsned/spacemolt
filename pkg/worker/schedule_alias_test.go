package worker

import (
	"strings"
	"testing"
	"time"
)

// Operators type "10m" and "30m", not "ten_minutely"; the scheduler must
// accept the short forms everywhere it is used, not just in one tool.
func TestSchedulerAddAcceptsFrequencyAliases(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"10m": "ten_minutely", "10min": "ten_minutely",
		"15m": "quarter_hourly", "15min": "quarter_hourly",
		"30m": "half_hourly", "30min": "half_hourly", "halfhour": "half_hourly",
		"1h": "hourly", "hour": "hourly",
		"12h": "twice_daily",
		"1d":  "daily", "24h": "daily", "day": "daily",
		"1w": "weekly", "week": "weekly",
		"HOURLY": "hourly", // canonical names stay valid, case-insensitively
	}
	for alias, want := range cases {
		s := newTestScheduler(t)
		task, err := s.Add(alias, "get_nearby", now)
		if err != nil {
			t.Errorf("Add(%q): %v", alias, err)
			continue
		}
		if task.Frequency != want {
			t.Errorf("Add(%q).Frequency = %q, want %q", alias, task.Frequency, want)
		}
	}
}

func TestSchedulerAddRejectsUnknownAliasNamingEveryFrequency(t *testing.T) {
	s := newTestScheduler(t)
	_, err := s.Add("45m", "get_nearby", time.Now())
	if err == nil {
		t.Fatal("Add(\"45m\") should fail")
	}
	for _, name := range []string{"ten_minutely", "quarter_hourly", "half_hourly", "hourly", "twice_daily", "daily", "weekly"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q should name %s", err.Error(), name)
		}
	}
}
