package supervisor

import (
	"context"
	"io"
	"log"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

func TestServerReceivesHelloStatusAndSends(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "om.sock")
	fleet := NewFleet()
	srv, err := NewServer(sock, fleet, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	gotEvent := make(chan control.Event, 1)
	srv.SetEventHook(func(_ string, ev control.Event) { gotEvent <- ev })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	// Fake worker dials in.
	conn, err := dialRetry(t, sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	enc := control.NewEncoder(conn)
	dec := control.NewDecoder(conn)

	hello, _ := control.NewEnvelope(control.TypeHello, "a",
		control.Hello{AgentID: "a", Role: "resident", Station: "S1"})
	_ = enc.Encode(hello)
	st, _ := control.NewEnvelope(control.TypeStatus, "a", control.Status{System: "SOL"})
	_ = enc.Encode(st)
	ev, _ := control.NewEnvelope(control.TypeEvent, "a", control.Event{Kind: "combat"})
	_ = enc.Encode(ev)

	select {
	case got := <-gotEvent:
		if got.Kind != "combat" {
			t.Fatalf("event hook got %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event hook never fired")
	}

	// Fleet should now know agent "a".
	waitFor(t, func() bool {
		snap := fleet.Snapshot()
		return len(snap) == 1 && snap[0].LastStatus.System == "SOL"
	})

	// Overmind -> worker send is received by the fake worker.
	abort, _ := control.NewEnvelope(control.TypeAbort, "a", control.Abort{Reason: "test"})
	if err := srv.Send("a", abort); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := dec.Decode()
	if err != nil || got.Type != control.TypeAbort {
		t.Fatalf("worker did not receive abort: %+v err=%v", got, err)
	}
}

func dialRetry(t *testing.T, sock string) (net.Conn, error) {
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

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 100 {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition never met")
}
