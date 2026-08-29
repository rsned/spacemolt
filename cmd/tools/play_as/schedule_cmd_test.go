package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/worker"
)

func newTestScheduler(t *testing.T) *worker.Scheduler {
	t.Helper()
	s, err := worker.LoadScheduler(filepath.Join(t.TempDir(), "scheduled_commands.json"))
	if err != nil {
		t.Fatalf("LoadScheduler: %v", err)
	}
	return s
}

func TestHandleScheduleAddRunsImmediatelyAndPersists(t *testing.T) {
	s := newTestScheduler(t)
	var ran []string
	runner := func(cmd string) { ran = append(ran, cmd) }
	now := time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)

	handleScheduleAdd(s, runner, now, []string{"daily", "view_storage", "--station_id", "treasure_cache_trading_post"})

	tasks := s.List()
	if len(tasks) != 1 {
		t.Fatalf("want 1 scheduled task, got %d", len(tasks))
	}
	if tasks[0].Frequency != "daily" {
		t.Errorf("frequency = %q, want daily", tasks[0].Frequency)
	}
	if tasks[0].Command != "view_storage --station_id treasure_cache_trading_post" {
		t.Errorf("command = %q, want full command with flags", tasks[0].Command)
	}
	if len(ran) != 1 || ran[0] != "view_storage --station_id treasure_cache_trading_post" {
		t.Errorf("run-immediately: runner calls = %v, want one matching the command", ran)
	}
}

func TestHandleScheduleAddRejectsBadFrequency(t *testing.T) {
	s := newTestScheduler(t)
	var ran []string
	runner := func(cmd string) { ran = append(ran, cmd) }
	now := time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)

	handleScheduleAdd(s, runner, now, []string{"monthly", "get_nearby"})

	if len(s.List()) != 0 {
		t.Errorf("bad frequency should not add a task")
	}
	if len(ran) != 0 {
		t.Errorf("bad frequency should not run the command, got %v", ran)
	}
}

func TestHandleScheduleAddRejectsSchedulingItself(t *testing.T) {
	s := newTestScheduler(t)
	now := time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)

	handleScheduleAdd(s, func(string) {}, now, []string{"hourly", "schedule_add", "daily", "get_nearby"})

	if len(s.List()) != 0 {
		t.Errorf("scheduling a schedule_* builtin should be rejected")
	}
}

func TestHandleScheduleAddAcceptsShortInterval(t *testing.T) {
	s := newTestScheduler(t)
	now := time.Date(2026, 8, 29, 12, 5, 0, 0, time.UTC)
	ran := 0
	handleScheduleAdd(s, func(string) { ran++ }, now, []string{"30m", "get_system_agents"})

	tasks := s.List()
	if len(tasks) != 1 {
		t.Fatalf("want 1 scheduled task, got %d", len(tasks))
	}
	if tasks[0].Frequency != "half_hourly" {
		t.Errorf("Frequency = %q, want half_hourly", tasks[0].Frequency)
	}
	if ran != 1 {
		t.Errorf("command should run once immediately, ran %d", ran)
	}
}
