package game

import (
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
}

// The server's standalone auto-dock notification reuses the issuing command's
// request_id with type ok and payload {"type":"auto_dock"}. Treating it as the
// command's terminal frame is what made a REPL sell_wreck print "Automatically
// docked..." and orphan the real action_result (seen live 2026-08-30). It is
// an intermediate frame; only the notification FORM is skipped — a real
// terminal ok that merely carries the inline auto_docked flag must still
// terminate, or every auto-docking command would hang to timeout.
func TestTerminateOnActionOrOK_AutoDockNotificationIsIntermediate(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		done    bool
	}{
		{"auto_dock notification", map[string]any{"type": "auto_dock", "message": "Automatically docked"}, false},
		{"auto_undock notification", map[string]any{"type": "auto_undock"}, false},
		{"pending ack", map[string]any{"pending": true}, false},
		{"plain ok", map[string]any{"message": "done"}, true},
		{"terminal ok with inline auto_docked flag", map[string]any{"auto_docked": true, "action": "dock"}, true},
	}
	for _, tc := range cases {
		done, err := terminateOnActionOrOK(protocol.Response{Type: protocol.TypeOK, Payload: tc.payload})
		if err != nil {
			t.Errorf("%s: err = %v", tc.name, err)
		}
		if done != tc.done {
			t.Errorf("%s: done = %v, want %v", tc.name, done, tc.done)
		}
	}
}
