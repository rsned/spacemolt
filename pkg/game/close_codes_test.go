package game

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/internal/protocol"
)

func TestClosePolicy_Known1000_DocumentedReason(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusNormalClosure, "max session age")
	if !known {
		t.Fatal("expected known=true for documented close=1000")
	}
	if p.action != closeReplay {
		t.Errorf("action = %v, want closeReplay", p.action)
	}
}

func TestClosePolicy_Known1000_UndocumentedReason(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusNormalClosure, "region migration")
	if known {
		t.Fatal("expected known=false for undocumented reason on 1000")
	}
	// Even unknown reason on 1000 still defaults to replay (per spec).
	if p.action != closeReplay {
		t.Errorf("action = %v, want closeReplay fallback", p.action)
	}
}

func TestClosePolicy_KnownFailFast(t *testing.T) {
	cases := []websocket.StatusCode{
		websocket.StatusGoingAway,
		websocket.StatusAbnormalClosure,
		websocket.StatusInternalError,
	}
	for _, code := range cases {
		p, known := lookupClosePolicy(code, "")
		if !known {
			t.Errorf("code=%d should be known", code)
		}
		if p.action != closeFailFast {
			t.Errorf("code=%d action = %v, want closeFailFast", code, p.action)
		}
	}
}

func TestClosePolicy_Unknown(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusCode(4999), "wat")
	if known {
		t.Fatal("expected known=false for code=4999")
	}
	if p.action != closeFailFast {
		t.Errorf("unknown code action = %v, want closeFailFast", p.action)
	}
}

func TestHandleClose_FailFastPropagates(t *testing.T) {
	c := newSubmitClientSkeleton()
	// Register a fake pending subscription.
	ch := make(chan protocol.Response, 1)
	c.router.registerByID("req-fast", ch, terminateOnAction)

	closeErr := &websocket.CloseError{Code: websocket.StatusGoingAway, Reason: "test"}
	c.handleClose(closeErr)

	select {
	case resp := <-ch:
		if resp.Type != protocol.TypeError {
			t.Errorf("resp.Type = %q, want error", resp.Type)
		}
		if code, _ := resp.Payload["code"].(string); code != "connection_closed" {
			t.Errorf("code = %q, want connection_closed", code)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("pending request not failed on close")
	}
}

func TestClosePolicy_4001IsDisplaced(t *testing.T) {
	p, known := lookupClosePolicy(CloseSessionReplaced, "")
	if !known || p.action != closeDisplaced {
		t.Errorf("got action=%v known=%v, want closeDisplaced/true", p.action, known)
	}
}

func TestClosePolicy_4002IsFailFast(t *testing.T) {
	p, known := lookupClosePolicy(CloseAuthTimeout, "")
	if !known || p.action != closeFailFast {
		t.Errorf("got action=%v known=%v, want closeFailFast/true", p.action, known)
	}
}

func TestClosePolicy_4003IsRateLimited(t *testing.T) {
	p, known := lookupClosePolicy(CloseConnectionRateLimited, "")
	if !known || p.action != closeRateLimited {
		t.Errorf("got action=%v known=%v, want closeRateLimited/true", p.action, known)
	}
}

func TestCloseRetryAfter(t *testing.T) {
	tests := []struct {
		reason string
		want   time.Duration
		ok     bool
	}{
		{"rate_limited retry_after=30", 30 * time.Second, true},
		{"retry_after=5", 5 * time.Second, true},
		{"rate_limited", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := closeRetryAfter(tt.reason)
		if got != tt.want || ok != tt.ok {
			t.Errorf("closeRetryAfter(%q) = %v,%v want %v,%v", tt.reason, got, ok, tt.want, tt.ok)
		}
	}
}

func newGatedCloseClient(t *testing.T) (*Client, string, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "gate")
	c := newSubmitClientSkeleton()
	c.gate = newTestGate(t, path, testGateCfg, clk)
	return c, path, clk
}

func TestHandleClose_4003RecordsBlockOnGate(t *testing.T) {
	c, path, clk := newGatedCloseClient(t)
	c.handleClose(&websocket.CloseError{Code: CloseConnectionRateLimited, Reason: "rate_limited retry_after=30"})
	st := readGateFile(t, path)
	if want := clk.t.Add(30 * time.Second).UnixNano(); st.BlockedUntilNanos != want {
		t.Errorf("blocked_until = %d, want %d", st.BlockedUntilNanos, want)
	}
	if st.BlockSource != "close4003" {
		t.Errorf("block_source = %q, want close4003", st.BlockSource)
	}
}

func TestHandleClose_4003NoHintUsesDefaultBlock(t *testing.T) {
	c, path, clk := newGatedCloseClient(t)
	c.handleClose(&websocket.CloseError{Code: CloseConnectionRateLimited})
	st := readGateFile(t, path)
	if want := clk.t.Add(ConnectBareBlock).UnixNano(); st.BlockedUntilNanos != want {
		t.Errorf("blocked_until = %d, want %d", st.BlockedUntilNanos, want)
	}
}

func TestHandleClose_4001SetsDisplaced(t *testing.T) {
	c := newSubmitClientSkeleton()
	ch := make(chan protocol.Response, 1)
	c.router.registerByID("req-disp", ch, terminateOnAction)
	c.handleClose(&websocket.CloseError{Code: CloseSessionReplaced, Reason: "session_replaced"})
	if !c.Displaced() {
		t.Error("Displaced() = false, want true")
	}
	select {
	case resp := <-ch:
		if code, _ := resp.Payload["code"].(string); code != "connection_closed" {
			t.Errorf("code = %q, want connection_closed", code)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("pending request not failed on 4001")
	}
	c.clearDisplaced()
	if c.Displaced() {
		t.Error("Displaced() = true after clearDisplaced")
	}
}

func TestParseErrorState_IPTimedOutRecordsBlock(t *testing.T) {
	c, path, clk := newGatedCloseClient(t)
	c.parseErrorState(map[string]any{"code": "ip_timed_out", "message": "too many requests, try again in 120 seconds"})
	st := readGateFile(t, path)
	if min := clk.t.Add(120 * time.Second).UnixNano(); st.BlockedUntilNanos < min {
		t.Errorf("blocked_until = %d, want >= %d", st.BlockedUntilNanos, min)
	}
	if st.BlockSource != "ip_timed_out" {
		t.Errorf("block_source = %q, want ip_timed_out", st.BlockSource)
	}
}
