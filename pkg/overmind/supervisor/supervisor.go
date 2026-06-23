package supervisor

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// SpawnFunc starts a worker process for spec, told to dial socket.
type SpawnFunc func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error)

// DefaultSpawn returns a SpawnFunc that launches workerBin with flags.
func DefaultSpawn(workerBin string) SpawnFunc {
	return func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx,
			workerBin,
			"--agent", spec.AgentID,
			"--role", spec.Role,
			"--station", spec.Station,
			"--socket", socket,
		)
		cmd.Stdout = log.Writer()
		cmd.Stderr = log.Writer()
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("supervisor: start worker %q: %w", spec.AgentID, err)
		}
		return cmd, nil
	}
}

// workerProc tracks one live worker process so the supervisor can tell a
// still-booting worker apart from a dead one, and can kill a hung one before
// respawning. exited is closed by the reaping goroutine when cmd.Wait returns.
type workerProc struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc // cancels the worker's ctx -> SIGKILL
	launchedAt time.Time
	exited     chan struct{}
}

// alive reports whether the process has not yet exited.
func (p *workerProc) alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}

// Supervisor spawns and keeps workers alive.
type Supervisor struct {
	server *Server
	fleet  *Fleet
	specs  []WorkerSpec
	spawn  SpawnFunc
	logger *log.Logger
	// SilenceTimeout is the heartbeat-gap tolerance for an established worker
	// (one that has already sent Hello). Cold-start is covered by BootTimeout.
	SilenceTimeout time.Duration
	MaxRestarts    int
	restarts       map[string]int

	// procs tracks the live process per agent id; procMu guards it.
	procMu sync.Mutex
	procs  map[string]*workerProc

	// StaggerInterval spaces initial worker launches to stay under the per-IP
	// /login rate limit. BootTimeout bounds how long a worker may be alive but
	// not yet have sent Hello before it is treated as wedged. KillGrace is the
	// SIGTERM->SIGKILL escalation window.
	StaggerInterval time.Duration
	BootTimeout     time.Duration
	KillGrace       time.Duration
}

// NewSupervisor wires a supervisor. server may be nil in tests.
func NewSupervisor(server *Server, fleet *Fleet, specs []WorkerSpec, spawn SpawnFunc, logger *log.Logger) *Supervisor {
	return &Supervisor{
		server: server, fleet: fleet, specs: specs, spawn: spawn, logger: logger,
		SilenceTimeout:  9 * game.SleepTick,  // 90s: heartbeat-gap tolerance for established workers
		BootTimeout:     30 * game.SleepTick, // 5min: max alive-but-no-Hello before a boot is "wedged"
		StaggerInterval: game.SleepMedium,    // 5s between initial spawns (per-IP /login pacing)
		KillGrace:       game.SleepMedium,    // 5s SIGTERM->SIGKILL window
		MaxRestarts:     100,
		restarts:        make(map[string]int),
		procs:           make(map[string]*workerProc),
	}
}

func (s *Supervisor) socket() string {
	if s.server == nil {
		return ""
	}
	return s.server.Addr()
}

// Run spawns each spec, then periodically restarts silent/dead workers.
func (s *Supervisor) Run(ctx context.Context) error {
	for i, spec := range s.specs {
		if i > 0 && s.StaggerInterval > 0 {
			select {
			case <-time.After(s.StaggerInterval):
			case <-ctx.Done():
				return nil
			}
		}
		s.launch(ctx, spec)
	}
	ticker := time.NewTicker(game.SleepMedium)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.reapAndRestart(ctx)
		}
	}
}

func (s *Supervisor) launch(ctx context.Context, spec WorkerSpec) {
	wctx, wcancel := context.WithCancel(ctx)
	cmd, err := s.spawn(wctx, spec, s.socket())
	if err != nil {
		wcancel()
		s.logger.Printf("spawn %q failed: %v", spec.AgentID, err)
		return
	}
	if cmd == nil {
		wcancel()
		return
	}
	proc := &workerProc{
		cmd:        cmd,
		cancel:     wcancel,
		launchedAt: time.Now(),
		exited:     make(chan struct{}),
	}
	s.procMu.Lock()
	s.procs[spec.AgentID] = proc
	s.procMu.Unlock()
	go func() {
		// Reap the child when it exits (or is killed on ctx cancel) so it does
		// not linger as a zombie. wcancel here releases the per-worker context
		// when the process ends on its own; it is idempotent with kill().
		_ = cmd.Wait()
		proc.cancel()
		close(proc.exited)
	}()
}

// kill terminates a live worker: SIGTERM first (the worker checkpoints and
// exits on it), escalating to SIGKILL via ctx cancel if it does not exit
// within KillGrace. Returns only once the process has actually exited, so the
// caller can safely respawn without two processes for one agent.
func (s *Supervisor) kill(p *workerProc) {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.exited:
		// Exited cleanly within the grace window.
	case <-time.After(s.KillGrace):
		p.cancel() // ctx cancel -> SIGKILL via exec.CommandContext
		<-p.exited
	}
}

func (s *Supervisor) reapAndRestart(ctx context.Context) {
	now := time.Now()
	healthy := make(map[string]WorkerInfo)
	for _, w := range s.fleet.Snapshot() {
		healthy[w.AgentID] = w
	}
	for _, spec := range s.specs {
		proc := procSnapshot(s, spec.AgentID)

		// No process tracked: never launched, or fully reaped earlier.
		if proc == nil {
			s.tryRestart(ctx, spec, false)
			continue
		}

		if proc.alive() {
			w, seen := healthy[spec.AgentID]
			switch {
			case seen && NeedsRestart(w, now, s.SilenceTimeout):
				// Established worker whose heartbeat went silent: hung.
				s.kill(proc)
				s.tryRestart(ctx, spec, true)
			case !seen && now.Sub(proc.launchedAt) > s.BootTimeout:
				// Alive but never sent Hello within the boot window: wedged.
				s.kill(proc)
				s.tryRestart(ctx, spec, true)
			case seen && w.Healthy:
				// Healthy: clear the crash-loop counter so MaxRestarts bounds
				// restarts-per-incident, not lifetime restarts.
				delete(s.restarts, spec.AgentID)
			default:
				// Still booting (alive, no Hello yet, within BootTimeout): leave it.
			}
			continue
		}

		// Process has exited: respawn (subject to the crash-loop cap).
		s.tryRestart(ctx, spec, false)
	}
}

// procSnapshot returns the tracked proc for an agent under the registry lock.
func procSnapshot(s *Supervisor, agentID string) *workerProc {
	s.procMu.Lock()
	defer s.procMu.Unlock()
	return s.procs[agentID]
}

// tryRestart relaunches spec unless the crash-loop cap is reached. killed marks
// whether a live process was just terminated (for the log line).
func (s *Supervisor) tryRestart(ctx context.Context, spec WorkerSpec, killed bool) {
	if s.restarts[spec.AgentID] >= s.MaxRestarts {
		return
	}
	s.restarts[spec.AgentID]++
	s.fleet.MarkRestart(spec.AgentID)
	s.logger.Printf("restarting worker %q (killed=%v, restart #%d)", spec.AgentID, killed, s.restarts[spec.AgentID])
	s.launch(ctx, spec)
}
