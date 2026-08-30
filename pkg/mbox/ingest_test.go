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
		Channels:      []string{"local"},
		MaxPerChannel: 500,
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

// A message we already hold (from a push) is NOT the floor of what we know:
// pushes only arrive while logged in, so history below a known message is
// full of holes. The crawl must skip the known row and keep descending.
// craftsman-1's private channel demonstrated the failure: a June NPC DM sat
// below an August push and was unreachable by any number of backfill runs.
func TestBackfillCrawlsPastKnownID(t *testing.T) {
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

	// Page 1: 1 new message, then the known message, then an older one.
	// Page 2: an even older message on the far side of the hole.
	fc.addPage("local", []serverapi.ChatMessage{
		makeMsg("new-1", "local", base.Add(-1*time.Minute)),
		makeMsg("known-1", "local", base.Add(-2*time.Minute)),
		makeMsg("old-1", "local", base.Add(-3*time.Minute)),
	})
	fc.addPage("local", []serverapi.ChatMessage{
		makeMsg("older-1", "local", base.Add(-90*24*time.Hour)),
	})

	report, err := ing.Backfill(context.Background(), fc, BackfillOptions{
		Channels:      []string{"local"},
		MaxPerChannel: 500,
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}

	cr := report.Channels["local"]
	if cr.Fetched != 3 {
		t.Errorf("Fetched = %d, want 3 (new-1, old-1, older-1; known-1 skipped, not terminal)", cr.Fetched)
	}
	msgs, err := s.List(Query{Channel: "local", Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 4 {
		t.Errorf("stored %d messages, want 4", len(msgs))
	}
	// The cursor is the true crawl floor, so the next run resumes below the
	// hole instead of re-hitting the same known row.
	cursor, ok, err := s.Cursor("local")
	if err != nil || !ok {
		t.Fatalf("Cursor: ok=%v err=%v", ok, err)
	}
	if want := base.Add(-90 * 24 * time.Hour); !cursor.Equal(want) {
		t.Errorf("cursor = %v, want %v", cursor, want)
	}
}

// A server that ignored `before` would hand back the same page forever. With
// the known-ID stop gone, the no-progress check is what ends that crawl.
func TestBackfillStopsWhenPageMakesNoProgress(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	base := time.Now().UTC().Truncate(time.Second)
	page := []serverapi.ChatMessage{
		makeMsg("a", "local", base.Add(-1*time.Minute)),
		makeMsg("b", "local", base.Add(-2*time.Minute)),
	}
	fc := newFakeClient()
	for range 5 {
		fc.addPage("local", page)
	}
	report, err := NewIngester(s).Backfill(context.Background(), fc, BackfillOptions{Channels: []string{"local"}, MaxPerChannel: 500})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if fc.callIdx["local"] > 2 {
		t.Errorf("made %d requests against a stuck server, want at most 2", fc.callIdx["local"])
	}
	if report.Channels["local"].Fetched != 2 {
		t.Errorf("Fetched = %d, want 2", report.Channels["local"].Fetched)
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
		Channels:      []string{"local"},
		MaxPerChannel: 4,
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

func TestIngesterBlocklistFlagsPushAsSpam(t *testing.T) {
	s := newTestStore(t)
	ing := NewIngester(s)

	bl, err := LoadBlocklist(filepath.Join(t.TempDir(), "spam_list.json"))
	if err != nil {
		t.Fatalf("LoadBlocklist: %v", err)
	}
	if _, err := bl.Add("storgio17"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ing.SetBlocklist(bl)

	now := time.Now().UTC()
	// Blocked by display name.
	ing.HandlePush(serverapi.ChatMessage{
		ID:           "blocked-1",
		Channel:      "local",
		SenderID:     "noisy-id",
		Sender:       "storgio17",
		Content:      "Selling Cargo Expander III",
		TimestampUTC: now.Format(time.RFC3339Nano),
	})
	// Not blocked.
	ing.HandlePush(serverapi.ChatMessage{
		ID:           "ok-1",
		Channel:      "local",
		SenderID:     "friend-id",
		Sender:       "Buddy27",
		Content:      "hi",
		TimestampUTC: now.Format(time.RFC3339Nano),
	})

	blocked, err := s.Get("blocked-1")
	if err != nil || blocked == nil {
		t.Fatalf("Get blocked-1: %v", err)
	}
	if blocked.SpamAt == nil {
		t.Error("blocked message has nil SpamAt, want flagged as spam")
	}

	ok, err := s.Get("ok-1")
	if err != nil || ok == nil {
		t.Fatalf("Get ok-1: %v", err)
	}
	if ok.SpamAt != nil {
		t.Error("non-blocked message was flagged as spam")
	}
}
