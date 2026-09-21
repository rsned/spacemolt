package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// last_run is runtime state, not configuration, and mixing the two made
// schedule.json rewrite itself on every tick. Across 169 workers that left 161
// tracked files permanently dirty in git: `checkDue` stamps last_run and calls
// saveLocked, so the task list -- which almost never changes -- was rewritten
// several times a minute purely to move a timestamp.
//
// The task file must be byte-stable while only timestamps move.
func TestCheckDue_DoesNotRewriteTheTaskFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedule.json")
	s, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	if _, err := s.Add("hourly", "capture_profile", base); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if due := s.checkDue(base.Add(2 * time.Hour)); len(due) != 1 {
		t.Fatalf("want the task due, got %d", len(due))
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("checkDue rewrote the task file.\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// The timestamp still has to survive a restart -- it is what stops a task
// re-firing immediately -- so it moves to a sibling file rather than being
// dropped.
func TestCheckDue_PersistsLastRunToTheStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedule.json")
	s, _ := LoadScheduler(path)
	base := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	task, _ := s.Add("hourly", "capture_profile", base)
	ran := base.Add(2 * time.Hour)
	s.checkDue(ran)

	raw, err := os.ReadFile(SchedulerStatePath(path))
	if err != nil {
		t.Fatalf("state file: %v", err)
	}
	var state map[string]time.Time
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("state file is not a timestamp map: %v (%s)", err, raw)
	}
	got, ok := state[strconv.Itoa(task.ID)]
	if !ok {
		t.Fatalf("no last_run recorded for task %d in %s", task.ID, raw)
	}
	if !got.Equal(ran) {
		t.Errorf("last_run = %v, want %v", got, ran)
	}

	// And a reload must see it, or the task fires again immediately.
	s2, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	if due := s2.checkDue(ran.Add(time.Minute)); len(due) != 0 {
		t.Errorf("task re-fired after reload; last_run did not survive: %+v", due)
	}
}

// Existing agents have last_run embedded in schedule.json and no state file.
// That timestamp must still be honoured on the first load after deploy --
// otherwise all 169 workers stampede every scheduled command at once.
func TestLoadScheduler_MigratesEmbeddedLastRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedule.json")
	legacy := `[{"id":1,"frequency":"hourly","command":"capture_profile",
	             "created_at":"2026-06-24T06:40:06Z","last_run":"2026-09-21T10:00:00Z"}]`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadScheduler(path)
	if err != nil {
		t.Fatal(err)
	}
	// 10:30 is inside the hour already run: nothing due.
	if due := s.checkDue(time.Date(2026, 9, 21, 10, 30, 0, 0, time.UTC)); len(due) != 0 {
		t.Fatalf("embedded last_run ignored; task would re-fire: %+v", due)
	}
}

// The state file is a cache of when things ran. Losing it must not lose the
// task list, and must not wedge the scheduler.
func TestLoadScheduler_MissingStateFileIsFine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedule.json")
	s, _ := LoadScheduler(path)
	base := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	s.Add("hourly", "capture_profile", base) //nolint:errcheck
	s.checkDue(base.Add(2 * time.Hour))

	if err := os.Remove(SchedulerStatePath(path)); err != nil {
		t.Fatal(err)
	}
	s2, err := LoadScheduler(path)
	if err != nil {
		t.Fatalf("a missing state file must not fail the load: %v", err)
	}
	if len(s2.List()) != 1 {
		t.Errorf("task list lost with the state file: %+v", s2.List())
	}
}
