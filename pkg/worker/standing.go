package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// AssignedTask is a one-shot task handed to the worker by the overmind. It is a
// local mirror of control.Assign so pkg/worker does not import pkg/overmind/control.
type AssignedTask struct {
	ID     string
	Script string
	Params map[string]string
}

// StandingDeps are the collaborators RunStanding needs. All are injectable so
// the driver is testable without a game connection.
type StandingDeps struct {
	Runner    CommandRunner                       // executes a tokenized command (WorkerDispatch)
	Scheduler *Scheduler                          // per-agent recurring tasks
	Client    interface{ GetState() *game.State } // for token resolution
	ExecMu    *sync.Mutex                         // serializes scheduled + idle work on the one game conn
	Paused     func() bool                         // gate from the control reader's paused flag
	Draining   func() bool                         // second gate (drain): finish current pass, take no new work
	SetDrained func(bool)                          // publishes whether the worker is held idle due to drain
	Out        io.Writer                           // worker stdout / logs
	NowFn     func() time.Time                    // injectable clock

	IdleInterval     time.Duration // between idle passes (0 → game.SleepShort)
	ScheduleInterval time.Duration // scheduler tick (0 → game.SleepLong)
	AgentID          string        // for script resolution search paths

	// Task hooks (nil when the worker has no control channel). NextTask returns
	// and consumes the pending assigned task (or nil); OnTaskResult reports a
	// finished task's id and error (nil = success).
	NextTask     func() *AssignedTask
	OnTaskResult func(taskID string, err error)

	// PayDebts, when set, runs once per non-drained idle pass under ExecMu to
	// pay any outstanding rescue-fee debt. nil for workers with no fee wiring.
	PayDebts func(context.Context)

	// Handoffs, when set, runs once per non-drained idle pass under ExecMu to
	// fulfill pending stock-handoff records for this agent. nil when the worker
	// has no handoff queue configured.
	Handoffs func(context.Context)
}

// RunStanding drives a worker's default standing behavior until ctx is
// cancelled: it registers the role's scheduled commands with the Scheduler and
// runs the role's idle script in a loop, both serialized on deps.ExecMu so they
// never interleave on the single game connection. It returns when ctx is
// cancelled, after any in-flight idle pass completes.
func RunStanding(ctx context.Context, role Role, deps StandingDeps) error {
	if deps.Out == nil {
		deps.Out = io.Discard
	}
	if deps.NowFn == nil {
		deps.NowFn = func() time.Time { return time.Now().UTC() }
	}
	if deps.IdleInterval == 0 {
		deps.IdleInterval = game.SleepShort
	}
	if deps.ScheduleInterval == 0 {
		deps.ScheduleInterval = game.SleepLong
	}
	if deps.SetDrained == nil {
		deps.SetDrained = func(bool) {}
	}

	// Register schedule entries (idempotent: skip a command already registered,
	// so a restart does not duplicate it).
	if deps.Scheduler != nil {
		existing := make(map[string]bool)
		for _, t := range deps.Scheduler.List() {
			existing[t.Frequency+"|"+t.Command] = true
		}
		for _, se := range role.Schedule {
			if existing[se.Every+"|"+se.Command] {
				continue
			}
			if _, err := deps.Scheduler.Add(se.Every, se.Command, deps.NowFn()); err != nil {
				fmt.Fprintf(deps.Out, "standing: schedule add %q failed: %v\n", se.Command, err) //nolint:errcheck
			}
		}
		// run executes one scheduled command line under ExecMu.
		run := func(t ScheduledTask) {
			fmt.Fprintf(deps.Out, "⏰ [scheduled %s] %s\n", t.Frequency, t.Command) //nolint:errcheck
			_ = deps.runLine(ctx, t.Command)
		}
		deps.Scheduler.StartLoop(ctx, deps.ScheduleInterval, deps.ExecMu, run, deps.NowFn)
	}

	// Resolve the idle script once into command lines.
	idleCmds := deps.resolveIdle(role)

	// Idle loop.
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		paused := deps.Paused != nil && deps.Paused()
		draining := deps.Draining != nil && deps.Draining()
		if paused || draining {
			deps.SetDrained(draining) // drained only when held *because* of drain
			if sleepCtx(ctx, deps.IdleInterval) {
				return nil
			}
			continue
		}
		deps.SetDrained(false)
		deps.ExecMu.Lock()
		if deps.PayDebts != nil {
			deps.PayDebts(ctx)
		}
		if deps.Handoffs != nil {
			deps.Handoffs(ctx)
		}
		if task := deps.nextTask(); task != nil {
			deps.runTask(ctx, task)
		} else {
			for _, line := range idleCmds {
				select {
				case <-ctx.Done():
					deps.ExecMu.Unlock()
					return nil
				default:
				}
				_ = deps.runLine(ctx, line)
			}
		}
		deps.ExecMu.Unlock()
		if sleepCtx(ctx, deps.IdleInterval) {
			return nil
		}
	}
}

