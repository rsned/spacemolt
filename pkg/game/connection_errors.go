package game

import (
	"fmt"

	"github.com/coder/websocket"
)

// ConnectionClosed signals that the WebSocket received a close frame
// while a request was outstanding, and the close-code policy (see
// close_codes.go) was fail-fast rather than replay. Callers can
// inspect Code/Reason via errors.As.
type ConnectionClosed struct {
	Code   websocket.StatusCode
	Reason string
}

func (e *ConnectionClosed) Error() string {
	return fmt.Sprintf("websocket closed: code=%d reason=%q", int(e.Code), e.Reason)
}

// ConnectionLost signals that the WebSocket disconnected and the
// client failed to re-establish a working session (network down,
// auth failed during reconnect, etc.). The Cause is the last
// reconnect-attempt error.
type ConnectionLost struct {
	Cause error
}

func (e *ConnectionLost) Error() string {
	if e.Cause == nil {
		return "websocket connection lost"
	}
	return "websocket connection lost: " + e.Cause.Error()
}

func (e *ConnectionLost) Unwrap() error {
	return e.Cause
}
