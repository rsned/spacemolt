package game

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// The v1 socket authenticates a Clerk ws-token with its own frame type; the
// password-shaped login frame would be rejected.
func TestLoginWithToken_SendsLoginTokenFrame(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)

	errCh := make(chan error, 1)
	go func() { errCh <- c.LoginWithToken(context.Background(), "tok") }()

	var sent protocol.Message
	select {
	case sent = <-sendCh:
	case <-time.After(2 * time.Second):
		t.Fatal("LoginWithToken sent nothing")
	}
	if sent.Type != "login_token" {
		t.Errorf("Type = %q, want login_token", sent.Type)
	}
	if got, _ := sent.Payload["token"].(string); got != "tok" {
		t.Errorf("Payload[token] = %q, want tok", got)
	}
	if _, ok := sent.Payload["password"]; ok {
		t.Error("token login frame must not carry a password")
	}

	c.router.dispatch(protocol.Response{Type: protocol.TypeLoggedIn, RequestID: sent.RequestID})
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("LoginWithToken: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("LoginWithToken did not resolve on logged_in")
	}
}

// details.retry_after is the server's stated wait; dropping it forces the gate
// back onto a guessed default.
func TestServerError_RetryAfterPopulated(t *testing.T) {
	err := serverErrorFromPayload(map[string]any{
		"code":    "rate_limited",
		"message": "too many logins",
		"details": map[string]any{"retry_after": float64(9)},
	})
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T, want *ServerError", err)
	}
	if se.RetryAfter != 9*time.Second {
		t.Errorf("RetryAfter = %v, want 9s", se.RetryAfter)
	}
}

// Connect used to retry a 429 internally with exponential backoff, burning
// several dials per gated slot. It now makes one dial and hands the server's
// stated wait back to the gate.
func TestConnect_429ReturnsWithoutRetry(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := NewClient("ws"+strings.TrimPrefix(srv.URL, "http"), "u", "p", nil)
	c.SetDebugLogging(false)

	start := time.Now()
	err := c.Connect(context.Background())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Connect succeeded against a 429 server")
	}
	if elapsed >= time.Second {
		t.Errorf("Connect took %v; want < 1s (no internal retry)", elapsed)
	}
	if !strings.Contains(err.Error(), "retry-after: 30") {
		t.Errorf("err = %v, want it to carry retry-after: 30", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server saw %d dials, want 1", n)
	}
}
