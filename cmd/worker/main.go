// Command worker is a thin stub overmind worker process.
//
// It connects to the game server, reports heartbeats to the overmind over the
// control socket, checkpoints its state, and obeys abort/pause/resume.  In
// Plan A it runs no real automation — it proves the
// connect→hello→heartbeat→checkpoint→abort skeleton.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/overmind/checkpoint"
	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/rescue"
	"github.com/rsned/spacemolt/pkg/worker"
)

func main() {
	agentID := flag.String("agent", "", "Agent ID (required, e.g. miner-1)")
	role := flag.String("role", "idle", "Worker role (e.g. miner, trader)")
	station := flag.String("station", "", "Home station POI (optional)")
	socketPath := flag.String("socket", "", "Unix socket path to overmind control channel")
	dbPath := flag.String("db-path", "", "Path to checkpoint DB (default: data/agents/<agent>/checkpoint.db)")
	rolesPath := flag.String("roles", filepath.Join("data", "overmind", "roles.yaml"), "Path to roles config")
	kbPath := flag.String("kb-path", filepath.Join("data", "spacemolt-knowledge.db"), "Path to shared knowledge base")
	marketDBPath := flag.String("market-db-path", filepath.Join("data", "market.db"), "Path to market collector database")
	rescueQueuePath := flag.String("rescue-queue", filepath.Join("data", "overmind", "rescue-queue.json"), "Shared stranded-worker rescue queue file")
	handoffQueuePath := flag.String("handoff-queue", "", "Shared crafting-brain stock handoff queue file (empty disables handoff fulfillment)")
	missionCategories := flag.String("mission-categories", "", "Comma-separated mission-board categories for the missions role (empty = delivery only; learning pool: delivery,exploration)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *agentID == "" {
		fmt.Fprintln(os.Stderr, "Usage: worker --agent <agent-id> --socket <path> [flags]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[worker:%s] ", *agentID), log.LstdFlags)

	// Resolve DB path.
	resolvedDB := *dbPath
	if resolvedDB == "" {
		resolvedDB = filepath.Join("data", "agents", *agentID, "checkpoint.db")
	}

	// ── Step 1: Open checkpoint and load saved known-state ──────────────────
	store, err := checkpoint.Open(resolvedDB)
	if err != nil {
		log.Fatalf("open checkpoint: %v", err)
	}
	defer func() { _ = store.Close() }()

	saved, _, err := store.LoadKnownState()
	if err != nil {
		log.Fatalf("load known state: %v", err)
	}

	// Load saved intent for the standing behavior label.
	savedIntent, hasIntent, err := store.LoadIntent()
	if err != nil {
		log.Fatalf("load intent: %v", err)
	}
	standing := "idle"
	var pendingTask atomic.Pointer[worker.AssignedTask]
	var activeTaskID atomic.Pointer[string]
	// activity carries the role goroutine's current human-readable unit of work
	// (set via WorkerDispatch.setActivity) for the heartbeat goroutine to report.
	var activity atomic.Pointer[string]
	if hasIntent {
		standing = savedIntent.StandingBehavior
		if savedIntent.ActiveTaskID != "" {
			id := savedIntent.ActiveTaskID
			activeTaskID.Store(&id)
		}
	}

	// ── Step 2: Signal handling & root context ───────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("received signal %v, shutting down", sig)
		cancel()
	}()

	// ── Step 3: Connect to game server ───────────────────────────────────────
	logger.Printf("connecting to game server as %s", *agentID)
	client, _, err := game.InitializeAgent(*agentID, logger, ctx, *debug)
	if err != nil {
		log.Fatalf("initialize agent: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			logger.Printf("warning: close client: %v", closeErr)
		}
	}()

	// Fetch fresh state.
	if err := client.GetStatus(ctx); err != nil {
		logger.Printf("warning: get_status: %v", err)
	}
	if err := client.GetSystem(ctx); err != nil {
		logger.Printf("warning: get_system: %v", err)
	}
	st := client.GetState()

	live := buildKnownState(st, int(st.CurrentTick))

	// ── Step 4: Reconcile saved vs live state ────────────────────────────────
	rec := checkpoint.Reconcile(saved, live, 0.25)
	logger.Printf("reconcile: %s", rec.Disposition)

	// ── Step 5: Dial control socket (bounded retry) ──────────────────────────
	const maxDialAttempts = 10
	var conn net.Conn
	if *socketPath != "" {
		for attempt := range maxDialAttempts {
			if ctx.Err() != nil {
				log.Fatalf("context cancelled before socket connect")
			}
			conn, err = net.Dial("unix", *socketPath)
			if err == nil {
				break
			}
			logger.Printf("dial attempt %d/%d failed: %v; retrying in %v",
				attempt+1, maxDialAttempts, err, game.SleepQuick)
			select {
			case <-time.After(game.SleepQuick):
			case <-ctx.Done():
				log.Fatalf("context cancelled waiting for socket")
			}
		}
		if conn == nil {
			log.Fatalf("could not connect to control socket %s after %d attempts", *socketPath, maxDialAttempts)
		}
		defer func() { _ = conn.Close() }()
		logger.Printf("connected to control socket %s", *socketPath)
	} else {
		logger.Printf("no --socket specified; running without overmind control channel")
	}

	// Send Hello and reconcile_diverged event (if socket available).
	if conn != nil {
		enc := control.NewEncoder(conn)

		hello := control.Hello{
			AgentID: *agentID,
			Role:    *role,
			Station: *station,
			PID:     os.Getpid(),
		}
		if err := sendEnvelope(enc, control.TypeHello, *agentID, hello); err != nil {
			log.Fatalf("send hello: %v", err)
		}
		logger.Printf("sent hello")

		if rec.Disposition == checkpoint.Diverged {
			detail := strings.Join(rec.Reasons, "; ")
			evt := control.Event{
				Kind:      "reconcile_diverged",
				Detail:    detail,
				Timestamp: time.Now().Format(time.RFC3339Nano),
			}
			if err := sendEnvelope(enc, control.TypeEvent, *agentID, evt); err != nil {
				logger.Printf("warning: send reconcile_diverged event: %v", err)
			} else {
				logger.Printf("reconcile diverged: %s", detail)
			}
		}

		// ── Step 6: Reader goroutine ─────────────────────────────────────────
		var paused atomic.Bool
		var draining atomic.Bool
		var drained atomic.Bool
		readerDone := make(chan struct{})

		go func() {
			defer close(readerDone)
			dec := control.NewDecoder(conn)
			for {
				env, decErr := dec.Decode()
				if decErr != nil {
					if decErr == io.EOF {
						logger.Printf("control socket closed (EOF)")
					} else {
						logger.Printf("control decode error: %v", decErr)
					}
					cancel()
					return
				}
				switch env.Type {
				case control.TypeAbort:
					var ab control.Abort
					if intoErr := env.Into(&ab); intoErr != nil {
						logger.Printf("warning: decode abort payload: %v", intoErr)
					}
					logger.Printf("received abort: reason=%q flee=%v", ab.Reason, ab.Flee)
					// Save checkpoint before exit.
					liveNow := buildKnownState(client.GetState(), int(client.GetState().CurrentTick))
					if saveErr := store.SaveKnownState(liveNow); saveErr != nil {
						logger.Printf("warning: save known state on abort: %v", saveErr)
					}
					logger.Printf("checkpoint saved; exiting on abort")
					os.Exit(0)
				case control.TypeAssign:
					var as control.Assign
					if intoErr := env.Into(&as); intoErr != nil {
						logger.Printf("warning: decode assign payload: %v", intoErr)
						break
					}
					logger.Printf("received task %q (script=%s)", as.TaskID, as.Script)
					pendingTask.Store(&worker.AssignedTask{ID: as.TaskID, Script: as.Script, Params: as.Params})
				case control.TypePause:
					paused.Store(true)
					logger.Printf("paused")
				case control.TypeResume:
					paused.Store(false)
					draining.Store(false)
					logger.Printf("resumed")
				case control.TypeDrain:
					draining.Store(true)
					logger.Printf("draining: will finish current pass then idle")
				default:
					logger.Printf("unhandled control message type: %s", env.Type)
				}
			}
		}()

		// ── Step 6b: Open shared KB (best-effort) ───────────────────────────
		var kb knowledge.Base
		if sqliteKB, kbErr := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *kbPath, WAL: true}); kbErr != nil {
			logger.Printf("warning: open KB %s: %v (tracking disabled)", *kbPath, kbErr)
		} else {
			kb = sqliteKB
			defer func() { _ = sqliteKB.Close() }()
		}

		// ── Step 6b2: Open market collector (best-effort) ───────────────────
		var mc *market.Collector
		if mktColl, mktErr := market.Open(market.Config{DBPath: *marketDBPath, WAL: true}); mktErr != nil {
			logger.Printf("warning: open market DB %s: %v (market snapshots disabled)", *marketDBPath, mktErr)
		} else {
			mc = mktColl
			defer func() { _ = mktColl.Close() }()
		}

		// ── Step 6c: Standing behavior ───────────────────────────────────────
		roles, rolesErr := worker.LoadRoles(*rolesPath)
		if rolesErr != nil {
			logger.Printf("warning: load roles %s: %v (no standing behavior)", *rolesPath, rolesErr)
		}
		roleCfg, haveRole := roles[*role]
		if haveRole {
			dispatch := worker.NewWorkerDispatch(client, kb, mc, os.Stdout)
			dispatch.AgentID = *agentID
			dispatch.Station = *station
			dispatch.SetActivitySink(&activity)
			dispatch.Rescue = rescue.NewQueue(*rescueQueuePath)
			if *missionCategories != "" {
				for c := range strings.SplitSeq(*missionCategories, ",") {
					if c = strings.TrimSpace(c); c != "" {
						dispatch.MissionCategories = append(dispatch.MissionCategories, c)
					}
				}
			}
			var handoffQueue *handoff.Queue
			if *handoffQueuePath != "" {
				handoffQueue = handoff.NewQueue(*handoffQueuePath)
			}
			sched, schedErr := worker.LoadScheduler(filepath.Join("data", "agents", *agentID, "schedule.json"))
			if schedErr != nil {
				logger.Printf("warning: load scheduler: %v", schedErr)
			}
			var execMu sync.Mutex
			go func() {
				deps := worker.StandingDeps{
					Runner:     dispatch,
					Scheduler:  sched,
					Client:     client,
					ExecMu:     &execMu,
					Paused:     paused.Load,
					Draining:   draining.Load,
					SetDrained: drained.Store,
					Out:        os.Stdout,
					NowFn:      func() time.Time { return time.Now().UTC() },
					AgentID:    *agentID,
					NextTask: func() *worker.AssignedTask {
						t := pendingTask.Swap(nil)
						if t != nil {
							id := t.ID
							activeTaskID.Store(&id)
						}
						return t
					},
					PayDebts: func(c context.Context) {
						worker.PayRescueDebt(c, client, os.Stdout, filepath.Join("data", "agents"), *agentID)
					},
					OnTaskResult: func(taskID string, err error) {
						kind := "task_done"
						detail := taskID
						if err != nil {
							kind = "task_failed"
							detail = taskID + ": " + err.Error()
						}
						if sendErr := sendEnvelope(enc, control.TypeEvent, *agentID, control.Event{
							Kind: kind, Detail: detail, Timestamp: time.Now().Format(time.RFC3339Nano),
						}); sendErr != nil {
							logger.Printf("warning: send task event: %v", sendErr)
						}
						activeTaskID.Store(nil)
						logger.Printf("task %s finished: %s", taskID, kind)
					},
				}
				if handoffQueue != nil {
					deps.Handoffs = func(c context.Context) { _ = dispatch.HandoffPass(c, handoffQueue) }
				}
				if rerr := worker.RunStanding(ctx, roleCfg, deps); rerr != nil {
					logger.Printf("standing behavior ended: %v", rerr)
				}
			}()
			standing = *role
			logger.Printf("standing behavior started for role %q", *role)
		} else {
			logger.Printf("no standing behavior for role %q; idle heartbeat only", *role)
		}

		// ── Step 7: Heartbeat loop ────────────────────────────────────────────
		ticker := time.NewTicker(game.SleepTick)
		defer ticker.Stop()

	heartbeat:
		for {
			select {
			case <-ctx.Done():
				break heartbeat
			case <-readerDone:
				break heartbeat
			case <-ticker.C:
				// Re-arm reconnection if the game connection dropped — a standing
				// worker issues no command to wake the dormant handler on its own.
				if nudgeReconnect(client) {
					logger.Printf("game connection down; requested reconnect")
				}
				nowState := client.GetState()
				nowTick := int(nowState.CurrentTick)

				// Self-heal the faction tag for a worker that joined a faction
				// mid-run: the tag is only carried by faction_info, not any
				// player payload, so fetch it once when we have a faction id but
				// no tag yet. The response populates state asynchronously via the
				// client handler, so this naturally stops once the tag lands.
				if nowState.Player.FactionID != "" && nowState.Player.FactionTag == "" {
					if fiErr := client.FactionInfo(ctx); fiErr != nil {
						logger.Printf("warning: faction_info (tag refresh): %v", fiErr)
					}
				}

				tid := ""
				if p := activeTaskID.Load(); p != nil {
					tid = *p
				}
				act := ""
				if p := activity.Load(); p != nil {
					act = *p
				}
				status := buildStatus(nowState, standing, tid, act, drained.Load(), client.IsConnected(), time.Now())
				if sendErr := sendEnvelope(enc, control.TypeStatus, *agentID, status); sendErr != nil {
					logger.Printf("warning: send status: %v", sendErr)
				}

				ks := buildKnownState(nowState, nowTick)
				if saveErr := store.SaveKnownState(ks); saveErr != nil {
					logger.Printf("warning: save known state: %v", saveErr)
				}

				if paused.Load() {
					logger.Printf("heartbeat [paused] system=%s poi=%s credits=%.0f",
						nowState.CurrentSystem, nowState.CurrentPOI, nowState.Credits)
				} else {
					logger.Printf("heartbeat system=%s poi=%s credits=%.0f",
						nowState.CurrentSystem, nowState.CurrentPOI, nowState.Credits)
				}
			}
		}
	} else {
		// No socket: just run heartbeat loop logging state until signal.
		ticker := time.NewTicker(game.SleepTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				goto done
			case <-ticker.C:
				if nudgeReconnect(client) {
					logger.Printf("game connection down; requested reconnect")
				}
				nowState := client.GetState()
				ks := buildKnownState(nowState, int(nowState.CurrentTick))
				if saveErr := store.SaveKnownState(ks); saveErr != nil {
					logger.Printf("warning: save known state: %v", saveErr)
				}
				logger.Printf("heartbeat (no socket) system=%s poi=%s credits=%.0f",
					nowState.CurrentSystem, nowState.CurrentPOI, nowState.Credits)
			}
		}
	}

