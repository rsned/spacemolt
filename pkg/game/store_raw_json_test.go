package game

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStoreRawJSON_LastErrorUpdatedByBothErrorTypes verifies that a plain
// `error` frame updates the _last_error slot, not just `action_error`.
//
// Regression: a deferred mine failure ("Resources depleted") arrives as a
// TypeError frame. The TypeError branch used to return without touching
// _last_error, so the slot still held the previous command's TypeActionError
// (e.g. a scan "invalid_target") and the REPL printed that stale error.
func TestStoreRawJSON_LastErrorUpdatedByBothErrorTypes(t *testing.T) {
	c := &Client{latestRawJSON: make(map[string][]byte)}

	// A prior scan fails as an action_error and lands in _last_error.
	c.storeRawJSON(protocol.Response{
		Type:    protocol.TypeActionError,
		Payload: map[string]any{"command": "scan", "code": "invalid_target"},
	})
	if got := string(c.GetRawJSON("_last_error")); !strings.Contains(got, "scan") {
		t.Fatalf("_last_error after scan action_error = %q, want it to contain the scan error", got)
	}

	// A later mine fails as a plain error frame. _last_error must now reflect
	// the mine error, not the stale scan error.
	c.storeRawJSON(protocol.Response{
		Type:    protocol.TypeError,
		Payload: map[string]any{"command": "mine", "message": "Resources depleted"},
	})
	got := string(c.GetRawJSON("_last_error"))
	if strings.Contains(got, "scan") {
		t.Errorf("_last_error still holds the stale scan error: %q", got)
	}
	if !strings.Contains(got, "Resources depleted") {
		t.Errorf("_last_error after mine error = %q, want it to contain the mine error", got)
	}
}
