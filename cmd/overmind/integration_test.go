//go:build integration

package main

// A focused end-to-end check of the control plane using an in-process fake
// worker (no game server, no subprocess). Run: go test -tags=integration ./cmd/overmind/

import (
	"context"
	"io"
	"log"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// TestControlPlaneEndToEnd exercises the full control-plane path:
//  1. Supervisor server starts on a temp Unix socket.
//  2. An in-process fake worker dials, sends Hello then Status.
//  3. Fleet snapshot reflects the agent with the reported status.
//  4. Overmind sends an Abort; fake worker receives it.
func TestControlPlaneEndToEnd(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "om-e2e.sock")
	fleet := supervisor.NewFleet()
	srv, err := supervisor.NewServer(sock, fleet, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	// ── Fake worker dials in ──────────────────────────────────────────────────
	conn, err := dialRetryInteg(t, sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	enc := control.NewEncoder(conn)
	dec := control.NewDecoder(conn)

	// Step 2: Send Hello then Status.
	hello, err := control.NewEnvelope(control.TypeHello, "worker-1",
		control.Hello{AgentID: "worker-1", Role: "miner", Station: "S7"})
	if err != nil {
		t.Fatalf("build hello: %v", err)
	}
	if err := enc.Encode(hello); err != nil {
		t.Fatalf("encode hello: %v", err)
	}

	status, err := control.NewEnvelope(control.TypeStatus, "worker-1",
		control.Status{System: "Vega Prime", Hull: 80, MaxHull: 100})
	if err != nil {
		t.Fatalf("build status: %v", err)
	}
	if err := enc.Encode(status); err != nil {
		t.Fatalf("encode status: %v", err)
	}

	// Step 3: Wait for fleet snapshot to reflect the agent.
	waitForInteg(t, func() bool {
		snap := fleet.Snapshot()
		return len(snap) == 1 && snap[0].LastStatus.System == "Vega Prime"
	})

	// Step 4: Overmind sends Abort; fake worker must receive it.
	abort, err := control.NewEnvelope(control.TypeAbort, "worker-1",
		control.Abort{Reason: "integration-test"})
	if err != nil {
		t.Fatalf("build abort: %v", err)
	}
	if err := srv.Send("worker-1", abort); err != nil {
		t.Fatalf("srv.Send abort: %v", err)
	}

	got, err := dec.Decode()
	if err != nil {
		t.Fatalf("worker decode: %v", err)
	}
	if got.Type != control.TypeAbort {
		t.Fatalf("expected TypeAbort, got %q", got.Type)
	}
}

// dialRetryInteg polls the Unix socket until the server is ready (up to 1 s).
// The 20 ms poll interval is test scaffolding; it is exempt from the
// pkg/game Sleep-constant rule.
func dialRetryInteg(t *testing.T, sock string) (net.Conn, error) {
	t.Helper()
	var lastErr error
	for range 50 {
		c, err := net.Dial("unix", sock)
		if err == nil {
			return c, nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	return nil, lastErr
}

// waitForInteg polls cond until it returns true (up to 2 s).
// The 20 ms poll interval is test scaffolding; it is exempt from the
// pkg/game Sleep-constant rule.
func waitForInteg(t *testing.T, cond func() bool) {
	t.Helper()
	for range 100 {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition never met within 2 s")
}