done:
	logger.Printf("shutdown complete")
}

// displaySystem returns a stable, human-facing label for the current system.
// State.CurrentSystem is overloaded: player-data responses write the system id while
// system-data responses write the display name, so the same system can surface either
// way depending on which command last refreshed state. Prefer SystemData.Name when it
// describes the current system; fall back to CurrentSystem so a stale SystemData (e.g.
// mid-travel) never mislabels the location.
func displaySystem(st *game.State) string {
	if st == nil {
		return ""
	}
	cur := st.CurrentSystem
	if name := st.System.Name; name != "" && (cur == "" || st.System.ID == cur || name == cur) {
		return name
	}
	return cur
}

// reconnectable is the subset of *game.Client the heartbeat loop needs to keep
// an idle worker's game connection alive.
type reconnectable interface {
	IsConnected() bool
	RequestReconnect() bool
}

// nudgeReconnect re-arms a dormant reconnection burst when the game connection
// is down. The auto-reconnect handler gives up after a short burst and then
// waits to be re-woken (originally by REPL input); a standing/idle worker never
// issues such a command, so without this it stays game-disconnected after a
// transient outage (e.g. the nightly router/DNS restart) while still
// heartbeating to the overmind. Called every heartbeat tick, it is a no-op when
// connected or when a burst is already in flight. Returns true when it actually
// started a fresh burst (so the caller can log the recovery attempt).
func nudgeReconnect(c reconnectable) bool {
	if c.IsConnected() {
		return false
	}
	return c.RequestReconnect()
}

