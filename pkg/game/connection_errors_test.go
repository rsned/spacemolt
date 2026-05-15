package game

import (
	"errors"
	"testing"

	"github.com/coder/websocket"
)

func TestConnectionClosed_Error(t *testing.T) {
	e := &ConnectionClosed{Code: websocket.StatusGoingAway, Reason: "rolling restart"}
	got := e.Error()
	want := "websocket closed: code=1001 reason=\"rolling restart\""
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestConnectionLost_Error_Unwrap(t *testing.T) {
	root := errors.New("dial tcp: connection refused")
	e := &ConnectionLost{Cause: root}
	if !errors.Is(e, root) {
		t.Errorf("errors.Is should match wrapped cause")
	}
	if e.Error() == "" {
		t.Errorf("Error() must not be empty")
	}
}

func TestConnectionLost_NilCause(t *testing.T) {
	e := &ConnectionLost{}
	if e.Error() == "" {
		t.Errorf("Error() must not be empty when Cause is nil")
	}
}
