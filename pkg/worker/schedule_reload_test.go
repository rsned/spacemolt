package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTasks writes a task list the way an operator edits schedule.json by
// hand, then pushes its mtime forward so the change is visible even on a
// filesystem with coarse timestamps.
func writeTasks(t *testing.T, path string, tasks []ScheduledTask, mtime time.Time) {
	t.Helper()
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// A running worker loaded schedule.json once at startup and never again, so
// hand-added tasks sat unrun until the next relaunch. Observed 2026-10-29/30:
// five crafts on miningbot_0000 and four delivers on trader-2 never fired for
// two days. An edit to the file must reach the running scheduler.
func TestSchedulerPicksUpTaskAddedToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	created := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	writeTasks(t, path, []ScheduledTask{{ID: 1, Frequency: "hourly", Command: "kb_update", CreatedAt: created}}, created)

	s, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 3, 30, 0, 0, time.UTC)
	s.checkDue(now) // task 1 runs; it is not due again this hour

	writeTasks(t, path, []ScheduledTask{
		{ID: 1, Frequency: "hourly", Command: "kb_update", CreatedAt: created},
		{ID: 2, Frequency: "daily", Command: "deliver ceramite_plating 1132 a b c", CreatedAt: now},
	}, now.Add(time.Minute))

	due := s.checkDue(now.Add(2 * time.Minute))
	if len(due) != 1 || due[0].ID != 2 {
		t.Fatalf("after adding task 2 to the file, due = %+v, want only task 2", due)
	}
}

// Reloading must keep what the scheduler knows about runs: a task already run
// this period must not fire again just because the file was re-read.
func TestSchedulerReloadKeepsLastRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	created := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	tasks := []ScheduledTask{{ID: 1, Frequency: "hourly", Command: "kb_update", CreatedAt: created}}
	writeTasks(t, path, tasks, created)

	s, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 3, 30, 0, 0, time.UTC)
	s.checkDue(now)

	writeTasks(t, path, tasks, now.Add(time.Minute)) // touched, unchanged
	if due := s.checkDue(now.Add(2 * time.Minute)); len(due) != 0 {
		t.Errorf("re-read file re-fired an already-run task: %+v", due)
	}
}

// A half-written or mistyped edit must not wipe the running schedule.
func TestSchedulerReloadIgnoresCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	created := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	writeTasks(t, path, []ScheduledTask{{ID: 1, Frequency: "hourly", Command: "kb_update", CreatedAt: created}}, created)

	s, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 3, 30, 0, 0, time.UTC)
	if err := os.WriteFile(path, []byte("[{\"id\": 1,"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}

	if got := s.List(); len(got) != 1 || got[0].Command != "kb_update" {
		t.Fatalf("corrupt file replaced the schedule: %+v", got)
	}
	if due := s.checkDue(now); len(due) != 1 {
		t.Errorf("corrupt file stopped the existing task from running: %+v", due)
	}
}

// The scheduler's own writes (Add) must not read back as an outside edit and
// re-fire the task it just stamped.
func TestSchedulerOwnSaveIsNotAReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	s, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 3, 30, 0, 0, time.UTC)
	if _, err := s.Add("hourly", "kb_update", now); err != nil {
		t.Fatal(err)
	}
	if due := s.checkDue(now.Add(time.Minute)); len(due) != 0 {
		t.Errorf("Add's own save was treated as an edit and re-fired: %+v", due)
	}
}

// Add, Remove and RetireCovered save the whole in-memory list. Without a
// reload first, a task an operator had just added by hand would be written
// over and lost.
func TestSchedulerAddKeepsHandEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	created := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	writeTasks(t, path, []ScheduledTask{{ID: 1, Frequency: "hourly", Command: "kb_update", CreatedAt: created}}, created)
	s, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 3, 30, 0, 0, time.UTC)
	writeTasks(t, path, []ScheduledTask{
		{ID: 1, Frequency: "hourly", Command: "kb_update", CreatedAt: created},
		{ID: 2, Frequency: "daily", Command: "view_market", CreatedAt: now},
	}, now)

	if _, addErr := s.Add("hourly", "facilities", now.Add(time.Minute)); addErr != nil {
		t.Fatal(addErr)
	}
	reread, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for _, task := range reread.List() {
		cmds = append(cmds, task.Command)
	}
	if len(cmds) != 3 {
		t.Fatalf("file after Add = %v, want kb_update, view_market and facilities", cmds)
	}
}