// buildStatus constructs a control.Status heartbeat snapshot from game state.
// connected reports whether the game-server connection is currently live; when
// false the supervisor leaves the worker to the reconnect gate instead of
// restarting it (a restart's fresh login deepens a per-IP block).
func buildStatus(st *game.State, standing, taskID, activity string, drained, connected bool, now time.Time) control.Status {
	return control.Status{
		Disconnected: !connected,
		System: displaySystem(st),
		POI:    st.CurrentPOI,
		// Docked must reflect a real station/outpost dock, not merely "parked at
		// some POI": st.Doc is the server flag (true only at station/outpost,
		// false at gas clouds/suns/planets and during transit), so a hauler
		// passing through a station-less system no longer reports a phantom dock.
		Docked:           st.Doc && !st.Traveling,
		Hull:             st.Hull,
		MaxHull:          st.MaxHull,
		Fuel:             st.Fuel,
		MaxFuel:          st.MaxFuel,
		Credits:          st.Credits,
		CargoUsed:        st.Ship.CargoUsed,
		CargoCapacity:    st.Ship.CargoCapacity,
		StandingBehavior: standing,
		ActiveTaskID:     taskID,
		Activity:         activity,
		FactionID:        st.Player.FactionID,
		FactionTag:       st.Player.FactionTag,
		Drained:          drained,
		Timestamp:        now.Format(time.RFC3339Nano),
	}
}

// buildKnownState constructs a checkpoint.KnownState snapshot from game state.
func buildKnownState(st *game.State, tick int) checkpoint.KnownState {
	return checkpoint.KnownState{
		System:  st.CurrentSystem,
		POI:     st.CurrentPOI,
		Docked:  st.Doc && !st.Traveling, // authoritative server flag; see buildStatus
		Credits: st.Credits,
		Tick:    tick,
	}
}

// sendEnvelope wraps a payload in a control.Envelope and writes it.
func sendEnvelope(enc *control.Encoder, t control.Type, agentID string, payload any) error {
	env, err := control.NewEnvelope(t, agentID, payload)
	if err != nil {
		return fmt.Errorf("new envelope %s: %w", t, err)
	}
	if err := enc.Encode(env); err != nil {
		return fmt.Errorf("encode envelope %s: %w", t, err)
	}
	return nil
}
