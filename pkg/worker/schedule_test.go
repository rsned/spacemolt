package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCurrentBoundary(t *testing.T) {
	// 2026-05-31 is a Sunday. 2026-05-28 is a Thursday.
	now := time.Date(2026, 5, 28, 14, 37, 12, 0, time.UTC) // Thursday 14:37
	cases := []struct {
		freq string
		want time.Time
	}{
		{"quarter_hourly", time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)}, // 14:37 → 14:30
		{"half_hourly", time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)},    // 14:37 → 14:30
		{"hourly", time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)},
		{"daily", time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)},
		{"weekly", time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)}, // previous Sunday
	}
	for _, tc := range cases {
		if got := CurrentBoundary(tc.freq, now); !got.Equal(tc.want) {
			t.Errorf("CurrentBoundary(%q, %v) = %v, want %v", tc.freq, now, got, tc.want)
		}
	}
}

func TestHalfHourlyBoundaries(t *testing.T) {
	// Before the half-hour: floor to :00, next is :30.
	early := time.Date(2026, 5, 28, 14, 12, 0, 0, time.UTC)
	if got := CurrentBoundary("half_hourly", early); !got.Equal(time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("14:12 current = %v, want 14:00", got)
	}
	if got := NextBoundary("half_hourly", early); !got.Equal(time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)) {
		t.Errorf("14:12 next = %v, want 14:30", got)
	}
	// After the half-hour: floor to :30, next rolls to the next hour's :00.
	late := time.Date(2026, 5, 28, 14, 45, 0, 0, time.UTC)
	if got := CurrentBoundary("half_hourly", late); !got.Equal(time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)) {
		t.Errorf("14:45 current = %v, want 14:30", got)
	}
	if got := NextBoundary("half_hourly", late); !got.Equal(time.Date(2026, 5, 28, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("14:45 next = %v, want 15:00", got)
	}
}

func TestQuarterHourlyBoundaries(t *testing.T) {
	// 14:12 → floor to :00, next is :15.
	a := time.Date(2026, 5, 28, 14, 12, 0, 0, time.UTC)
	if got := CurrentBoundary("quarter_hourly", a); !got.Equal(time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("14:12 current = %v, want 14:00", got)
	}
	if got := NextBoundary("quarter_hourly", a); !got.Equal(time.Date(2026, 5, 28, 14, 15, 0, 0, time.UTC)) {
		t.Errorf("14:12 next = %v, want 14:15", got)
	}
	// 14:47 → floor to :45, next rolls across the hour to 15:00.
	b := time.Date(2026, 5, 28, 14, 47, 0, 0, time.UTC)
	if got := CurrentBoundary("quarter_hourly", b); !got.Equal(time.Date(2026, 5, 28, 14, 45, 0, 0, time.UTC)) {
		t.Errorf("14:47 current = %v, want 14:45", got)
	}
	if got := NextBoundary("quarter_hourly", b); !got.Equal(time.Date(2026, 5, 28, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("14:47 next = %v, want 15:00", got)
	}
}

func TestCurrentBoundaryWeeklyOnSunday(t *testing.T) {
	now := time.Date(2026, 5, 31, 9, 0, 0, 0, time.UTC) // Sunday 09:00
	want := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	if got := CurrentBoundary("weekly", now); !got.Equal(want) {
		t.Errorf("weekly on Sunday = %v, want %v (same-day midnight)", got, want)
	}
}

func TestSchedulerDueFreshAndAfterRun(t *testing.T) {
	s := newTestScheduler(t)
	now := time.Date(2026, 5, 28, 14, 37, 0, 0, time.UTC)
	task, err := s.Add("hourly", "get_nearby", now)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Add stamps LastRun=now (run-immediately semantics), so not due this hour.
	if due := s.Due(now); len(due) != 0 {
		t.Fatalf("just-added task should not be due this period, got %v", taskIDs(due))
	}

	// Next hour boundary passes -> due.
	next := time.Date(2026, 5, 28, 15, 0, 1, 0, time.UTC)
	due := s.Due(next)
	if len(due) != 1 || due[0].ID != task.ID {
		t.Fatalf("expected task %d due at next hour, got %v", task.ID, taskIDs(due))
	}
}

func TestSchedulerCheckDueCollapsesMissedPeriods(t *testing.T) {
	s := newTestScheduler(t)
	addTime := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	if _, err := s.Add("daily", "view_storage", addTime); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// play_as comes back 3 days later — 3 daily boundaries were missed.
	now := time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC)
	due := s.checkDue(now)
	if len(due) != 1 {
		t.Fatalf("3 missed days should collapse to ONE due run, got %d", len(due))
	}

	// Immediately checking again yields nothing (LastRun was stamped).
	if again := s.checkDue(now); len(again) != 0 {
		t.Fatalf("second checkDue same instant should be empty, got %d", len(again))
	}
}

func TestSchedulerPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scheduled_commands.json")

	s, err := LoadScheduler(path)
	if err != nil {
		t.Fatalf("LoadScheduler(missing): %v", err)
	}
	if len(s.List()) != 0 {
		t.Fatalf("fresh scheduler should be empty")
	}

	now := time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)
	a, _ := s.Add("hourly", "get_nearby", now)
	b, _ := s.Add("daily", "view_storage --station_id treasure_cache_trading_post", now)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}

	// Reload sees both, ids stable, command with flags preserved.
	s2, err := LoadScheduler(path)
	if err != nil {
		t.Fatalf("LoadScheduler(reopen): %v", err)
	}
	got := s2.List()
	if len(got) != 2 {
		t.Fatalf("reloaded %d tasks, want 2", len(got))
	}
	byID := map[int]ScheduledTask{}
	for _, ts := range got {
		byID[ts.ID] = ts
	}
	if byID[b.ID].Command != "view_storage --station_id treasure_cache_trading_post" {
		t.Errorf("command with flags not preserved: %q", byID[b.ID].Command)
	}

	// Remove one, reload, only the other remains.
	if !s2.Remove(a.ID) {
		t.Fatalf("Remove(%d) returned false", a.ID)
	}
	s3, _ := LoadScheduler(path)
	if len(s3.List()) != 1 || s3.List()[0].ID != b.ID {
		t.Fatalf("after remove, want only task %d, got %v", b.ID, taskIDs(s3.List()))
	}
}

func TestSchedulerAddValidatesFrequency(t *testing.T) {
	s := newTestScheduler(t)
	now := time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)
	if _, err := s.Add("monthly", "get_nearby", now); err == nil {
		t.Fatal("Add with invalid frequency should error")
	}
	if _, err := s.Add("hourly", "", now); err == nil {
		t.Fatal("Add with empty command should error")
	}
}

func TestSchedulerAddRejectsDuplicates(t *testing.T) {
	s := newTestScheduler(t)
	now := time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)

	if _, err := s.Add("hourly", "update_market", now); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	// Same freq + same command is a complete duplicate -> rejected.
	if _, err := s.Add("hourly", "update_market", now); err == nil {
		t.Fatal("duplicate Add should error")
	}
	// Normalization: surrounding whitespace and frequency case don't matter.
	if _, err := s.Add("HOURLY", "  update_market  ", now); err == nil {
		t.Fatal("duplicate Add (after normalization) should error")
	}
	if len(s.List()) != 1 {
		t.Fatalf("duplicates should not be stored, got %d tasks", len(s.List()))
	}

	// Same command at a different frequency is NOT a duplicate.
	if _, err := s.Add("daily", "update_market", now); err != nil {
		t.Fatalf("same command different freq should be allowed: %v", err)
	}
	// Different command at the same frequency is NOT a duplicate.
	if _, err := s.Add("hourly", "get_nearby", now); err != nil {
		t.Fatalf("different command same freq should be allowed: %v", err)
	}
	if len(s.List()) != 3 {
		t.Fatalf("want 3 distinct tasks, got %d", len(s.List()))
	}
}

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	s, err := LoadScheduler(filepath.Join(t.TempDir(), "scheduled_commands.json"))
	if err != nil {
		t.Fatalf("LoadScheduler: %v", err)
	}
	return s
}

func taskIDs(tasks []ScheduledTask) []int {
	out := make([]int, len(tasks))
	for i, ts := range tasks {
		out[i] = ts.ID
	}
	return out
}
