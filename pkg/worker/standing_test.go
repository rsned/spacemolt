package worker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// recordRunner records every command line it runs.
type recordRunner struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordRunner) Run(_ context.Context, tokens []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, joinTokens(tokens))
	return nil
}
func (r *recordRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// ranCommandContaining returns true if any recorded command contains all of the
// given substrings (case-insensitive).
func (r *recordRunner) ranCommandContaining(substrs ...string) bool {
	for _, line := range r.snapshot() {
		lower := strings.ToLower(line)
		all := true
		for _, s := range substrs {
			if !strings.Contains(lower, strings.ToLower(s)) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func joinTokens(t []string) string {
	s := ""
	for i, x := range t {
		if i > 0 {
			s += " "
		}
		s += x
	}
	return s
}

type stateClient struct{ st *game.State }

func (s stateClient) GetState() *game.State { return s.st }

func TestRunStandingRunsIdleLoopThenStopsOnCancel(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")

	ctx, cancel := context.WithCancel(context.Background())
	// idle script with no tokens so resolution is a no-op.
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner:       r,
		Scheduler:    sched,
		Client:       stateClient{st: &game.State{}},
		ExecMu:       &mu,
		Paused:       func() bool { return false },
		Out:          io.Discard,
		NowFn:        func() time.Time { return time.Unix(0, 0).UTC() },
		IdleInterval: time.Millisecond,
		AgentID:      "test",
	}
	// Override script resolution by writing the script under a temp scripts dir
	// the resolver can find; simplest: inject commands directly (see Step 4 note).
	done := make(chan struct{})
	go func() { _ = RunStanding(ctx, role, deps); close(done) }()
	// Let a few idle passes run.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunStanding did not return after cancel")
	}
	if len(r.snapshot()) == 0 {
		t.Fatal("idle loop never ran a command")
	}
}

func TestRunStandingPausedDoesNotRunIdle(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var paused atomic.Bool
	paused.Store(true)
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: paused.Load, Out: io.Discard,
		NowFn: func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("paused worker ran %d commands", n)
	}
}

func TestRunStandingRunsAssignedTaskThenResumesIdle(t *testing.T) {
	// Write a small task script to the per-agent search path so ResolveScriptArg
	// can find it. t.Chdir switches the working directory for this test (restored
	// automatically), making relative path lookups land in the temp tree.
	tmp := t.TempDir()
	t.Chdir(tmp)
	scriptDir := filepath.Join(tmp, "data", "agents", "miner-1", "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptContent := "autopilot $TARGET_SYSTEM$\nmine\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "test_task.smolt"), []byte(scriptContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// The idle script has to exist on disk. resolveIdle yields NO commands for a
	// role whose script is missing (a silent role is genuinely silent now, with
	// liveness left to the keepalive), so relying on the old get_status fallback
	// would make the "idle resumes" assertion below vacuous.
	idleDir := filepath.Join(tmp, "data", "scripts")
	if err := os.MkdirAll(idleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(idleDir, "idle_probe.smolt"), []byte("get_status\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &recordRunner{}
	var execMu sync.Mutex
	delivered := false
	var gotTaskID string
	var gotErr error

	task := &AssignedTask{ID: "t1", Script: "test_task", Params: map[string]string{"TARGET_SYSTEM": "bunda"}}

	deps := StandingDeps{
		Runner:           rec,
		Client:           stateClient{st: &game.State{}},
		ExecMu:           &execMu,
		Out:              io.Discard,
		AgentID:          "miner-1",
		IdleInterval:     time.Millisecond,
		ScheduleInterval: time.Hour,
		NextTask: func() *AssignedTask {
			if delivered {
				return nil
			}
			delivered = true
			return task
		},
		OnTaskResult: func(id string, err error) { gotTaskID, gotErr = id, err },
	}

	// Role with a distinct idle command so we can tell idle from task work.
	role := Role{Idle: "idle_probe"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// let a couple of idle passes happen after the task, then stop
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_ = RunStanding(ctx, role, deps)

	if gotTaskID != "t1" {
		t.Fatalf("OnTaskResult task id = %q, want t1", gotTaskID)
	}
	if gotErr != nil {
		t.Fatalf("OnTaskResult err = %v, want nil", gotErr)
	}
	// The test_task script issues an autopilot to the substituted target.
	if !rec.ranCommandContaining("autopilot", "bunda") {
		t.Fatalf("expected task autopilot to bunda; recorded=%v", rec.snapshot())
	}
	// After the task, idle (get_status) must have run on a later pass.
	if !rec.ranCommandContaining("get_status") {
		t.Fatalf("expected idle get_status to resume; recorded=%v", rec.snapshot())
	}
}

func TestRunStandingDrainingHoldsAndReportsDrained(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var draining atomic.Bool
	draining.Store(true)
	var drained atomic.Bool
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Draining: draining.Load, SetDrained: drained.Store, Out: io.Discard,
		NowFn: func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("draining worker ran %d commands, want 0", n)
	}
	if !drained.Load() {
		t.Fatal("expected SetDrained(true) while held by drain")
	}
}

func TestRunStandingResumeAfterDrainRunsIdleAndClearsDrained(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var draining atomic.Bool
	draining.Store(true)
	var drained atomic.Bool
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Draining: draining.Load, SetDrained: drained.Store, Out: io.Discard,
		NowFn: func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	draining.Store(false) // resume
	time.Sleep(20 * time.Millisecond)
	if len(r.snapshot()) == 0 {
		t.Fatal("resumed worker never ran an idle command")
	}
	if drained.Load() {
		t.Fatal("expected drained cleared once passes resumed")
	}
}

// TestRoleSeedingSkipsACommandAlreadyCoveredByAFinerSchedule is the 2026-08-13
// regression. Seeding runs on EVERY worker start, and its idempotence test used
// to key on frequency|command — so a role's `hourly update_market` did not match
// an operator's hand-added `ten_minutely update_market` and was appended beside
// it, again after every restart.
//
// The two are not spread across the hour: boundaries are wall-clock aligned, so
// :00 is both an hourly and a ten-minutely mark and one scheduler pass fires
// both back to back. That is a duplicate market capture per agent per hour,
// which is how 43 of 45 marketbots ended up double-capturing.
func TestRoleSeedingSkipsACommandAlreadyCoveredByAFinerSchedule(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	sched, err := LoadScheduler(filepath.Join(t.TempDir(), "sched.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The operator's finer capture, already on the books.
	if _, err := sched.Add("ten_minutely", "update_market", now); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	role := Role{
		Idle: "noop_idle",
		Schedule: []ScheduleEntry{
			{Every: "hourly", Command: "update_market"}, // covered — must not be added
			{Every: "hourly", Command: "capture_profile"},
		},
	}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false }, Out: io.Discard,
		NowFn: func() time.Time { return now }, IdleInterval: time.Millisecond,
		AgentID: "test",
	}
	done := make(chan struct{})
	go func() { _ = RunStanding(ctx, role, deps); close(done) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunStanding did not return after cancel")
	}

	var freqs []string
	for _, task := range sched.List() {
		if task.Command == "update_market" {
			freqs = append(freqs, task.Frequency)
		}
	}
	if len(freqs) != 1 {
		t.Errorf("update_market scheduled %d times (%v); the hourly entry is covered by ten_minutely and buys nothing", len(freqs), freqs)
	}
	if len(freqs) > 0 && freqs[0] != "ten_minutely" {
		t.Errorf("kept the %s entry; the finer ten_minutely schedule is the one that must survive", freqs[0])
	}
	// An uncovered role entry must still be seeded — the guard is not a mute.
	var sawProfile bool
	for _, task := range sched.List() {
		if task.Command == "capture_profile" {
			sawProfile = true
		}
	}
	if !sawProfile {
		t.Error("capture_profile was not scheduled; the coverage guard dropped an entry nothing covered")
	}
}

// The park has to be honoured at the SAME boundary drain uses: the top of an
// idle pass. Anywhere else and a hauler would be cut off mid-run.
func TestRunStandingQuiescedHoldsAndReportsQuiesced(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var gotReason atomic.Value
	gotReason.Store("")
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Quiesced:    func() (bool, string) { return true, "wildlife testing" },
		SetQuiesced: func(_ bool, reason string) { gotReason.Store(reason) },
		Out:         io.Discard,
		NowFn:       func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("quiesced worker ran %d commands, want 0", n)
	}
	if got := gotReason.Load().(string); got != "wildlife testing" {
		t.Errorf("published reason = %q, want %q", got, "wildlife testing")
	}
}

// Clearing the flag puts it straight back to work — an operator who parked an
// agent by mistake should not have to restart it.
func TestRunStandingResumeAfterQuiesceClearsFlag(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var parked atomic.Bool
	parked.Store(true)
	var published atomic.Bool
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Quiesced:    func() (bool, string) { return parked.Load(), "" },
		SetQuiesced: func(q bool, _ string) { published.Store(q) },
		Out:         io.Discard,
		NowFn:       func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	parked.Store(false)
	time.Sleep(20 * time.Millisecond)
	if len(r.snapshot()) == 0 {
		t.Fatal("un-parked worker never ran an idle command")
	}
	if published.Load() {
		t.Fatal("expected quiesced cleared once passes resumed")
	}
}

// A park landing mid-pass must not truncate that pass. This is the whole point
// of the design: the gate is at the boundary, so the run in flight completes.
func TestRunStandingQuiesceDoesNotInterruptAPassInFlight(t *testing.T) {
	var mu sync.Mutex
	var parked atomic.Bool
	// A runner that trips the park as soon as the pass's first command runs.
	r := &recordRunner{}
	gate := &quiesceTripRunner{inner: r, onFirst: func() { parked.Store(true) }}
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Three commands in the pass; all three must run even though the park is
	// set while the first is executing. The script has to exist on disk --
	// resolveIdle falls back to a single get_status when it does not, which
	// would make this test vacuous.
	t.Chdir(writeIdleScript(t, "three_line_idle", "get_status\nget_ship\nget_cargo\n"))
	role := Role{Idle: "three_line_idle"}
	deps := StandingDeps{
		Runner: gate, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Quiesced:    func() (bool, string) { return parked.Load(), "" },
		SetQuiesced: func(bool, string) {},
		Out:         io.Discard,
		NowFn:       func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(40 * time.Millisecond)
	got := r.snapshot()
	if len(got) != 3 {
		t.Fatalf("pass ran %d commands (%v), want all 3 — the park must not truncate a pass in flight", len(got), got)
	}
}

// quiesceTripRunner fires onFirst once, when the first command of a pass runs.
type quiesceTripRunner struct {
	inner   *recordRunner
	once    sync.Once
	onFirst func()
}

func (q *quiesceTripRunner) Run(ctx context.Context, tokens []string) error {
	q.once.Do(q.onFirst)
	return q.inner.Run(ctx, tokens)
}

// writeIdleScript drops an idle script into a temp dir laid out the way
// ResolveScriptArg expects (data/scripts/<name>.smolt) and returns that dir for
// t.Chdir.
func writeIdleScript(t *testing.T, name, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "data", "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".smolt"), []byte(body), 0o644); err != nil {
		t.Fatalf("write idle script: %v", err)
	}
	return root
}
