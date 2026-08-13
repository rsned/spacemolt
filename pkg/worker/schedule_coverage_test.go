package worker

import (
	"testing"
	"time"
)

// TestCoversMatchesActualBoundaries checks Covers against the thing it claims to
// summarize: the boundaries CurrentBoundary actually produces. Covers is a
// divisibility shortcut, and a shortcut that drifts from the real schedule is
// worse than no shortcut, so this walks a full week of minutes and asserts the
// two agree for every ordered pair of frequencies.
func TestCoversMatchesActualBoundaries(t *testing.T) {
	freqs := []string{"ten_minutely", "quarter_hourly", "half_hourly", "hourly", "daily", "weekly"}
	// A Sunday, so the weekly anchor falls inside the window.
	start := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	// boundaries[f] is every instant f fires at during the week.
	boundaries := map[string]map[time.Time]bool{}
	for _, f := range freqs {
		boundaries[f] = map[time.Time]bool{}
	}
	for m := 0; m < 7*24*60; m++ {
		now := start.Add(time.Duration(m) * time.Minute)
		for _, f := range freqs {
			if b := CurrentBoundary(f, now); b.Equal(now) {
				boundaries[f][now] = true
			}
		}
	}

	for _, have := range freqs {
		for _, want := range freqs {
			// True iff every boundary of want is also a boundary of have.
			actual := true
			for b := range boundaries[want] {
				if !boundaries[have][b] {
					actual = false
					break
				}
			}
			if got := Covers(have, want); got != actual {
				t.Errorf("Covers(%s, %s) = %v; walking the real boundaries says %v",
					have, want, got, actual)
			}
		}
	}
}

// TestTenMinutelyAndQuarterHourlyDoNotCoverEachOther pins the one pair that a
// naive coarse/fine ordering gets wrong. :10 is not a quarter mark and :15 is
// not a ten mark, so neither subsumes the other and dropping either would lose
// real firings — a plain "keep the finest" rule would silently do exactly that.
func TestTenMinutelyAndQuarterHourlyDoNotCoverEachOther(t *testing.T) {
	if Covers("ten_minutely", "quarter_hourly") {
		t.Error("ten_minutely claimed to cover quarter_hourly, but nothing fires at :15")
	}
	if Covers("quarter_hourly", "ten_minutely") {
		t.Error("quarter_hourly claimed to cover ten_minutely, but nothing fires at :10")
	}
}

// TestCoversRejectsUnknownFrequencies keeps a typo from reading as "covered",
// which would silently drop a role's schedule entry instead of scheduling it.
func TestCoversRejectsUnknownFrequencies(t *testing.T) {
	if Covers("fortnightly", "hourly") || Covers("hourly", "fortnightly") {
		t.Error("an unknown frequency must neither cover nor be covered")
	}
}
