package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/mbox"
)

func TestBaseAgentMboxHelpers(t *testing.T) {
	dir := t.TempDir()
	store, err := mbox.Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open mbox: %v", err)
	}
	defer func() { _ = store.Close() }()

	agent := NewBaseAgent("test-1", Personality{Name: "Test", Role: "miner"}, nil, nil)
	agent.SetMbox(store)

	now := time.Now().UTC()
	if _, err := store.Ingest(mbox.Message{ID: "m1", Channel: "local", SenderID: "npc-station", Sender: "StationKeeper",
		Content: "Station Aldrin needs iron_ore", TimestampUTC: now, Source: "push"}); err != nil {
		t.Fatalf("Ingest m1: %v", err)
	}
	if _, err := store.Ingest(mbox.Message{ID: "m2", Channel: "faction", SenderID: "player-42", Sender: "GunnyDraper",
		Content: "Anyone near Kepler?", TimestampUTC: now.Add(time.Second), Source: "push"}); err != nil {
		t.Fatalf("Ingest m2: %v", err)
	}
	if _, err := store.Ingest(mbox.Message{ID: "m3", Channel: "local", SenderID: "npc-station", Sender: "StationKeeper",
		Content: "Station Hoffman needs uranium", TimestampUTC: now.Add(2 * time.Second), Source: "push"}); err != nil {
		t.Fatalf("Ingest m3: %v", err)
	}

	msgs := agent.RecentFromSender("npc-station", 5)
	if len(msgs) != 2 {
		t.Fatalf("RecentFromSender: got %d, want 2", len(msgs))
	}

	msgs = agent.UnreadMatching([]string{"iron_ore"})
	if len(msgs) != 1 || msgs[0].ID != "m1" {
		t.Fatalf("UnreadMatching iron_ore: got %v", msgs)
	}

	agent.MarkProcessed("m1", "m2")
	msgs = agent.UnreadMatching([]string{"iron_ore"})
	if len(msgs) != 0 {
		t.Fatal("expected no unread after MarkProcessed")
	}
}

func TestBaseAgentMboxNilStore(t *testing.T) {
	agent := NewBaseAgent("test-2", Personality{Name: "Test", Role: "miner"}, nil, nil)

	// All helpers should be nil-safe when mbox is not set
	msgs := agent.RecentFromSender("npc-station", 5)
	if msgs != nil {
		t.Fatal("expected nil from RecentFromSender with no mbox")
	}
	msgs = agent.UnreadMatching([]string{"test"})
	if msgs != nil {
		t.Fatal("expected nil from UnreadMatching with no mbox")
	}
	agent.MarkProcessed("m1") // should not panic
}
