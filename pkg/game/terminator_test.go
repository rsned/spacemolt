package game

import (
	"errors"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestTerminateOnAction_Result(t *testing.T) {
	done, err := terminateOnAction(protocol.Response{Type: protocol.TypeActionResult})
	if !done {
		t.Error("expected action_result to terminate")
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestTerminateOnAction_Error(t *testing.T) {
	resp := protocol.Response{
		Type:    protocol.TypeActionError,
		Payload: map[string]any{"message": "boom"},
	}
	done, err := terminateOnAction(resp)
	if !done {
		t.Error("expected action_error to terminate")
	}
	if err == nil {
		t.Error("expected non-nil error")
	}
}

func TestTerminateOnAction_Ok(t *testing.T) {
	// ok with pending:true is intermediate, NOT terminal
	resp := protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"pending": true},
	}
	done, err := terminateOnAction(resp)
	if done {
		t.Error("expected pending ok not to terminate")
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestTerminateOnTypes(t *testing.T) {
	term := terminateOnTypes(protocol.TypePOIArrival, protocol.TypeActionError)
	if done, _ := term(protocol.Response{Type: protocol.TypePOIArrival}); !done {
		t.Error("expected poi_arrival to terminate")
	}
	if done, _ := term(protocol.Response{Type: protocol.TypeTick}); done {
		t.Error("expected tick not to terminate")
	}
	// ActionError variant returns done=true with non-nil error.
	done, err := term(protocol.Response{
		Type:    protocol.TypeActionError,
		Payload: map[string]any{"message": "fail"},
	})
	if !done || err == nil {
		t.Errorf("expected action_error to terminate with error, got done=%v err=%v", done, err)
	}
	_ = errors.New
}
