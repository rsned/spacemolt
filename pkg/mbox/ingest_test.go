package mbox

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestHandlePush(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	ing := NewIngester(s)

	msg := serverapi.ChatMessage{
		ID:           "push-001",
		Channel:      "local",
		SenderID:     "player-42",
		Sender:       "GunnyDraper",
		Content:      "Anyone selling tritanium?",
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
	}

	ing.HandlePush(msg)

	got, err := s.Get("push-001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("message not found after HandlePush")
	}
	if got.Source != "push" {
		t.Fatalf("source = %q, want push", got.Source)
	}
	if got.Sender != "GunnyDraper" {
		t.Fatalf("sender = %q, want GunnyDraper", got.Sender)
	}
}

func TestHandlePushDedupe(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	ing := NewIngester(s)

	msg := serverapi.ChatMessage{
		ID:           "dup-001",
		Channel:      "system",
		SenderID:     "p1",
		Sender:       "Alice",
		Content:      "hello",
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
	}

	ing.HandlePush(msg)
	ing.HandlePush(msg) // duplicate

	msgs, _ := s.List(Query{Channel: "system", Limit: 10})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}
