package game

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/internal/protocol"
)

// newSubmitTestClient returns a Client with stubbed connection so Submit can
// run without a real WebSocket. sendCh receives every Send call.
func newSubmitTestClient(t *testing.T) (*Client, chan protocol.Message) {
	t.Helper()
	c := newSubmitClientSkeleton()
	sendCh := make(chan protocol.Message, 16)
	c.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		sendCh <- msg
		return nil
	}
	return c, sendCh
}

// newSubmitClientSkeleton returns a minimal Client suitable for Submit unit
// tests — no real WebSocket, no listener goroutine. Mirrors the
// constructor's field init for the bits Submit/router touch.
func newSubmitClientSkeleton() *Client {
	c := &Client{
		debugLogger: log.New(io.Discard, "", 0),
	}
	c.router = newResponseRouter()
	c.inflight = newInflight(16)
	c.actionLocks = newActionLockMap()
	c.connected = true
	return c
}

func TestSubmit_AckOnly_QueryReturnsTerminal(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	sent := <-sendCh
	if sent.RequestID == "" {
		t.Fatal("sent message missing RequestID")
	}
	if h.ID() != sent.RequestID {
		t.Errorf("handle.ID() = %q, want %q", h.ID(), sent.RequestID)
	}

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"status": "ok"},
	})

	resp, err := h.Result(ctx)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if resp.RequestID != sent.RequestID {
		t.Errorf("resp.RequestID = %q, want %q", resp.RequestID, sent.RequestID)
	}
}

func TestSubmit_Mutation_PendingThenTerminal(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx,
		protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go func() {
		c.router.dispatch(protocol.Response{
			Type: protocol.TypeOK, RequestID: sent.RequestID,
			Payload: map[string]any{"pending": true, "command": "mine"},
		})
		time.Sleep(5 * time.Millisecond)
		c.router.dispatch(protocol.Response{
			Type: protocol.TypeActionResult, RequestID: sent.RequestID,
			Payload: map[string]any{"command": "mine", "yield": 3},
		})
	}()

	ack, err := h.Ack(ctx)
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if p, _ := ack.Payload["pending"].(bool); !p {
		t.Errorf("expected pending=true on ack")
	}

	resp, err := h.Result(ctx)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if resp.Type != protocol.TypeActionResult {
		t.Errorf("Result type = %q, want action_result", resp.Type)
	}
}

func TestSubmit_PendingAckExtendsDeadline(t *testing.T) {
	// Regression: mutations execute serially server-side, so a mutation sent
	// while another is in flight (e.g. dock during travel) is queued and its
	// terminal frame lands many ticks later. The pending ack must extend the
	// waiter's deadline past the short initial timeout, or the (request_id-
	// tagged) terminal arrives after the waiter is gone and is orphaned —
	// the reported "timeout waiting for dock" + "orphan response" symptom.
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	_, terminate := dockTransitionMatchers("dock", protocol.TypeDocked)
	h, err := c.Submit(ctx, protocol.Message{Type: "dock"},
		WithTerminator(terminate), WithTimeout(60*time.Millisecond))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	// Immediate pending ack: the server queued the dock behind an in-flight
	// action. This must reset the 60ms initial timeout to the long deferred
	// budget so the terminal — arriving much later — still resolves.
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"pending": true, "command": "dock"},
	})

	// Terminal arrives well after the ORIGINAL 60ms timeout would have fired.
	go func() {
		time.Sleep(150 * time.Millisecond)
		c.router.dispatch(protocol.Response{
			Type: protocol.TypeOK, RequestID: sent.RequestID,
			Payload: map[string]any{"action": "dock", "base": "Grand Exchange Station"},
		})
	}()

	resp, err := h.Result(ctx)
	if err != nil {
		t.Fatalf("Result: %v (pending ack should have extended the deadline)", err)
	}
	if act, _ := resp.Payload["action"].(string); act != "dock" {
		t.Errorf("Result action = %q, want dock", act)
	}
	if got := c.router.orphans().Count(); got != 0 {
		t.Errorf("orphan count = %d, want 0 (terminal must reach the live waiter)", got)
	}
}

func TestSubmit_ServerError(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "buy"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeError, RequestID: sent.RequestID,
		Payload: map[string]any{"code": "insufficient_credits", "message": "not enough credits"},
	})

	_, err = h.Result(ctx)
	if err == nil {
		t.Fatal("expected ServerError, got nil")
	}
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err type = %T, want *ServerError", err)
	}
	if se.Code != "insufficient_credits" {
		t.Errorf("se.Code = %q, want insufficient_credits", se.Code)
	}
}

