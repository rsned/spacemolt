package supervisor

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// SpawnFunc starts a worker process for spec, told to dial socket.
type SpawnFunc func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error)

// DefaultSpawn returns a SpawnFunc that launches workerBin with flags.
// handoffQueuePath, when non-empty, is forwarded to every spawned worker as
// --handoff-queue so marketbot/craftsman workers share the same crafting-brain
// stock handoff queue file the overmind itself uses. An empty path forwards
// nothing, leaving the worker's own --handoff-queue default (disabled) in
// effect. assetsDBPath follows the identical shape: forwarded as
// --assets-db-path when non-empty, otherwise omitted, leaving the worker's
// own default ("", asset capture disabled) in effect.
func DefaultSpawn(workerBin, handoffQueuePath, assetsDBPath string) SpawnFunc {
	return SpawnWith(SpawnConfig{
		WorkerBin:        workerBin,
		HandoffQueuePath: handoffQueuePath,
		AssetsDBPath:     assetsDBPath,
	})
}

// SpawnConfig carries the fleet-wide values forwarded to every worker this
// overmind starts. Each is omitted from the worker's argv when empty, leaving
// the worker's own default in effect — so adding one here never changes the
// behaviour of a fleet that does not set it.
type SpawnConfig struct {
	WorkerBin string
	// HandoffQueuePath is the shared crafting-brain stock handoff queue.
	HandoffQueuePath string
	// AssetsDBPath is the agent asset + capability ledger.
	AssetsDBPath string
	// SecondmentPath is the fleet-loan ledger a worker writes nominations into.
	// Only the haul fleet sets it today: a hauler that finishes a delivery in
	// nebula space nominates itself for the pirate-unlock chain, which is cheap
	// to run from there and a 20+ jump deadhead from anywhere else.
	SecondmentPath string
	// FleetName is recorded on a nomination so the reconciler knows which fleet
	// to return the agent to.
	FleetName string
}

