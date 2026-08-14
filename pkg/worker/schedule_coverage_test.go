package worker

import (
	"path/filepath"
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

// TestRetireCoveredDropsTheRedundantOneAndKeepsTheFiner is the role-cadence
// change: raising `resident` from hourly to ten_minutely seeds the finer entry
// beside the agent's existing hourly one, and every hourly firing then coincides
// with a ten-minutely firing in the same pass.
func TestRetireCoveredDropsTheRedundantOneAndKeepsTheFiner(t *testing.T) {
	s, err := LoadScheduler(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	mustAdd(t, s, "hourly", "update_market", now)
	mustAdd(t, s, "ten_minutely", "update_market", now)
	mustAdd(t, s, "hourly", "capture_profile", now)

	dropped := s.RetireCovered()
	if len(dropped) != 1 || dropped[0].Frequency != "hourly" || dropped[0].Command != "update_market" {
		t.Fatalf("dropped %+v; want exactly the hourly update_market", dropped)
	}
	var freqs []string
	for _, task := range s.List() {
		if task.Command == "update_market" {
			freqs = append(freqs, task.Frequency)
		}
	}
	if len(freqs) != 1 || freqs[0] != "ten_minutely" {
		t.Errorf("update_market left as %v; the finer schedule must be the survivor", freqs)
	}
	// An unrelated command must be untouched.
	var sawProfile bool
	for _, task := range s.List() {
		if task.Command == "capture_profile" {
			sawProfile = true
		}
	}
	if !sawProfile {
		t.Error("retired capture_profile; nothing covers it")
	}
}

// TestRetireCoveredHandlesTheRoleChangeResidue — miner-2's case. It left the
// unlock role (daily kb_update) for resident (hourly kb_update) and carried both.
func TestRetireCoveredHandlesTheRoleChangeResidue(t *testing.T) {
	s, _ := LoadScheduler(filepath.Join(t.TempDir(), "s.json"))
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	mustAdd(t, s, "daily", "kb_update", now)
	mustAdd(t, s, "hourly", "kb_update", now)

	s.RetireCovered()
	got := s.List()
	if len(got) != 1 || got[0].Frequency != "hourly" {
		t.Fatalf("left %+v; hourly covers daily, so only the hourly kb_update should remain", got)
	}
}

// TestRetireCoveredKeepsIncomparableFrequencies. ten_minutely and
// quarter_hourly do not cover each other (:10 is no quarter mark, :15 is no ten
// mark), so retiring either would lose real firings.
func TestRetireCoveredKeepsIncomparableFrequencies(t *testing.T) {
	s, _ := LoadScheduler(filepath.Join(t.TempDir(), "s.json"))
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	mustAdd(t, s, "ten_minutely", "update_market", now)
	mustAdd(t, s, "quarter_hourly", "update_market", now)

	if dropped := s.RetireCovered(); len(dropped) != 0 {
		t.Errorf("dropped %+v; neither frequency covers the other", dropped)
	}
	if len(s.List()) != 2 {
		t.Errorf("left %d tasks, want both", len(s.List()))
	}
}

func mustAdd(t *testing.T, s *Scheduler, freq, cmd string, now time.Time) {
	t.Helper()
	if _, err := s.Add(freq, cmd, now); err != nil {
		t.Fatalf("Add(%s,%s): %v", freq, cmd, err)
	}
}