func TestSubmit_ResultCalledTwiceReturnsSameValue(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"status": "ok"},
	})

	r1, err1 := h.Result(ctx)
	r2, err2 := h.Result(ctx)
	if err1 != nil || err2 != nil {
		t.Fatalf("err1=%v err2=%v", err1, err2)
	}
	if r1.RequestID != r2.RequestID {
		t.Errorf("Result/Result mismatch: %q vs %q", r1.RequestID, r2.RequestID)
	}
}

func TestSubmit_CtxCancelReleasesResources(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())

	h, err := c.Submit(ctx, protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction),
		WithTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-sendCh

	cancel()
	_, err = h.Result(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}

	// Resources released: cap should drain to 0.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.inflight.InFlight() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("inflight slot not released, count=%d", c.inflight.InFlight())
}

func TestSubmit_ResultConcurrentCallersAllResolve(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"status": "ok"},
	})

	var wg sync.WaitGroup
	var results [4]protocol.Response
	var errs [4]error
	for i := range 4 {
		wg.Go(func() {
			callCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			results[i], errs[i] = h.Result(callCtx)
		})
	}
	wg.Wait()

	for i := range 4 {
		if errs[i] != nil {
			t.Errorf("Result[%d]: %v", i, errs[i])
		}
		if results[i].RequestID != sent.RequestID {
			t.Errorf("Result[%d].RequestID = %q, want %q", i, results[i].RequestID, sent.RequestID)
		}
	}
}

func TestSubmit_UsesUUIDv7(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	_, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh
	// UUIDv7 string is 36 chars with version "7" at position 14.
	if len(sent.RequestID) != 36 || sent.RequestID[14] != '7' {
		t.Errorf("RequestID = %q, want UUIDv7 (36 chars, version 7)", sent.RequestID)
	}
}

func TestReplay_OnNormalClose_FreshUUIDAndDeliver(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	first := <-sendCh
	origID := first.RequestID

	// Simulate a graceful close: replayPending stages the message but
	// does not send. drainPendingReplay (normally called by Reconnect
	// after re-login) is invoked explicitly here to mimic the full
	// close-then-reconnect cycle.
	c.replayPending(&websocket.CloseError{Code: websocket.StatusNormalClosure, Reason: "max session age"})
	c.drainPendingReplay(ctx)

	second := <-sendCh
	if second.RequestID == "" || second.RequestID == origID {
		t.Errorf("replayed RequestID = %q, want fresh non-empty (orig=%q)", second.RequestID, origID)
	}
	if got := h.ID(); got != second.RequestID {
		t.Errorf("handle.ID() = %q, want %q (post-replay)", got, second.RequestID)
	}

	// Deliver terminal under the new ID; Result must succeed.
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: second.RequestID,
		Payload: map[string]any{"command": "mine"},
	})
	if _, err := h.Result(ctx); err != nil {
		t.Errorf("Result after replay: %v", err)
	}
}

func TestReplay_LateOriginalResponseIsOrphan(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	first := <-sendCh
	origID := first.RequestID

	c.replayPending(&websocket.CloseError{Code: websocket.StatusNormalClosure, Reason: "rolling restart"})
	c.drainPendingReplay(ctx)
	<-sendCh // consume replayed send

	// Late response under the old ID must be orphaned, not delivered.
	before := c.router.orphans().Count()
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: origID,
		Payload: map[string]any{"command": "mine"},
	})
	if got := c.router.orphans().Count(); got != before+1 {
		t.Errorf("orphan count delta = %d, want 1", got-before)
	}

	// Cleanup: deliver under the new id so the handle resolves.
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: h.ID(),
		Payload: map[string]any{"command": "mine"},
	})
	if _, err := h.Result(ctx); err != nil {
		t.Errorf("Result: %v", err)
	}
}

func TestAwait_FillsResultSink(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "get_status", "credits": 42.0},
	})

	resp, err := c.await(ctx, h)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if resp.RequestID != sent.RequestID {
		t.Errorf("returned resp.RequestID = %q, want %q", resp.RequestID, sent.RequestID)
	}
	if sink.RequestID != sent.RequestID {
		t.Errorf("sink.RequestID = %q, want %q", sink.RequestID, sent.RequestID)
	}
	if got, _ := sink.Payload["credits"].(float64); got != 42.0 {
		t.Errorf("sink.Payload[credits] = %v, want 42", sink.Payload["credits"])
	}
}

func TestAwait_NoSinkIsNoop(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background() // no sink attached

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "get_status"},
	})

	if _, err := c.await(ctx, h); err != nil {
		t.Fatalf("await with no sink must not error: %v", err)
	}
}