// SpawnWith returns a SpawnFunc that launches cfg.WorkerBin with flags.
func SpawnWith(cfg SpawnConfig) SpawnFunc {
	return func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		args := []string{
			"--agent", spec.AgentID,
			"--role", spec.Role,
			"--station", spec.Station,
			"--socket", socket,
		}
		if cfg.HandoffQueuePath != "" {
			args = append(args, "--handoff-queue", cfg.HandoffQueuePath)
		}
		if cfg.AssetsDBPath != "" {
			args = append(args, "--assets-db-path", cfg.AssetsDBPath)
		}
		if cfg.SecondmentPath != "" {
			args = append(args, "--secondment-ledger", cfg.SecondmentPath)
		}
		if cfg.FleetName != "" {
			args = append(args, "--fleet-name", cfg.FleetName)
		}
		if len(spec.MissionCategories) > 0 {
			args = append(args, "--mission-categories", strings.Join(spec.MissionCategories, ","))
		}
		if spec.EnableFreight {
			args = append(args, "--enable-freight")
		}
		if spec.FreightMaxPackages > 1 {
			args = append(args, "--freight-max-packages", strconv.Itoa(spec.FreightMaxPackages))
		}
		if spec.DisableFreightBootstrap {
			args = append(args, "--freight-bootstrap=false")
		}
		cmd := exec.CommandContext(ctx, cfg.WorkerBin, args...)
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
	// StallTimeout bounds how long a worker may heartbeat while frozen (undocked,
	// no system/POI/credit progress) before the stall watchdog restarts it — the
	// safety net for a "healthy but stuck" worker that SilenceTimeout cannot see
	// (e.g. a shuttle wedged undocked in a station-less system). A non-positive
	// value disables the watchdog.
	StallTimeout time.Duration
	// DisconnectGrace bounds how long a worker that reports its game connection
	// down (LastStatus.Disconnected) is left to the fleet-wide reconnect gate
	// before the stall watchdog falls back to restarting it. A disconnected
	// worker freezes (no progress) and would otherwise trip the stall watchdog,
	// whose restart forces a fresh login — the exact login storm that trips and
	// deepens a per-IP rate-limit block, turning a brief server hiccup into a
	// multi-hour outage. The gate already reconnects gracefully (paced, honoring
	// the block); only a worker still disconnected past this grace is treated as
	// wedged and restarted. A non-positive value disables the exemption.
	DisconnectGrace time.Duration
	MaxRestarts     int
	restarts        map[string]int

	// Stranded-quarantine tuning (see Stranded in fleet.go). OnQuarantine is
	// invoked (from the reap goroutine) after a worker is quarantined so the
	// host can enqueue a rescue; nil-safe.
	FuelStrandFraction float64
	FuelStrandFloor    float64
	StallRestartLimit  int
	OnQuarantine       func(w WorkerInfo, reason string)

	// releases holds ReleaseQuarantine requests until the next reap tick, so
	// the restarts map stays single-goroutine.
	releaseMu sync.Mutex
	releases  []string

	// Membership: queued roster changes applied at reap-tick start (same
	// single-goroutine ownership pattern as releases). leaving tracks
	// in-progress removals; RemoveDrainTimeout bounds the per-worker drain
	// wait before force-stop. Sender delivers per-worker control envelopes
	// (the *Server in production; a fake in tests).
	memberMu           sync.Mutex
	pending            []MembershipRequest
	specsMu            sync.Mutex
	leaving            map[string]*leavingState
	Sender             ControlSender
	RemoveDrainTimeout time.Duration

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

	// RestartBatch caps how many workers reapAndRestart may (re)launch in a
	// single reap tick, so a mass restart (many workers dying at once) does not
	// burst the per-IP /login limit. The reap loop runs every game.SleepMedium,
	// so the default of 1 paces relaunches at the same ~12/min as the initial
	// stagger. Empirically the per-IP connect throttle engages around ~25/min
	// (see cmd/debug/login-probe), so 1-per-5s-tick keeps ~2x headroom. A
	// non-positive value disables pacing (relaunch everything eligible at once).
	RestartBatch int
}

// NewSupervisor wires a supervisor. server may be nil in tests.
func NewSupervisor(server *Server, fleet *Fleet, specs []WorkerSpec, spawn SpawnFunc, logger *log.Logger) *Supervisor {
	s := &Supervisor{
		server: server, fleet: fleet, specs: specs, spawn: spawn, logger: logger,
		SilenceTimeout:  9 * game.SleepTick,   // 90s: heartbeat-gap tolerance for established workers
		StallTimeout:    90 * game.SleepTick,  // 15min: undocked-and-frozen tolerance (stall watchdog)
		DisconnectGrace: 180 * game.SleepTick, // 30min: leave a reconnecting worker to the gate before restarting
		BootTimeout:     30 * game.SleepTick,  // 5min: max alive-but-no-Hello before a boot is "wedged"
		StaggerInterval: game.SleepMedium,     // 5s between initial spawns (per-IP /login pacing)
		KillGrace:       game.SleepMedium,     // 5s SIGTERM->SIGKILL window
		RestartBatch:    1,                    // 1 relaunch per reap tick (~12/min, mirrors stagger)
		MaxRestarts:     100,
		restarts:        make(map[string]int),
		procs:           make(map[string]*workerProc),

		FuelStrandFraction: 0.10, // fuel-dead when fuel < max(10% of tank, floor)
		FuelStrandFloor:    10,
		StallRestartLimit:  3, // quarantine after 3 futile stall-restarts

		leaving:            make(map[string]*leavingState),
		RemoveDrainTimeout: DefaultRemoveDrainTimeout,
	}
	if server != nil {
		s.Sender = server
	}
	return s
}

// ReleaseQuarantine schedules a quarantined worker for relaunch on the next
// reap tick (after its rescue record is done). Safe from any goroutine.
func (s *Supervisor) ReleaseQuarantine(agentID string) {
	s.releaseMu.Lock()
	defer s.releaseMu.Unlock()
	s.releases = append(s.releases, agentID)
}

