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
	Frequency string    `json:"frequency"` // ten_minutely | quarter_hourly | half_hourly | hourly | daily | weekly
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"created_at"`
	LastRun   time.Time `json:"last_run,omitzero"` // UTC; zero = never run
}

// ValidFrequencies is the closed set of supported frequencies.
// frequencyAliases maps the short forms operators actually type to the
// canonical frequency names. Canonical names pass through NormalizeFrequency
// unchanged, so callers can accept either without knowing which they got.
var frequencyAliases = map[string]string{
	"10m": "ten_minutely", "10min": "ten_minutely",
	"15m": "quarter_hourly", "15min": "quarter_hourly",
	"30m": "half_hourly", "30min": "half_hourly", "halfhour": "half_hourly",
	"1h": "hourly", "hour": "hourly",
	"12h": "twice_daily",
	"1d":  "daily", "24h": "daily", "day": "daily",
	"1w": "weekly", "week": "weekly",
}

// NormalizeFrequency lower-cases and trims a frequency and resolves the short
// aliases ("10m", "30m", "1d", ...) to canonical names. Unknown input is
// returned trimmed and lower-cased so the caller's validation names it.
func NormalizeFrequency(freq string) string {
	freq = strings.ToLower(strings.TrimSpace(freq))
	if canon, ok := frequencyAliases[freq]; ok {
		return canon
	}
	return freq
}

var ValidFrequencies = map[string]bool{"ten_minutely": true, "quarter_hourly": true, "half_hourly": true, "hourly": true, "twice_daily": true, "daily": true, "weekly": true}

// frequencyPeriod is each frequency's boundary spacing. Every frequency is
// anchored to an instant that is itself a multiple of every shorter period
// (the sub-hour marks to the top of the hour, daily to midnight, weekly to
// Sunday midnight), so "does A fire at every boundary B fires at" reduces to
// "does A's period divide B's". See CurrentBoundary for the anchors.
var frequencyPeriod = map[string]time.Duration{
	"ten_minutely":   10 * time.Minute,
	"quarter_hourly": 15 * time.Minute,
	"half_hourly":    30 * time.Minute,
	"hourly":         time.Hour,
	"twice_daily":    12 * time.Hour,
	"daily":          24 * time.Hour,
	"weekly":         7 * 24 * time.Hour,
}

// Covers reports whether a task at frequency have already fires at every
// boundary want fires at — so scheduling want alongside have would buy nothing
// but a second identical run in the same scheduler pass.
//
// This is NOT a coarse-to-fine ordering. ten_minutely and quarter_hourly are
// incomparable: :10 is not a quarter mark and :15 is not a ten mark, so neither
// covers the other and both earn their place. An unknown frequency covers
// nothing and is covered by nothing.
func Covers(have, want string) bool {
	h, okHave := frequencyPeriod[have]
	w, okWant := frequencyPeriod[want]
	if !okHave || !okWant {
		return false
	}
	return w%h == 0
}

// maxBoundaryPhase caps how far an agent's boundary grid is shifted from the
// shared one. Five minutes spreads a 160-worker fleet to well under one firing
// per second, which is the whole point: on 2026-08-23 the unphased grid put
// 110-160 commands into a single second and tripped the shared IP rate limiter.
// It is deliberately below every frequency's period so a phase never reorders
// boundaries, and small enough that "daily" still means the expected time of day.
const maxBoundaryPhase = 5 * time.Minute

// maxWidePhase caps the spread for commands that do not care when in the period
// they run. It is an hour so an "hourly" capture can land anywhere in its hour
// and a "daily" one still lands within an hour of its expected time of day --
// wide enough to flatten the fleet's baseline, narrow enough that a daily job
// does not wander across the day.
const maxWidePhase = time.Hour

// widePhaseCommands are the bookkeeping captures: append-only syncs whose exact
// firing instant carries no information. Spreading them across the period costs
// nothing and takes them out of the window reserved for time-sensitive work.
//
// update_market is deliberately ABSENT. A market read is a measurement, and
// keeping an agent's reads on its own tight phase is what makes successive
// snapshots comparable -- that burst is wanted.
var widePhaseCommands = map[string]bool{
	"capture_action_log":       true,
	"capture_citizenship":      true,
	"capture_faction":          true,
	"capture_fuel":             true,
	"capture_profile":          true,
	"capture_storage":          true,
	"capture_tax":              true,
	"capture_wildlife_attacks": true,
}

// FrequencyPeriod is how long one full period of freq lasts, or 0 when freq is
// unknown. It reads the same table Covers uses, so the two can never disagree.
func FrequencyPeriod(freq string) time.Duration {
	return frequencyPeriod[freq]
}

// phaseCapFor is how far command may be shifted on freq's grid. Time-sensitive
// commands keep the original tight window; bookkeeping captures get half the
// period (bounded by maxWidePhase), which stays strictly under the period and
// so preserves the no-reordering invariant.
func phaseCapFor(freq, command string) time.Duration {
	if !widePhaseCommands[command] {
		return maxBoundaryPhase
	}
	period := FrequencyPeriod(freq)
	if period == 0 {
		return maxBoundaryPhase
	}
	return max(min(period/2, maxWidePhase), maxBoundaryPhase)
}

// BoundaryPhaseFor is BoundaryPhase with a per-command spread.
//
// For a time-sensitive command it returns exactly BoundaryPhase(seed, freq), so
// an agent's market reads all share one phase and stay together. For a
// bookkeeping capture it hashes the command in as well and spreads over a wider
// cap, so those neither collide with the tight window nor with each other.
func BoundaryPhaseFor(seed, freq, command string) time.Duration {
	if seed == "" {
		return 0
	}
	limit := phaseCapFor(freq, command)
	if limit == maxBoundaryPhase {
		return BoundaryPhase(seed, freq)
	}
	return time.Duration(fnv1a(seed+"\x00"+freq+"\x00"+command) % uint64(limit))
}

// BoundaryPhase returns the fixed offset this seed applies to freq's boundary
// grid, in [0, maxBoundaryPhase). It is a pure function of the seed and the
// frequency, so an agent keeps the same phase across restarts (no drift, no
// re-herding) while two agents almost never share one. Including freq keeps an
// agent's own hourly and ten_minutely tasks off the same instant.
func BoundaryPhase(seed, freq string) time.Duration {
	if seed == "" {
		return 0
	}
	return time.Duration(fnv1a(seed+"\x00"+freq) % uint64(maxBoundaryPhase))
}

// fnv1a is stable across processes and architectures, unlike maphash, which is
// randomly seeded per process and would re-herd the fleet on every restart.
func fnv1a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for _, b := range []byte(s) {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}

// SetPhaseSeed puts this scheduler on its own phase of the boundary grid,
// keyed by the seed (the agent id). Callers that want lock-step boundaries --
// the play_as REPL, and tests asserting the raw grid -- simply never call it.
// Deliberately explicit: deriving the seed from the schedule file's path would
// hand every t.TempDir() test a different random phase.
func (s *Scheduler) SetPhaseSeed(seed string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.phaseSeed = seed
}

// phasedBoundary is CurrentBoundary shifted onto this scheduler's own grid.
// Shifting the clock back by the phase, snapping, then shifting forward keeps
// the period exactly intact -- it only moves where the period starts.
func (s *Scheduler) phasedBoundary(freq, command string, now time.Time) time.Time {
	phase := BoundaryPhaseFor(s.phaseSeed, freq, command)
	if phase == 0 {
		return CurrentBoundary(freq, now)
	}
	return CurrentBoundary(freq, now.Add(-phase)).Add(phase)
}

// CurrentBoundary returns the most recent wall-clock boundary (UTC) for freq at
// or before now: the most recent ten-minute mark (:00, :10, …), quarter hour,
// half hour (:00 or :30), the top of the hour, midnight, or the most recent
// Sunday midnight. Returns the zero time for an unknown frequency.
func CurrentBoundary(freq string, now time.Time) time.Time {
	now = now.UTC()
	switch freq {
	case "ten_minutely":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), (now.Minute()/10)*10, 0, 0, time.UTC)
	case "quarter_hourly":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), (now.Minute()/15)*15, 0, 0, time.UTC)
	case "half_hourly":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), (now.Minute()/30)*30, 0, 0, time.UTC)
	case "hourly":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.UTC)
	case "twice_daily":
		return time.Date(now.Year(), now.Month(), now.Day(), (now.Hour()/12)*12, 0, 0, 0, time.UTC)
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
	case "ten_minutely":
		return cur.Add(10 * time.Minute)
	case "quarter_hourly":
		return cur.Add(15 * time.Minute)
	case "half_hourly":
		return cur.Add(30 * time.Minute)
	case "hourly":
		return cur.Add(time.Hour)
	case "twice_daily":
		return cur.Add(12 * time.Hour)
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
	path string
	mu   sync.Mutex
	// phaseSeed staggers this agent's boundaries against the shared UTC grid.
	// Empty means no phase (the old lock-step behaviour), which is what the
	// REPL and tests want; LoadScheduler derives it from the agent directory.
	phaseSeed string
	tasks     []ScheduledTask
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
	freq = NormalizeFrequency(freq)
	command = strings.TrimSpace(command)
	if !ValidFrequencies[freq] {
		return ScheduledTask{}, fmt.Errorf("unknown frequency %q (want ten_minutely, quarter_hourly, half_hourly, hourly, twice_daily, daily, or weekly; short forms 10m 15m 30m 1h 12h 1d 1w)", freq)
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

// RetireCovered removes every task that another task of the SAME command
// already covers, and returns what it removed. Keeping the covered one buys
// nothing: its every firing coincides with the finer task's, in the same
// scheduler pass, so it is a duplicate run of an identical command.
//
// This is the other half of the seeding guard. That one refuses to ADD a task
// something already covers; this one retires a task that a NEWLY ADDED finer
// task has just made redundant. Without it, changing a role's cadence silently
// accumulates residue: raising `resident` to ten_minutely leaves every agent's
// old hourly update_market in place, and moving an agent between roles leaves
// the old role's coarser entries behind (miner-2 carried both a daily and an
// hourly kb_update after graduating out of the unlock pool).
//
// Retiring is safe for hand-added tasks too, because coverage is defined per
// command: a coarser entry for the same command has no firing of its own to
// lose. Frequencies that merely look coarser are NOT retired — ten_minutely and
// quarter_hourly do not cover each other, so both survive.
func (s *Scheduler) RetireCovered() []ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	covered := func(t ScheduledTask) bool {
		for _, o := range s.tasks {
			if o.ID == t.ID || o.Command != t.Command || o.Frequency == t.Frequency {
				continue
			}
			if Covers(o.Frequency, t.Frequency) {
				return true
			}
		}
		return false
	}
	var dropped []ScheduledTask
	kept := s.tasks[:0:0]
	for _, t := range s.tasks {
		if covered(t) {
			dropped = append(dropped, t)

			continue
		}
		kept = append(kept, t)
	}
	if len(dropped) == 0 {
		return nil
	}
	s.tasks = kept
	_ = s.saveLocked()

	return dropped
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
		if s.phasedBoundary(t.Frequency, t.Command, now).After(t.LastRun) {
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
