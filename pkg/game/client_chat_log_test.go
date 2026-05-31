package game

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestHandleResponseChatMessageDoesNotDoubleLog guards against the game client
// re-logging chat content. The listen() loop already dumps every non-quiet
// frame (Response Type + full Response Payload) through debugLogger, so a
// second per-message "[CHAT] ..." line is pure duplication — in play_as it
// shows up alongside the user-facing rendered chat line.
func TestHandleResponseChatMessageDoesNotDoubleLog(t *testing.T) {
	var buf bytes.Buffer
	c := &Client{
		debugLogger:   log.New(&buf, "", 0),
		latestRawJSON: make(map[string][]byte),
	}

	resp := protocol.Response{
		Type: protocol.TypeChatMessage,
		Payload: map[string]any{
			"channel":   "system",
			"sender":    "[CUSTOMS] Federation Customs I",
			"sender_id": "17a08149befb15b51a1fcf8bca325c36",
			"content":   "naomi9 has departed the inspection zone.",
			"id":        "2a014bfddda0745733f20af8e8044876",
		},
	}
	c.handleResponse(resp)

	if strings.Contains(buf.String(), "[CHAT]") {
		t.Errorf("handleResponse emitted a redundant [CHAT] log line:\n%s", buf.String())
	}
}
