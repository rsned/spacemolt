package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/outcomes"
)

// failingRunner fails any command containing one of its substrings.
type failingRunner struct {
	recordRunner
	fail map[string]error
}

func (r *failingRunner) Run(ctx context.Context, tokens []string) error {
	_ = r.recordRunner.Run(ctx, tokens)
	line := joinTokens(tokens)
	for sub, err := range r.fail {
		if strings.Contains(line, sub) {
			return err
		}
	}
	return nil
}

// runStandingBriefly runs RunStanding for a few passes and stops it.
func runStandingBriefly(t *testing.T, role Role, deps StandingDeps) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = RunStanding(ctx, role, deps); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunStanding did not return after cancel")
	}
}

func idleScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "idle.smolt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStandingRecordsScheduledOutcome(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 11, 1, 10, 30, 0, 0, time.UTC)
	sched, err := LoadScheduler(filepath.Join(dir, "sched.json"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := sched.Add("hourly", "craft grow_sunspindle 1", t0)
	if err != nil {
		t.Fatal(err)
	}
	r := &failingRunner{fail: map[string]error{"grow_sunspindle": errors.New("cannot_craft: Not enough materials")}}
	var mu sync.Mutex
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false }, Out: io.Discard,
		NowFn: func() time.Time { return t0.Add(2 * time.Hour) }, IdleInterval: time.Millisecond,
		ScheduleInterval: time.Millisecond, AgentID: "test", Outcomes: outcomes.New(dir),
	}
	runStandingBriefly(t, Role{}, deps)

	got, err := outcomes.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	o, ok := got[fmt.Sprintf("task:%d", task.ID)]
	if !ok || o.Status != outcomes.StatusFailed || !strings.Contains(o.Reason, "cannot_craft") {
		t.Errorf("outcome = %+v (present %v), want failed with cannot_craft; all: %v", o, ok, got)
	}
}

func TestStandingRecordsIdleLineOutcome(t *testing.T) {
	dir := t.TempDir()
	sched, _ := LoadScheduler(filepath.Join(dir, "sched.json"))
	var mu sync.Mutex
	deps := StandingDeps{
		Runner: &recordRunner{}, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false }, Out: io.Discard,
		NowFn: time.Now, IdleInterval: time.Millisecond, AgentID: "test", Outcomes: outcomes.New(dir),
	}
	runStandingBriefly(t, Role{Idle: idleScript(t, "ensure_home\n")}, deps)

	got, err := outcomes.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if o := got["idle:ensure_home"]; o.Status != outcomes.StatusOK {
		t.Errorf("idle:ensure_home = %+v, want ok; all: %v", o, got)
	}
}

func TestStandingNilOutcomesIsNoop(t *testing.T) {
	dir := t.TempDir()
	sched, _ := LoadScheduler(filepath.Join(dir, "sched.json"))
	var mu sync.Mutex
	deps := StandingDeps{
		Runner: &recordRunner{}, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false }, Out: io.Discard,
		NowFn: time.Now, IdleInterval: time.Millisecond, AgentID: "test",
	}
	runStandingBriefly(t, Role{Idle: idleScript(t, "ensure_home\n")}, deps)

	if _, err := os.Stat(filepath.Join(dir, outcomes.FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("outcome file written with nil Outcomes (stat err %v)", err)
	}
}

// shutdownRunner blocks every command until ctx is cancelled.
type shutdownRunner struct{}

func (shutdownRunner) Run(ctx context.Context, _ []string) error {
	<-ctx.Done()
	return ctx.Err()
}

// A worker stopped mid-command (a rollout, a restart) did not fail the work:
// no "context canceled" outcome is recorded.
func TestStandingSkipsRecordOnShutdown(t *testing.T) {
	dir := t.TempDir()
	sched, _ := LoadScheduler(filepath.Join(dir, "sched.json"))
	var mu sync.Mutex
	deps := StandingDeps{
		Runner: shutdownRunner{}, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false }, Out: io.Discard,
		NowFn: time.Now, IdleInterval: time.Millisecond, AgentID: "test", Outcomes: outcomes.New(dir),
	}
	runStandingBriefly(t, Role{Idle: idleScript(t, "ensure_home\n")}, deps)

	got, _ := outcomes.Load(dir)
	for k, o := range got {
		if o.Status == outcomes.StatusFailed {
			t.Errorf("%s recorded as failed on shutdown: %+v", k, o)
		}
	}
}
