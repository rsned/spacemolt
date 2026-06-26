package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ScheduledTask is a recurring command the user has registered. The command is
// a full play_as command line (e.g. "view_storage --station_id frontier") run
// through executeLogicalCommand at the task's frequency.
type ScheduledTask struct {
	ID        int       `json:"id"`
	Frequency string    `json:"frequency"` // half_hourly | hourly | daily | weekly
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"created_at"`
	LastRun   time.Time `json:"last_run,omitzero"` // UTC; zero = never run
}

// ValidFrequencies is the closed set of supported frequencies.
var ValidFrequencies = map[string]bool{"half_hourly": true, "hourly": true, "daily": true, "weekly": true}

// CurrentBoundary returns the most recent wall-clock boundary (UTC) for freq at
// or before now: the most recent half hour (:00 or :30), the top of the hour,
// midnight, or the most recent Sunday midnight. Returns the zero time for an
// unknown frequency.
func CurrentBoundary(freq string, now time.Time) time.Time {
	now = now.UTC()
	switch freq {
	case "half_hourly":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), (now.Minute()/30)*30, 0, 0, time.UTC)
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

// NextBoundary returns the next time freq will fire strictly after now, used
// for the "next due" column in view_scheduled.
func NextBoundary(freq string, now time.Time) time.Time {
	cur := CurrentBoundary(freq, now)
	switch freq {
	case "half_hourly":
		return cur.Add(30 * time.Minute)
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
	if !ValidFrequencies[freq] {
		return ScheduledTask{}, fmt.Errorf("unknown frequency %q (want half_hourly, hourly, daily, or weekly)", freq)
	}
	if command == "" {
		return ScheduledTask{}, fmt.Errorf("empty command")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.Frequency == freq && t.Command == command {
			return ScheduledTask{}, fmt.Errorf("duplicate of scheduled task #%d (%s): %s", t.ID, freq, command)
		}
	}
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
		if CurrentBoundary(t.Frequency, now).After(t.LastRun) {
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

// StartLoop runs the scheduler in a background goroutine until ctx is
// cancelled. It performs an immediate catch-up pass (logging any backfill),
// then fires due tasks at each tick. run executes one task; it is invoked under
// execMu so scheduled commands never interleave with foreground REPL commands.
// nowFn supplies the current time (injectable for tests).
func (s *Scheduler) StartLoop(ctx context.Context, interval time.Duration, execMu *sync.Mutex, run func(ScheduledTask), nowFn func() time.Time) {
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