// TestAwait_CommandMethodFillsSink proves the mechanical sweep wired await into
// a real command method end to end: GetDrones, driven through the Submit test
// harness, deposits its correlated terminal into the ctx sink.
func TestAwait_CommandMethodFillsSink(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	errCh := make(chan error, 1)
	go func() { errCh <- c.GetDrones(ctx) }()

	sent := <-sendCh
	if sent.Type != "get_drones" {
		t.Fatalf("sent.Type = %q, want get_drones", sent.Type)
	}
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "get_drones", "drones": []any{}},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("GetDrones: %v", err)
	}
	if sink.RequestID != sent.RequestID {
		t.Errorf("sink.RequestID = %q, want %q (sweep did not wire await into GetDrones)",
			sink.RequestID, sent.RequestID)
	}
}

// TestConvertedMutation_Correlates proves a converted fire-and-forget mutation
// now flows through Submit (stamps a request_id) and awaits a terminal.
func TestConvertedMutation_Correlates(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	errCh := make(chan error, 1)
	go func() { errCh <- c.AbandonMission(ctx, "m-1") }()

	sent := <-sendCh
	if sent.Type != "abandon_mission" {
		t.Fatalf("sent.Type = %q, want abandon_mission", sent.Type)
	}
	if sent.RequestID == "" {
		t.Fatal("converted mutation did not stamp a request_id (still on c.send)")
	}
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"command": "abandon_mission", "result": map[string]any{"abandoned": true}},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("AbandonMission: %v", err)
	}
	if sink.RequestID != sent.RequestID {
		t.Errorf("sink.RequestID = %q, want %q", sink.RequestID, sent.RequestID)
	}
}

// TestConvertedQuery_Correlates proves a converted query flows through Submit
// with WithAckOnly (first response terminal) and fills the sink.
func TestConvertedQuery_Correlates(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	errCh := make(chan error, 1)
	go func() { errCh <- c.GetVersion(ctx) }()

	sent := <-sendCh
	if sent.Type != "get_version" {
		t.Fatalf("sent.Type = %q, want get_version", sent.Type)
	}
	if sent.RequestID == "" {
		t.Fatal("converted query did not stamp a request_id (still on c.send)")
	}
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "get_version", "version": "v0.294.0"},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v, _ := sink.Payload["version"].(string); v != "v0.294.0" {
		t.Errorf("sink.Payload[version] = %v, want v0.294.0", sink.Payload["version"])
	}
}

func TestAwait_FillsResultSink_OnServerError(t *testing.T) {
	// await must populate the sink even when the terminal frame is an error,
	// so that chooseErrorJSON can display the request_id-correlated error
	// payload rather than falling back to the racy _last_error slot.
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	h, err := c.Submit(ctx, protocol.Message{Type: "buy"}, WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type:      protocol.TypeError,
		RequestID: sent.RequestID,
		Payload:   map[string]any{"code": "bad", "message": "nope"},
	})

	_, awaitErr := c.await(ctx, h)
	if awaitErr == nil {
		t.Fatal("await should return non-nil error on server error frame")
	}

	if sink.Type != protocol.TypeError {
		t.Errorf("sink.Type = %q, want %q", sink.Type, protocol.TypeError)
	}
	if len(sink.Payload) == 0 {
		t.Error("sink.Payload is empty, want populated error payload")
	}
	if code, _ := sink.Payload["code"].(string); code != "bad" {
		t.Errorf("sink.Payload[code] = %q, want \"bad\"", code)
	}
}

func TestReplay_DoesNotSendBeforeReconnect(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-sendCh // consume first send
	origID := h.ID()

	// replayPending alone must not send. It only stages.
	c.replayPending(&websocket.CloseError{Code: websocket.StatusNormalClosure, Reason: "max session age"})

	select {
	case msg := <-sendCh:
		t.Fatalf("replayPending sent prematurely (id=%s)", msg.RequestID)
	case <-time.After(50 * time.Millisecond):
		// expected: nothing sent
	}

	// Handle should already be rekeyed (so a late response under origID
	// becomes an orphan).
	if h.ID() == origID {
		t.Errorf("handle.ID() not updated after replayPending")
	}

	// Now drain.
	c.drainPendingReplay(ctx)
	second := <-sendCh
	if second.RequestID != h.ID() {
		t.Errorf("drained send RequestID = %q, want %q", second.RequestID, h.ID())
	}

	// Cleanup: resolve handle.
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: second.RequestID,
		Payload: map[string]any{"command": "mine"},
	})
	if _, err := h.Result(ctx); err != nil {
		t.Errorf("Result: %v", err)
	}
}
