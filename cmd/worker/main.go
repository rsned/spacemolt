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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/overmind/checkpoint"
	"github.com/rsned/spacemolt/pkg/overmind/control"
)

func main() {
	agentID := flag.String("agent", "", "Agent ID (required, e.g. miner-1)")
	role := flag.String("role", "idle", "Worker role (e.g. miner, trader)")
	station := flag.String("station", "", "Home station POI (optional)")
	socketPath := flag.String("socket", "", "Unix socket path to overmind control channel")
	dbPath := flag.String("db-path", "", "Path to checkpoint DB (default: data/agents/<agent>/checkpoint.db)")
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
	activeTaskID := ""
	if hasIntent {
		standing = savedIntent.StandingBehavior
		activeTaskID = savedIntent.ActiveTaskID
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

				status := buildStatus(nowState, standing, activeTaskID, time.Now())
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
