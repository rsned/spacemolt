package mbox

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// fakeGameClient implements BackfillClient for testing.
type fakeGameClient struct {
	pages   map[string][][]serverapi.ChatMessage
	callIdx map[string]int
	rawJSON []byte
	mu      sync.Mutex
}

func newFakeClient() *fakeGameClient {
	return &fakeGameClient{
		pages:   make(map[string][][]serverapi.ChatMessage),
		callIdx: make(map[string]int),
	}
}

func (f *fakeGameClient) addPage(channel string, msgs []serverapi.ChatMessage) {
	f.pages[channel] = append(f.pages[channel], msgs)
}

func (f *fakeGameClient) GetChatHistory(_ context.Context, channel string, _ map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.callIdx[channel]
	f.callIdx[channel]++
	pages := f.pages[channel]
	if i >= len(pages) {
		data, _ := json.Marshal(map[string]any{"channel": channel, "messages": []serverapi.ChatMessage{}})
		f.rawJSON = data
		return nil
	}
	data, _ := json.Marshal(map[string]any{"channel": channel, "messages": pages[i]})
	f.rawJSON = data
	return nil
}

func (f *fakeGameClient) GetRawJSON(_ string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rawJSON
}

func makeMsg(id, channel string, t time.Time) serverapi.ChatMessage {
	return serverapi.ChatMessage{
		ID:           id,
		Channel:      channel,
		SenderID:     "player-1",
		Sender:       "Tester",
		Content:      fmt.Sprintf("msg %s", id),
		TimestampUTC: t.UTC().Format(time.RFC3339),
	}
}

func TestBackfillBasic(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	ing := NewIngester(s)
	fc := newFakeClient()

	base := time.Now().UTC().Truncate(time.Second)
	var page []serverapi.ChatMessage
	for i := range 5 {
		page = append(page, makeMsg(fmt.Sprintf("msg-%d", i), "local", base.Add(-time.Duration(i)*time.Minute)))
	}
	fc.addPage("local", page)

	report, err := ing.Backfill(context.Background(), fc, BackfillOptions{
		Channels:        []string{"local"},
		MaxPerChannel:   500,
		RequestInterval: 0,
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}

	cr := report.Channels["local"]
	if cr.Fetched != 5 {
		t.Errorf("Fetched = %d, want 5", cr.Fetched)
	}
	if cr.Capped {
		t.Error("Capped = true, want false")
	}

	msgs, err := s.List(Query{Channel: "local", Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("stored %d messages, want 5", len(msgs))
	}
}

func TestBackfillStopsOnKnownID(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	base := time.Now().UTC().Truncate(time.Second)

	// Pre-seed one known message.
	knownMsg := Message{
		ID:           "known-1",
		Channel:      "local",
		SenderID:     "player-1",
		Sender:       "Tester",
		Content:      "already here",
		TimestampUTC: base.Add(-2 * time.Minute),
		Source:       "push",
	}
	if _, err := s.Ingest(knownMsg); err != nil {
		t.Fatalf("Ingest known: %v", err)
	}

	ing := NewIngester(s)
	fc := newFakeClient()

	// Page has: 1 new message, then the known message, then an older message.
	page := []serverapi.ChatMessage{
		makeMsg("new-1", "local", base.Add(-1*time.Minute)),
		makeMsg("known-1", "local", base.Add(-2*time.Minute)),
		makeMsg("old-1", "local", base.Add(-3*time.Minute)),
	}
	fc.addPage("local", page)

	report, err := ing.Backfill(context.Background(), fc, BackfillOptions{
		Channels:        []string{"local"},
		MaxPerChannel:   500,
		RequestInterval: 0,
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}

	cr := report.Channels["local"]
	if cr.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1 (should stop at known ID)", cr.Fetched)
	}

	msgs, err := s.List(Query{Channel: "local", Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 1 pre-seeded + 1 new = 2
	if len(msgs) != 2 {
		t.Errorf("stored %d messages, want 2", len(msgs))
	}
}

func TestBackfillCap(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	ing := NewIngester(s)
	fc := newFakeClient()

	base := time.Now().UTC().Truncate(time.Second)

	// Two pages of 3 messages each (6 total).
	var page1, page2 []serverapi.ChatMessage
	for i := range 3 {
		page1 = append(page1, makeMsg(fmt.Sprintf("p1-%d", i), "local", base.Add(-time.Duration(i)*time.Minute)))
	}
	for i := range 3 {
		page2 = append(page2, makeMsg(fmt.Sprintf("p2-%d", i), "local", base.Add(-time.Duration(3+i)*time.Minute)))
	}
	fc.addPage("local", page1)
	fc.addPage("local", page2)

	report, err := ing.Backfill(context.Background(), fc, BackfillOptions{
		Channels:        []string{"local"},
		MaxPerChannel:   4,
		RequestInterval: 0,
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}

	cr := report.Channels["local"]
	if cr.Fetched != 4 {
		t.Errorf("Fetched = %d, want 4", cr.Fetched)
	}
	if !cr.Capped {
		t.Error("Capped = false, want true")
	}
}

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

func TestIngester_SetSelfID_TagsSentMessages(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	ing := NewIngester(s)
	ing.SetSelfID("self-id")

	// Outbound echo: sender is self.
	ing.HandlePush(serverapi.ChatMessage{
		ID:           "out-1",
		Channel:      "private",
		SenderID:     "self-id",
		Sender:       "Me",
		Content:      "hi there",
		TargetID:     "peer-id",
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
	})
	// Inbound: sender is someone else.
	ing.HandlePush(serverapi.ChatMessage{
		ID:           "in-1",
		Channel:      "private",
		SenderID:     "peer-id",
		Sender:       "Peer",
		Content:      "hello back",
		TargetID:     "self-id",
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
	})

	out, err := s.Get("out-1")
	if err != nil || out == nil {
		t.Fatalf("Get out-1: %v", err)
	}
	if out.Source != "sent" {
		t.Errorf("outbound: Source = %q, want %q", out.Source, "sent")
	}

	in, err := s.Get("in-1")
	if err != nil || in == nil {
		t.Fatalf("Get in-1: %v", err)
	}
	if in.Source != "push" {
		t.Errorf("inbound: Source = %q, want %q", in.Source, "push")
	}
}

func TestIngester_NoSelfID_DoesNotTagSent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	ing := NewIngester(s)
	// No SetSelfID call — legacy callers should see no behavior change.

	ing.HandlePush(serverapi.ChatMessage{
		ID:           "legacy-1",
		Channel:      "private",
		SenderID:     "someone",
		Content:      "hi",
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
	})

	m, err := s.Get("legacy-1")
	if err != nil || m == nil {
		t.Fatalf("Get: %v", err)
	}
	if m.Source != "push" {
		t.Errorf("Source = %q, want %q (legacy behavior)", m.Source, "push")
	}
}
