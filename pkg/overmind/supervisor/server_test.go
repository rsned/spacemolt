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

func TestAdminConnRoutedAndAcked(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")
	fleet := NewFleet()
	srv, err := NewServer(sock, fleet, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	var gotOp control.Type
	var gotID string
	srv.SetAdminHook(func(op control.Type, agentID string) control.AdminAck {
		gotOp, gotID = op, agentID
		return control.AdminAck{AgentID: agentID, Status: control.AckAccepted}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go srv.Serve(ctx) //nolint:errcheck

	conn, err := net.DialTimeout("unix", sock, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	enc := control.NewEncoder(conn)
	env, _ := control.NewEnvelope(control.TypeAdminRemove, "a1", control.AdminRequest{AgentID: "a1"})
	if err := enc.Encode(env); err != nil {
		t.Fatalf("encode: %v", err)
	}
	dec := control.NewDecoder(conn)
	reply, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if reply.Type != control.TypeAdminAck {
		t.Fatalf("reply type = %q, want admin_ack", reply.Type)
	}
	var ack control.AdminAck
	if err := reply.Into(&ack); err != nil {
		t.Fatalf("into: %v", err)
	}
	if ack.Status != control.AckAccepted || gotOp != control.TypeAdminRemove || gotID != "a1" {
		t.Fatalf("ack=%+v gotOp=%q gotID=%q", ack, gotOp, gotID)
	}
	// The admin conn must not have registered as a worker connection.
	if err := srv.Send("a1", control.Envelope{Type: control.TypeDrain}); err == nil {
		t.Fatal("admin conn registered in conns: Send should fail for a1")
	}
}
