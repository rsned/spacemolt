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
	Runner     CommandRunner                       // executes a tokenized command (WorkerDispatch)
	Scheduler  *Scheduler                          // per-agent recurring tasks
	Client     interface{ GetState() *game.State } // for token resolution
	ExecMu     *sync.Mutex                         // serializes scheduled + idle work on the one game conn
	Paused     func() bool                         // gate from the control reader's paused flag
	Draining   func() bool                         // second gate (drain): finish current pass, take no new work
	SetDrained func(bool)                          // publishes whether the worker is held idle due to drain
	// Quiesced is the third gate: an operator park request, read from disk so
	// it survives a restart (see quiesce.go). Like drain it is consulted only
	// between passes, so a park never truncates a run in flight -- a hauler
	// finishes its claim, a miner finishes its deposit. Returns the operator's
	// reason alongside the flag.
	Quiesced func() (bool, string)
	// SetQuiesced publishes the park state (and reason) for the control channel.
	SetQuiesced func(bool, string)
	Out         io.Writer        // worker stdout / logs
	NowFn       func() time.Time // injectable clock

	IdleInterval time.Duration // between idle passes (0 → game.SleepTick)
	// KeepaliveInterval is how long a worker may stay silent before it sends a
	// single get_status purely to prove liveness (0 → game.SleepKeepalive). It
	// applies ONLY to a pass that put nothing on the wire: a pass that did real
	// work is already proof of life, so no heartbeat is appended to it.
	KeepaliveInterval time.Duration
	ScheduleInterval  time.Duration // scheduler tick (0 → game.SleepLong)
	// CommandTimeout caps a single dispatched command (0 → game.SleepCommandMaxWait).
	// Injectable so tests can assert the bound without waiting 30 minutes.
	CommandTimeout time.Duration
	AgentID        string // for script resolution search paths

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
// applyStandingDefaults fills the zero values of deps in place.
//
// IdleInterval is deliberately one full game tick, NOT SleepShort. It used to
// default to SleepShort (SleepTick/3 = 3.33s), which ran every worker's idle
// pass three times per tick -- roughly 43 passes per second across a
// 144-worker fleet. Nothing useful can happen at that rate: the game advances
// once per 10s and a mutation is capped at 1 per tick per agent regardless, so
// the extra passes could only emit redundant calls. On 2026-08-27 the fleet
// spent 4.5 hours IP-blocked, with find_route timeouts and "Your IP has been
// temporarily blocked" stranding seven miners. Each command's own response time
// is added on top of this interval by the blocking dispatch, so the real loop
// period is a tick plus the work.
func applyStandingDefaults(deps *StandingDeps) {
	if deps.Out == nil {
		deps.Out = io.Discard
	}
	if deps.NowFn == nil {
		deps.NowFn = func() time.Time { return time.Now().UTC() }
	}
	if deps.IdleInterval == 0 {
		deps.IdleInterval = game.SleepTick
	}
	if deps.KeepaliveInterval == 0 {
		deps.KeepaliveInterval = game.SleepKeepalive
	}
	if deps.ScheduleInterval == 0 {
		deps.ScheduleInterval = game.SleepLong
	}
	if deps.CommandTimeout == 0 {
		deps.CommandTimeout = game.SleepCommandMaxWait
	}
	if deps.SetDrained == nil {
		deps.SetDrained = func(bool) {}
	}
	if deps.SetQuiesced == nil {
		deps.SetQuiesced = func(bool, string) {}
	}
}