// Tick runs one reap-and-restart pass: drains pending releases, restarts
// silent/dead/stalled workers, and quarantines newly-stranded ones. Run calls
// this on its own ticker; it is exported separately so callers outside this
// package (e.g. the cmd/overmind rejoin-glue tests) can drive a deterministic
// tick instead of waiting on Run's game.SleepMedium ticker. Not safe to call
// concurrently with Run or with itself: the pass mutates the unsynchronized
// restarts map, which Run's ticker goroutine owns.
func (s *Supervisor) Tick(ctx context.Context) {
	s.reapAndRestart(ctx)
}

func (s *Supervisor) drainReleases() []string {
	s.releaseMu.Lock()
	defer s.releaseMu.Unlock()
	out := s.releases
	s.releases = nil
	return out
}

func (s *Supervisor) socket() string {
	if s.server == nil {
		return ""
	}
	return s.server.Addr()
}

// Run spawns each spec, then periodically restarts silent/dead workers.
func (s *Supervisor) Run(ctx context.Context) error {
	for i, spec := range s.Roster() {
		if s.fleet.IsQuarantined(spec.AgentID) {
			continue // restored-from-queue quarantine: do not launch stranded
		}
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
	now0 := time.Now()
	// Drain quarantine releases BEFORE applying membership (F3): if a quarantined
	// agent gets both a ReleaseQuarantine and a membership-remove in the same
	// tick, running applyMembership first would completeRemoval -> fleet.Remove
	// the entry, and the subsequent ClearQuarantine's fleet.get would re-create a
	// blank ghost entry that never reaps. Clearing quarantine first lets
	// memberRemove take the ordinary (non-quarantined) drain/stop path and leaves
	// no ghost. Releases without a paired remove behave identically either way.
	for _, id := range s.drainReleases() {
		s.fleet.ClearQuarantine(id)
		delete(s.restarts, id)
		s.logger.Printf("quarantine released for %q; relaunching", id)
	}
	s.applyMembership(now0)

	now := time.Now()
	healthy := make(map[string]WorkerInfo)
	for _, w := range s.fleet.Snapshot() {
		healthy[w.AgentID] = w
	}
	// budget caps relaunches this tick so a mass restart does not burst the
	// per-IP /login limit; a non-positive RestartBatch disables the cap.
	budget := s.RestartBatch
	if budget <= 0 {
		budget = len(s.Roster())
	}
	s.progressLeaving(ctx, now0, &budget)
	// Snapshot the roster AFTER progressLeaving: a removal or rolling update
	// completing this tick mutates s.specs (completeRemoval), and the main
	// loop below must see that result — otherwise a just-removed agent would
	// still appear in a stale pre-progressLeaving roster copy, fall into the
	// proc==nil branch, and get instantly relaunched via tryRestart.
	roster := s.Roster()
	for _, spec := range roster {
		if s.fleet.IsQuarantined(spec.AgentID) {
			continue // pulled from fleet; waiting on rescue
		}
		if s.leaving[spec.AgentID] != nil {
			continue // removal in progress; not a restart candidate
		}
		proc := procSnapshot(s, spec.AgentID)

		// No process tracked: never launched, or fully reaped earlier.
		if proc == nil {
			s.tryRestart(ctx, spec, false, &budget)
			continue
		}

		if proc.alive() {
			w, seen := healthy[spec.AgentID]
			// A worker whose only fleet entry came from tryRestart's MarkRestart
			// bookkeeping (never an actual Hello) has a zero LastSeen, which would
			// otherwise make NeedsRestart's silence check fire immediately on the
			// very next tick — killing a worker before it ever gets a chance to
			// report in. Treat that as not-yet-seen; BootTimeout governs it instead.
			seen = seen && !w.LastSeen.IsZero()
			switch {
			case seen && NeedsRestart(w, now, s.SilenceTimeout):
				// Established worker whose heartbeat went silent: hung. Kill it
				// now regardless of budget (stop the bleeding); the relaunch is
				// budget-gated and retried next tick if deferred.
				s.kill(proc)
				s.tryRestart(ctx, spec, true, &budget)
			case seen && w.LastStatus.Disconnected && s.DisconnectGrace > 0 && now.Sub(w.DisconnectedSince) <= s.DisconnectGrace:
				// Game-disconnected but still heartbeating over the control socket:
				// it self-heals via the fleet-wide reconnect gate, which paces logins
				// and honors any per-IP block. A restart here forces a fresh login
				// that cannot succeed during a block and deepens it — the storm that
				// turns a brief server hiccup into a multi-hour outage. Leave it to
				// the gate until DisconnectGrace elapses; only then does it fall
				// through (next ticks) to the stall watchdog as genuinely wedged.
			case seen && Stalled(w, now, s.StallTimeout):
				if stranded, reason := Stranded(w, now, s.StallTimeout, s.FuelStrandFraction, s.FuelStrandFloor, s.StallRestartLimit); stranded {
					// Beyond what a restart can fix: pull it from the fleet. The
					// kill frees the game session for the rescuer/operator.
					if s.logger != nil {
						s.logger.Printf("QUARANTINED %s: %s; rescue queued — no further restarts", spec.AgentID, reason)
					}
					s.kill(proc)
					// Ordering constraint: the rescue record must land (OnQuarantine
					// can take seconds — KB enrichment, disk I/O) before the
					// Quarantined flag becomes visible to the rejoin poll. The host's
					// pollRescues treats "quarantined with no record" as a manual
					// release signal, so flipping the flag first would race a
					// legitimate enqueue-in-flight into an immediate, wrong release.
					if s.OnQuarantine != nil {
						s.OnQuarantine(w, reason)
					}
					s.fleet.Quarantine(spec.AgentID, reason)
					continue
				}
				// Heartbeating but frozen: undocked with no progress for a long
				// window (the station-less-pocket trap). A plain restart forces a
				// fresh login + reconcile, which re-runs the role's stranded-recovery
				// (e.g. the shuttle escape hatch), so the respawn actually recovers.
				if s.logger != nil {
					s.logger.Printf("stall watchdog: %s frozen undocked in %q for >%s (last progress %s); restarting",
						spec.AgentID, w.LastStatus.System, s.StallTimeout, w.LastProgress.Format(time.RFC3339))
				}
				s.fleet.MarkStallRestart(spec.AgentID)
				s.kill(proc)
				s.tryRestart(ctx, spec, true, &budget)
			case !seen && now.Sub(proc.launchedAt) > s.BootTimeout:
				// Alive but never sent Hello within the boot window: wedged.
				s.kill(proc)
				s.tryRestart(ctx, spec, true, &budget)
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
		s.tryRestart(ctx, spec, false, &budget)
	}
}

// procSnapshot returns the tracked proc for an agent under the registry lock.
func procSnapshot(s *Supervisor, agentID string) *workerProc {
	s.procMu.Lock()
	defer s.procMu.Unlock()
	return s.procs[agentID]
}

// tryRestart relaunches spec unless the crash-loop cap is reached or this
// tick's relaunch budget is exhausted. killed marks whether a live process was
// just terminated (for the log line). budget is decremented only when a launch
// actually happens; a budget-deferred spec is retried on the next reap tick.
func (s *Supervisor) tryRestart(ctx context.Context, spec WorkerSpec, killed bool, budget *int) {
	if s.restarts[spec.AgentID] >= s.MaxRestarts {
		return
	}
	if *budget <= 0 {
		// This tick's per-IP /login pacing budget is spent; defer to next tick.
		return
	}
	*budget--
	s.restarts[spec.AgentID]++
	s.fleet.MarkRestart(spec.AgentID)
	s.logger.Printf("restarting worker %q (killed=%v, restart #%d)", spec.AgentID, killed, s.restarts[spec.AgentID])
	s.launch(ctx, spec)
}
