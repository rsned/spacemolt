package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ScheduledTask is a recurring command the user has registered. The command is
// a full play_as command line (e.g. "view_storage --station_id frontier") run
// through executeLogicalCommand at the task's frequency.
type ScheduledTask struct {
	ID        int       `json:"id"`
	Frequency string    `json:"frequency"` // hourly | daily | weekly
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"created_at"`
	LastRun   time.Time `json:"last_run,omitzero"` // UTC; zero = never run
}

// validFrequencies is the closed set of supported frequencies.
var validFrequencies = map[string]bool{"hourly": true, "daily": true, "weekly": true}

// currentBoundary returns the most recent wall-clock boundary (UTC) for freq at
// or before now: the top of the hour, midnight, or the most recent Sunday
// midnight. Returns the zero time for an unknown frequency.
func currentBoundary(freq string, now time.Time) time.Time {
	now = now.UTC()
	switch freq {
	case "hourly":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.UTC)
	case "daily":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case "weekly":
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return day.AddDate(0, 0, -int(day.Weekday())) // Weekday: Sunday == 0
	default:
		return time.Time{}
	}
}

// nextBoundary returns the next time freq will fire strictly after now, used
// for the "next due" column in view_scheduled.
func nextBoundary(freq string, now time.Time) time.Time {
	cur := currentBoundary(freq, now)
	switch freq {
	case "hourly":
		return cur.Add(time.Hour)
	case "daily":
		return cur.AddDate(0, 0, 1)
	case "weekly":
		return cur.AddDate(0, 0, 7)
	default:
		return time.Time{}
	}
}

// Scheduler owns the persisted set of scheduled tasks for one agent.
type Scheduler struct {
	path  string
	mu    sync.Mutex
	tasks []ScheduledTask
}

// LoadScheduler reads the scheduler state at path. A missing file yields an
// empty scheduler with no error.
func LoadScheduler(path string) (*Scheduler, error) {
	s := &Scheduler{path: path}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scheduler: read %s: %w", path, err)
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.tasks); err != nil {
			return nil, fmt.Errorf("scheduler: parse %s: %w", path, err)
		}
	}
	return s, nil
}

// Add validates and appends a new task. With run-immediately semantics the
// caller runs the command at add time, so LastRun is stamped to now: the task
// will not fire again until the next boundary. Persists before returning.
func (s *Scheduler) Add(freq, command string, now time.Time) (ScheduledTask, error) {
	freq = strings.ToLower(strings.TrimSpace(freq))
	command = strings.TrimSpace(command)
	if !validFrequencies[freq] {
		return ScheduledTask{}, fmt.Errorf("unknown frequency %q (want hourly, daily, or weekly)", freq)
	}
	if command == "" {
		return ScheduledTask{}, fmt.Errorf("empty command")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	task := ScheduledTask{
		ID:        s.nextIDLocked(),
		Frequency: freq,
		Command:   command,
		CreatedAt: now.UTC(),
		LastRun:   now.UTC(),
	}
	s.tasks = append(s.tasks, task)
	if err := s.saveLocked(); err != nil {
		s.tasks = s.tasks[:len(s.tasks)-1]
		return ScheduledTask{}, err
	}
	return task, nil
}

// Remove deletes the task with the given id, persisting the change. Returns
// false if no such task exists.
func (s *Scheduler) Remove(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tasks {
		if t.ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			_ = s.saveLocked()
			return true
		}
	}
	return false
}

// List returns a copy of the scheduled tasks, ordered by id.
func (s *Scheduler) List() []ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScheduledTask, len(s.tasks))
	copy(out, s.tasks)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Due returns the tasks whose current period boundary is newer than their last
// run — i.e. they are owed a run as of now. It does not mutate state.
func (s *Scheduler) Due(now time.Time) []ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dueLocked(now)
}

func (s *Scheduler) dueLocked(now time.Time) []ScheduledTask {
	var due []ScheduledTask
	for _, t := range s.tasks {
		if currentBoundary(t.Frequency, now).After(t.LastRun) {
			due = append(due, t)
		}
	}
	return due
}

// checkDue returns the tasks owed a run as of now and stamps their LastRun to
// now (collapsing any number of missed boundaries into a single run), then
// persists. The caller is responsible for actually running the returned
// commands.
func (s *Scheduler) checkDue(now time.Time) []ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	due := s.dueLocked(now)
	if len(due) == 0 {
		return nil
	}
	dueIDs := make(map[int]bool, len(due))
	for _, t := range due {
		dueIDs[t.ID] = true
	}
	for i := range s.tasks {
		if dueIDs[s.tasks[i].ID] {
			s.tasks[i].LastRun = now.UTC()
		}
	}
	_ = s.saveLocked()
	return due
}

// startLoop runs the scheduler in a background goroutine until ctx is
// cancelled. It performs an immediate catch-up pass (logging any backfill),
// then fires due tasks at each tick. run executes one task; it is invoked under
// execMu so scheduled commands never interleave with foreground REPL commands.
// nowFn supplies the current time (injectable for tests).
func (s *Scheduler) startLoop(ctx context.Context, interval time.Duration, execMu *sync.Mutex, run func(ScheduledTask), nowFn func() time.Time) {
	go func() {
		s.fire(execMu, run, nowFn(), true) // startup catch-up
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.fire(execMu, run, nowFn(), false)
			}
		}
	}()
}

// fire runs every task due as of now, collapsing missed boundaries to one run
// each. On the startup pass it announces how many missed tasks it's backfilling.
func (s *Scheduler) fire(execMu *sync.Mutex, run func(ScheduledTask), now time.Time, startup bool) {
	due := s.checkDue(now)
	if len(due) == 0 {
		return
	}
	if startup {
		fmt.Printf("\r⏰ backfilling %d missed scheduled task(s)\n", len(due))
	}
	for _, t := range due {
		execMu.Lock()
		run(t)
		execMu.Unlock()
	}
}

// nextIDLocked returns one past the current maximum id. Caller holds s.mu.
func (s *Scheduler) nextIDLocked() int {
	maxID := 0
	for _, t := range s.tasks {
		if t.ID > maxID {
			maxID = t.ID
		}
	}
	return maxID + 1
}

// handleScheduleAdd parses `schedule_add <freq> <command...>`, registers the
// task, and (run-immediately) executes the command once via runner.
func handleScheduleAdd(s *Scheduler, runner func(string), now time.Time, args []string) {
	if len(args) < 2 {
		fmt.Println("usage: schedule_add <hourly|daily|weekly> <command...>")
		return
	}
	freq := strings.ToLower(args[0])
	command := strings.Join(args[1:], " ")

	// Don't let the scheduler schedule its own management commands.
	if cmdName := strings.ToLower(args[1]); strings.HasPrefix(cmdName, "schedule_") || cmdName == "view_scheduled" {
		fmt.Printf("refusing to schedule the %q builtin\n", args[1])
		return
	}

	task, err := s.Add(freq, command, now)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("scheduled #%d (%s): %s\n", task.ID, task.Frequency, task.Command)
	fmt.Printf("  next due %s; running now…\n", nextBoundary(task.Frequency, now).Format("2006-01-02 15:04 UTC"))
	runner(command)
}

// handleScheduleRemove parses `schedule_remove <id>`.
func handleScheduleRemove(s *Scheduler, args []string) {
	if len(args) < 1 {
		fmt.Println("usage: schedule_remove <id>")
		return
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Printf("invalid id %q\n", args[0])
		return
	}
	if s.Remove(id) {
		fmt.Printf("removed scheduled task #%d\n", id)
	} else {
		fmt.Printf("no scheduled task #%d\n", id)
	}
}

// handleViewScheduled prints the scheduled tasks as a table.
func handleViewScheduled(s *Scheduler, now time.Time) {
	tasks := s.List()
	if len(tasks) == 0 {
		fmt.Println("  (no scheduled tasks)")
		return
	}
	fmt.Printf("  %-3s | %-7s | %-16s | %-16s | %s\n", "ID", "Freq", "Next Due (UTC)", "Last Run (UTC)", "Command")
	fmt.Printf("  %s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", 3), strings.Repeat("-", 7), strings.Repeat("-", 16),
		strings.Repeat("-", 16), strings.Repeat("-", 20))
	for _, t := range tasks {
		last := "never"
		if !t.LastRun.IsZero() {
			last = t.LastRun.UTC().Format("01-02 15:04")
		}
		next := nextBoundary(t.Frequency, now).Format("01-02 15:04")
		fmt.Printf("  %-3d | %-7s | %-16s | %-16s | %s\n", t.ID, t.Frequency, next, last, t.Command)
	}
}

// saveLocked writes the task list to disk atomically. Caller holds s.mu.
func (s *Scheduler) saveLocked() error {
	data, err := json.MarshalIndent(s.tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("scheduler: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("scheduler: create dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("scheduler: write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("scheduler: replace: %w", err)
	}
	return nil
}