func RunStanding(ctx context.Context, role Role, deps StandingDeps) error {
	applyStandingDefaults(&deps)

	// Register schedule entries (idempotent: skip a command already covered, so
	// a restart does not duplicate it).
	//
	// "Covered" is not "exactly this frequency". The test used to key on
	// frequency|command, which meant a role's `hourly update_market` did not
	// match a hand-added `ten_minutely update_market` and was added beside it —
	// and because seeding runs on EVERY start, it came back after every restart.
	// The two then fire in the SAME scheduler pass at the top of each hour, since
	// boundaries are wall-clock aligned and :00 is both an hourly and a
	// ten-minutely mark: one wasted capture per agent per hour. Live 2026-08-13
	// that was 43 of 45 marketbots, plus a redundant daily kb_update on nine.
	if deps.Scheduler != nil {
		existing := deps.Scheduler.List()
		covered := func(se ScheduleEntry) bool {
			for _, t := range existing {
				if t.Command == se.Command && Covers(t.Frequency, se.Every) {
					return true
				}
			}
			return false
		}
		for _, se := range role.Schedule {
			if covered(se) {
				continue
			}
			task, err := deps.Scheduler.Add(se.Every, se.Command, deps.NowFn())
			if err != nil {
				fmt.Fprintf(deps.Out, "standing: schedule add %q failed: %v\n", se.Command, err) //nolint:errcheck
				continue
			}
			// Keep the local view current so a role listing the same command at
			// two frequencies does not seed a self-duplicate.
			existing = append(existing, task)
		}
		// Retire whatever the seed just made redundant. A role whose cadence has
		// been raised (resident's update_market went hourly -> ten_minutely on
		// 2026-08-13) adds the finer entry above, and the agent's old coarser one
		// would otherwise fire alongside it forever.
		for _, t := range deps.Scheduler.RetireCovered() {
			fmt.Fprintf(deps.Out, "standing: retired scheduled #%d (%s) %s — already covered by a finer schedule\n", //nolint:errcheck
				t.ID, t.Frequency, t.Command)
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
	wire := serverCalls(deps.Runner)
	// lastWire is when this worker last put something on the wire. Zero means
	// "never", so a freshly started worker heartbeats on its first pass.
	var lastWire time.Time

	// Idle loop.
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		paused := deps.Paused != nil && deps.Paused()
		draining := deps.Draining != nil && deps.Draining()
		parked, parkReason := false, ""
		if deps.Quiesced != nil {
			parked, parkReason = deps.Quiesced()
		}
		if paused || draining || parked {
			deps.SetDrained(draining)            // drained only when held *because* of drain
			deps.SetQuiesced(parked, parkReason) // likewise, parked only when held by the park
			if sleepCtx(ctx, deps.IdleInterval) {
				return nil
			}
			continue
		}
		deps.SetDrained(false)
		deps.SetQuiesced(false, "")
		deps.ExecMu.Lock()
		var before uint64
		if wire != nil {
			before = wire()
		}
		ran := false
		if deps.PayDebts != nil {
			deps.PayDebts(ctx)
		}
		if deps.Handoffs != nil {
			deps.Handoffs(ctx)
		}
		if task := deps.nextTask(); task != nil {
			deps.runTask(ctx, task)
			ran = true
		} else {
			for _, line := range idleCmds {
				select {
				case <-ctx.Done():
					deps.ExecMu.Unlock()
					return nil
				default:
				}
				_ = deps.runLine(ctx, line)
				ran = true
			}
		}
		deps.ExecMu.Unlock()

		// Liveness, not polling. A pass that reached the server has already
		// proved this worker is alive, so nothing is appended to it. Only a
		// silent pass can owe a heartbeat, and then at most one per
		// KeepaliveInterval -- never one per tick.
		spoke := ran
		if wire != nil {
			// The counter is the better signal where the runner offers one: it
			// sees through the dispatch's redundancy guard, so a pass whose
			// every command was skipped locally counts as silent, and it also
			// catches wire traffic from PayDebts, Handoffs and the scheduler.
			spoke = wire() != before
		}
		now := deps.NowFn()
		switch {
		case spoke:
			lastWire = now
		case lastWire.IsZero() || now.Sub(lastWire) >= deps.KeepaliveInterval:
			deps.ExecMu.Lock()
			_ = deps.runLine(ctx, "get_status")
			deps.ExecMu.Unlock()
			lastWire = now
		}

		if sleepCtx(ctx, deps.IdleInterval) {
			return nil
		}
	}
}

// serverCallReporter is implemented by a CommandRunner that can report how many
// commands it has actually put on the wire. WorkerDispatch does; the play_as
// REPL runner does not, and callers must tolerate a nil counter.
type serverCallReporter interface{ ServerCalls() uint64 }

// serverCalls returns r's wire-call counter, or nil when r cannot report one.
func serverCalls(r CommandRunner) func() uint64 {
	rep, ok := r.(serverCallReporter)
	if !ok {
		return nil
	}
	return rep.ServerCalls
}

// resolveIdle loads the role's idle script into command lines. A role with no
// script -- or an unreadable one -- yields NO commands: it used to fall back to
// a single get_status, which is how an unconfigured worker came to poll the
// server every tick forever. Liveness is the keepalive's job now, so a silent
// role is genuinely silent.
func (deps StandingDeps) resolveIdle(role Role) []string {
	if role.Idle == "" {
		return nil
	}
	path, ok := ResolveScriptArg(role.Idle, deps.AgentID)
	if !ok {
		fmt.Fprintf(deps.Out, "standing: idle script %q not found; idling silently\n", role.Idle) //nolint:errcheck
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(deps.Out, "standing: read idle script %q: %v; idling silently\n", role.Idle, err) //nolint:errcheck
		return nil
	}
	cmds, err := SplitScriptCommands(string(content))
	if err != nil {
		fmt.Fprintf(deps.Out, "standing: parse idle script %q: %v; idling silently\n", role.Idle, err) //nolint:errcheck
		return nil
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
	// Bound the command. Without this a reply lost to a disconnect parks the
	// caller forever inside RequestHandle.Result (a sync.Cond wait that only
	// the reply or a context cancel releases) while holding ExecMu -- which
	// starves the scheduler too, since both run under that mutex. The ceiling
	// is the whole fix: Result already breaks on ctx.Err(), and Submit
	// registers a context.AfterFunc that broadcasts the cond on cancel, so the
	// existing cancellation path does the waking. A zero value here means a
	// direct caller (tests) opted out.
	if deps.CommandTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, deps.CommandTimeout)
		defer cancel()
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
