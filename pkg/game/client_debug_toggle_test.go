package game

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestSetDebugLogging_RoundTrip verifies that debug logging can be toggled off
// and back on at runtime: disabling discards output, and re-enabling restores
// the logger's original writer (the one NewClient was constructed with).
func TestSetDebugLogging_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	client := NewClient("wss://test.example.com", "u", "p", logger)

	// Disable: subsequent debug output is discarded.
	client.SetDebugLogging(false)
	client.debugLogger.Printf("hidden-line")
	if buf.Len() != 0 {
		t.Fatalf("expected no output while disabled, got %q", buf.String())
	}

	// Re-enable: output flows to the original writer again.
	client.SetDebugLogging(true)
	client.debugLogger.Printf("visible-line")
	if got := buf.String(); !strings.Contains(got, "visible-line") {
		t.Fatalf("expected restored output to contain %q, got %q", "visible-line", got)
	}
	if strings.Contains(buf.String(), "hidden-line") {
		t.Fatalf("disabled line should never have been written, got %q", buf.String())
	}
}
