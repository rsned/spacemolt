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
	role := Role{Idle: "get_status"}

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
