package dataservice

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/mbox"
)

// stubClient captures Chat() calls and hands out queued history on Fetch.
type stubClient struct {
	mu          sync.Mutex
	sentChats   []stubChat
	nextHistory []serverapi.ChatMessage
}

type stubChat struct {
	Channel  string
	Content  string
	TargetID string
}

func (s *stubClient) chat(channel, content, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sentChats = append(s.sentChats, stubChat{Channel: channel, Content: content, TargetID: targetID})
	return nil
}

func (s *stubClient) sentSnapshot() []stubChat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubChat, len(s.sentChats))
	copy(out, s.sentChats)
	return out
}

func (s *stubClient) countSent() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sentChats)
}

func (s *stubClient) setHistory(msgs []serverapi.ChatMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextHistory = msgs
}

func (s *stubClient) drainHistory() []serverapi.ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.nextHistory
	s.nextHistory = nil
	return out
}

type stubFetcher struct{ client *stubClient }

func (f *stubFetcher) Fetch(ctx context.Context, limit int) ([]serverapi.ChatMessage, error) {
	return f.client.drainHistory(), nil
}

type stubReplier struct{ client *stubClient }

func (r *stubReplier) Reply(ctx context.Context, targetID, content string) error {
	return r.client.chat("private", content, targetID)
}

// echoHandler replies with the raw args — just to exercise dispatch.
type echoHandler struct{}

func (echoHandler) Name() string                { return "echo" }
func (echoHandler) ShortHelp() string           { return "echo args back" }
func (echoHandler) PlaintextUsage() string      { return "echo <text>" }
func (echoHandler) JSONExample() map[string]any { return map[string]any{"query": "echo", "params": map[string]any{"text": "hi"}} }
func (echoHandler) HandlePlaintext(ctx context.Context, deps Deps, args []string) (string, error) {
	return "echo: " + strings.Join(args, " "), nil
}
func (echoHandler) HandleJSON(ctx context.Context, deps Deps, params map[string]any) (map[string]any, error) {
	text, _ := params["text"].(string)
	return map[string]any{"echo": text}, nil
}

func newTestService(t *testing.T) (*Service, *stubClient, *mbox.Store, func()) {
	t.Helper()
	client := &stubClient{}
	store, err := mbox.Open(filepath.Join(t.TempDir(), "mbox.db"))
	if err != nil {
		t.Fatalf("mbox.Open: %v", err)
	}
	reg := NewRegistry(Deps{})
	reg.Register(echoHandler{})
	cfg := Config{
		AgentID:      "databot-test",
		Registry:     reg,
		Mbox:         store,
		Fetcher:      &stubFetcher{client: client},
		Replier:      &stubReplier{client: client},
		PollInterval: 10 * time.Millisecond,
		ReplyPace:    1 * time.Millisecond,
	}
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	cleanup := func() { _ = store.Close() }
	return svc, client, store, cleanup
}

func TestService_DispatchesPrivateMessageToSelf(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID:           "m1",
			Channel:      "private",
			SenderID:     "miner-1",
			Sender:       "Preston",
			Content:      "echo hello",
			TargetID:     "databot-test",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	if !waitUntil(t, 400*time.Millisecond, func() bool { return client.countSent() == 1 }) {
		t.Fatalf("timed out waiting for reply; sent=%d", client.countSent())
	}
	sent := client.sentSnapshot()[0]
	if sent.TargetID != "miner-1" {
		t.Errorf("target: got %q", sent.TargetID)
	}
	if sent.Channel != "private" {
		t.Errorf("channel: got %q", sent.Channel)
	}
	if !strings.Contains(sent.Content, "echo: hello") {
		t.Errorf("content: got %q", sent.Content)
	}
}

func TestService_IgnoresMessagesForOthers(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID: "m1", Channel: "private", SenderID: "miner-1", Sender: "M",
			Content: "echo hi", TargetID: "some-other-bot",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = svc.Run(ctx)

	if n := client.countSent(); n != 0 {
		t.Errorf("should not have replied to someone else's DM; sent=%d", n)
	}
}

func TestService_IgnoresMessagesFromSelf(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID: "m1", Channel: "private", SenderID: "databot-test", Sender: "D",
			Content: "echo self", TargetID: "databot-test",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = svc.Run(ctx)

	if n := client.countSent(); n != 0 {
		t.Errorf("should not have self-replied; sent=%d", n)
	}
}

func TestService_DedupesIdenticalRequests(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	ts := time.Now().UTC()
	client.setHistory([]serverapi.ChatMessage{
		{ID: "m1", Channel: "private", SenderID: "miner-1", Content: "echo dup", TargetID: "databot-test", TimestampUTC: ts.Format(time.RFC3339Nano)},
		{ID: "m2", Channel: "private", SenderID: "miner-1", Content: "echo dup", TargetID: "databot-test", TimestampUTC: ts.Add(time.Second).Format(time.RFC3339Nano)},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	// Wait long enough for both messages to be processed but dedupe to kick in.
	time.Sleep(250 * time.Millisecond)
	if n := client.countSent(); n != 1 {
		t.Errorf("expected exactly one reply (dedupe), got %d", n)
	}
}

func TestService_ReplyTruncated(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	long := strings.Repeat("x", 1000)
	client.setHistory([]serverapi.ChatMessage{
		{ID: "m1", Channel: "private", SenderID: "miner-1", Content: "echo " + long, TargetID: "databot-test", TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano)},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	if !waitUntil(t, 300*time.Millisecond, func() bool { return client.countSent() == 1 }) {
		t.Fatalf("timed out waiting for reply")
	}
	if got := client.sentSnapshot()[0].Content; len(got) > MaxReplyChars {
		t.Errorf("sent content exceeded MaxReplyChars: %d", len(got))
	}
}

func TestService_JSONRoundTrip(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{ID: "m1", Channel: "private", SenderID: "miner-1", Content: `{"query":"echo","params":{"text":"ping"}}`, TargetID: "databot-test", TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano)},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	if !waitUntil(t, 300*time.Millisecond, func() bool { return client.countSent() == 1 }) {
		t.Fatalf("timed out waiting for reply")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(client.sentSnapshot()[0].Content), &parsed); err != nil {
		t.Fatalf("reply not JSON: %v", err)
	}
	if parsed["echo"] != "ping" {
		t.Errorf("echo: got %v", parsed["echo"])
	}
}

// waitUntil polls cond() every 5ms until it returns true or timeout.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}