// resolveIdle loads the role's idle script into command lines, falling back to a
// single get_status when the script is absent (keeps an unconfigured worker
// alive and tests hermetic).
func (deps StandingDeps) resolveIdle(role Role) []string {
	if role.Idle == "" {
		return []string{"get_status"}
	}
	path, ok := ResolveScriptArg(role.Idle, deps.AgentID)
	if !ok {
		fmt.Fprintf(deps.Out, "standing: idle script %q not found; using get_status\n", role.Idle) //nolint:errcheck
		return []string{"get_status"}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(deps.Out, "standing: read idle script %q: %v; using get_status\n", role.Idle, err) //nolint:errcheck
		return []string{"get_status"}
	}
	cmds, err := SplitScriptCommands(string(content))
	if err != nil {
		fmt.Fprintf(deps.Out, "standing: parse idle script %q: %v; using get_status\n", role.Idle, err) //nolint:errcheck
		return []string{"get_status"}
	}
	return cmds
}

// runLine resolves tokens against live state and executes a single command line.
// A loop header is expanded via ExecuteLoop; a plain line goes straight to the
// runner. Errors are logged (idle work is best-effort, force-like) and returned
// so task runners can detect failures.
func (deps StandingDeps) runLine(ctx context.Context, line string) error {
	stmts, err := ParseStatements(line)
	if err != nil {
		fmt.Fprintf(deps.Out, "standing: parse %q: %v\n", line, err) //nolint:errcheck
		return err
	}
	var lastErr error
	for _, st := range stmts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(st.Tokens) > 0 && strings.EqualFold(st.Tokens[0], "loop") {
			count, force, body, isBlock, perr := ParseLoopHeader(st)
			if perr != nil {
				fmt.Fprintf(deps.Out, "standing: %v\n", perr) //nolint:errcheck
				lastErr = perr
				continue
			}
			var inner []Statement
			if isBlock {
				inner, err = ParseStatements(body)
			} else {
				inner = []Statement{{Raw: body, Tokens: SplitArgs(body)}}
			}
			if err != nil {
				fmt.Fprintf(deps.Out, "standing: %v\n", err) //nolint:errcheck
				lastErr = err
				continue
			}
			rs := func(tokens []string) error { return deps.dispatch(ctx, tokens) }
			if lerr := ExecuteLoop(ctx, deps.Out, count, force, inner, 0, rs); lerr != nil {
				fmt.Fprintf(deps.Out, "standing: loop: %v\n", lerr) //nolint:errcheck
				lastErr = lerr
			}
			continue
		}
		if derr := deps.dispatch(ctx, st.Tokens); derr != nil {
			fmt.Fprintf(deps.Out, "standing: %q: %v\n", st.Raw, derr) //nolint:errcheck
			lastErr = derr
		}
	}
	return lastErr
}

// dispatch resolves tokens against live state, then runs them.
func (deps StandingDeps) dispatch(ctx context.Context, tokens []string) error {
	var st *game.State
	if deps.Client != nil {
		st = deps.Client.GetState()
	}
	resolved, err := ResolveTokens(tokens, st)
	if err != nil {
		return err
	}
	return deps.Runner.Run(ctx, resolved)
}

// nextTask returns the pending assigned task, or nil when there is no task hook
// or nothing pending.
func (deps StandingDeps) nextTask() *AssignedTask {
	if deps.NextTask == nil {
		return nil
	}
	return deps.NextTask()
}

// runTask resolves the task's script, substitutes its params, runs the lines
// once (stopping at the first error), and reports the result. Must be called
// with deps.ExecMu held.
func (deps StandingDeps) runTask(ctx context.Context, task *AssignedTask) {
	report := func(err error) {
		if deps.OnTaskResult != nil {
			deps.OnTaskResult(task.ID, err)
		}
	}
	path, ok := ResolveScriptArg(task.Script, deps.AgentID)
	if !ok {
		report(fmt.Errorf("task %q: script %q not found", task.ID, task.Script))
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		report(fmt.Errorf("task %q: read script: %w", task.ID, err))
		return
	}
	lines, err := SplitScriptCommands(string(content))
	if err != nil {
		report(fmt.Errorf("task %q: parse script: %w", task.ID, err))
		return
	}
	lines = SubstituteParams(lines, task.Params)
	fmt.Fprintf(deps.Out, "▶ task %s: running %s (%d lines)\n", task.ID, task.Script, len(lines)) //nolint:errcheck
	for _, line := range lines {
		if ctx.Err() != nil {
			report(ctx.Err())
			return
		}
		if e := deps.runLine(ctx, line); e != nil {
			report(fmt.Errorf("task %q: %q: %w", task.ID, line, e))
			return
		}
	}
	report(nil)
}

// sleepCtx sleeps d or returns true if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) (cancelled bool) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}
