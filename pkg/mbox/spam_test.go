package mbox

import (
	"testing"
	"time"
)

// ingestN is a small helper that ingests a message with the given id/sender.
func ingestSpamTestMsg(t *testing.T, s *Store, id, senderID, sender string) {
	t.Helper()
	_, err := s.Ingest(Message{
		ID:           id,
		Channel:      "local",
		SenderID:     senderID,
		Sender:       sender,
		Content:      "noise",
		TimestampUTC: time.Now().UTC().Add(-time.Minute),
		Source:       "push",
	})
	if err != nil {
		t.Fatalf("Ingest %s: %v", id, err)
	}
}

func TestMarkSpamBySenderFlagsExistingAndExcludesFromList(t *testing.T) {
	s := newTestStore(t)
	ingestSpamTestMsg(t, s, "spam-1", "noisy-id", "storgio17")
	ingestSpamTestMsg(t, s, "spam-2", "noisy-id", "storgio17")
	ingestSpamTestMsg(t, s, "keep-1", "friend-id", "Buddy27")

	n, err := s.MarkSpamBySender("noisy-id")
	if err != nil {
		t.Fatalf("MarkSpamBySender: %v", err)
	}
	if n != 2 {
		t.Fatalf("MarkSpamBySender flagged %d, want 2", n)
	}

	// Default List excludes spam.
	got, err := s.List(Query{Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, m := range got {
		if m.ID == "spam-1" || m.ID == "spam-2" {
			t.Fatalf("List returned spam message %s", m.ID)
		}
	}
	if len(got) != 1 || got[0].ID != "keep-1" {
		t.Fatalf("List = %v, want only keep-1", ids(got))
	}

	// SpamOnly query returns the flagged messages.
	spam, err := s.List(Query{Limit: 50, SpamOnly: true})
	if err != nil {
		t.Fatalf("List(SpamOnly): %v", err)
	}
	if len(spam) != 2 {
		t.Fatalf("spam list = %v, want spam-1 and spam-2", ids(spam))
	}
	for _, m := range spam {
		if m.SpamAt == nil {
			t.Errorf("spam message %s has nil SpamAt", m.ID)
		}
	}
}

func TestMarkSpamBySenderMatchesByNameCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	ingestSpamTestMsg(t, s, "spam-1", "noisy-id", "storgio17")

	n, err := s.MarkSpamBySender("STORGIO17")
	if err != nil {
		t.Fatalf("MarkSpamBySender: %v", err)
	}
	if n != 1 {
		t.Fatalf("MarkSpamBySender by name flagged %d, want 1", n)
	}
}

func TestUnmarkSpamBySenderReverses(t *testing.T) {
	s := newTestStore(t)
	ingestSpamTestMsg(t, s, "spam-1", "noisy-id", "storgio17")
	if _, err := s.MarkSpamBySender("noisy-id"); err != nil {
		t.Fatalf("MarkSpamBySender: %v", err)
	}

	n, err := s.UnmarkSpamBySender("noisy-id")
	if err != nil {
		t.Fatalf("UnmarkSpamBySender: %v", err)
	}
	if n != 1 {
		t.Fatalf("UnmarkSpamBySender restored %d, want 1", n)
	}

	got, err := s.List(Query{Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "spam-1" {
		t.Fatalf("List after unspam = %v, want spam-1 restored", ids(got))
	}
}

func TestIngestPersistsSpamAt(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	_, err := s.Ingest(Message{
		ID:           "auto-spam-1",
		Channel:      "system",
		SenderID:     "noisy-id",
		Sender:       "Federation Customs I",
		Content:      "border crossing noted",
		TimestampUTC: now.Add(-time.Minute),
		Source:       "push",
		SpamAt:       &now,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	got, err := s.Get("auto-spam-1")
	if err != nil || got == nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SpamAt == nil {
		t.Fatal("ingested message has nil SpamAt, want set")
	}

	// Excluded from default listings.
	list, err := s.List(Query{Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List = %v, want empty (message is spam)", ids(list))
	}
}

func TestUnreadCountsExcludeSpam(t *testing.T) {
	s := newTestStore(t)
	ingestSpamTestMsg(t, s, "spam-1", "noisy-id", "storgio17")
	ingestSpamTestMsg(t, s, "keep-1", "friend-id", "Buddy27")
	if _, err := s.MarkSpamBySender("noisy-id"); err != nil {
		t.Fatalf("MarkSpamBySender: %v", err)
	}

	counts, err := s.UnreadCounts()
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts["local"] != 1 {
		t.Fatalf("UnreadCounts[local] = %d, want 1 (spam excluded)", counts["local"])
	}
}

func ids(msgs []Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}
