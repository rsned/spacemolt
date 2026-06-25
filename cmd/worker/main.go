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
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/overmind/checkpoint"
	"github.com/rsned/spacemolt/pkg/overmind/control"
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
					logger.Printf("resumed")
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
			sched, schedErr := worker.LoadScheduler(filepath.Join("data", "agents", *agentID, "schedule.json"))
			if schedErr != nil {
				logger.Printf("warning: load scheduler: %v", schedErr)
			}
			var execMu sync.Mutex
			go func() {
				deps := worker.StandingDeps{
					Runner:    dispatch,
					Scheduler: sched,
					Client:    client,
					ExecMu:    &execMu,
					Paused:    paused.Load,
					Out:       os.Stdout,
					NowFn:     func() time.Time { return time.Now().UTC() },
					AgentID:   *agentID,
					NextTask: func() *worker.AssignedTask {
						t := pendingTask.Swap(nil)
						if t != nil {
							id := t.ID
							activeTaskID.Store(&id)
						}
						return t
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
				nowState := client.GetState()
				nowTick := int(nowState.CurrentTick)

				tid := ""
				if p := activeTaskID.Load(); p != nil {
					tid = *p
				}
				status := buildStatus(nowState, standing, tid, time.Now())
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

// buildStatus constructs a control.Status heartbeat snapshot from game state.
func buildStatus(st *game.State, standing, taskID string, now time.Time) control.Status {
	return control.Status{
		System:           st.CurrentSystem,
		POI:              st.CurrentPOI,
		Docked:           st.CurrentPOI != "" && !st.Traveling,
		Hull:             st.Hull,
		MaxHull:          st.MaxHull,
		Fuel:             st.Fuel,
		MaxFuel:          st.MaxFuel,
		Credits:          st.Credits,
		StandingBehavior: standing,
		ActiveTaskID:     taskID,
		Timestamp:        now.Format(time.RFC3339Nano),
	}
}

// buildKnownState constructs a checkpoint.KnownState snapshot from game state.
func buildKnownState(st *game.State, tick int) checkpoint.KnownState {
	return checkpoint.KnownState{
		System:  st.CurrentSystem,
		POI:     st.CurrentPOI,
		Docked:  st.CurrentPOI != "" && !st.Traveling,
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
