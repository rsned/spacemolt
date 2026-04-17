package dataservice

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/mbox"
)

// HistoryFetcher returns recent private-channel chat messages. A concrete
// implementation over game.GameClient lives in cmd/databot; this interface
// keeps Service testable without a live client.
type HistoryFetcher interface {
	Fetch(ctx context.Context, limit int) ([]serverapi.ChatMessage, error)
}

// Replier sends a chat reply. Abstracted for the same reason as HistoryFetcher.
type Replier interface {
	Reply(ctx context.Context, targetID, content string) error
}

// Config holds the runtime wiring for a Service.
type Config struct {
	AgentID  string
	Registry *Registry
	Mbox     *mbox.Store
	Fetcher  HistoryFetcher
	Replier  Replier
	Logger   *log.Logger

	// PollInterval controls the ingest-loop cadence. Defaults to 5s.
	PollInterval time.Duration

	// ReplyPace is the enforced minimum between outgoing Chat calls so the
	// server's 1-mutation-per-tick rule is respected. Defaults to 10s.
	ReplyPace time.Duration

	// HistoryLimit controls the max messages fetched per ingest call. Defaults to 50.
	HistoryLimit int
}

// Default config values.
const (
	defaultPollInterval = 5 * time.Second
	defaultReplyPace    = 10 * time.Second
	defaultHistoryLimit = 50
)

// Service is the long-running query responder.
type Service struct {
	cfg     Config
	seenMu  sync.Mutex
	seen    map[string]bool // dedup key → true; persists for the service lifetime
}

// NewService validates the config and returns a Service.
func NewService(cfg Config) (*Service, error) {
	if cfg.AgentID == "" {
		return nil, errors.New("dataservice: AgentID is required")
	}
	if cfg.Registry == nil {
		return nil, errors.New("dataservice: Registry is required")
	}
	if cfg.Mbox == nil {
		return nil, errors.New("dataservice: Mbox is required")
	}
	if cfg.Fetcher == nil {
		return nil, errors.New("dataservice: Fetcher is required")
	}
	if cfg.Replier == nil {
		return nil, errors.New("dataservice: Replier is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(log.Writer(), "[dataservice] ", log.LstdFlags)
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.ReplyPace <= 0 {
		cfg.ReplyPace = defaultReplyPace
	}
	if cfg.HistoryLimit <= 0 {
		cfg.HistoryLimit = defaultHistoryLimit
	}
	return &Service{cfg: cfg, seen: make(map[string]bool)}, nil
}

// Run drives the ingest and dispatch loops until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.ingestLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		s.dispatchLoop(ctx)
	}()
	wg.Wait()
	return nil
}

func (s *Service) ingestLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	s.ingestOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ingestOnce(ctx)
		}
	}
}

func (s *Service) ingestOnce(ctx context.Context) {
	msgs, err := s.cfg.Fetcher.Fetch(ctx, s.cfg.HistoryLimit)
	if err != nil {
		s.cfg.Logger.Printf("ingest fetch: %v", err)
		return
	}
	for _, m := range msgs {
		ts := parseTimestamp(m.TimestampUTC)
		_, err := s.cfg.Mbox.Ingest(mbox.Message{
			ID:           m.ID,
			Channel:      m.Channel,
			SenderID:     m.SenderID,
			Sender:       m.Sender,
			Content:      m.Content,
			TargetID:     m.TargetID,
			TargetName:   m.TargetName,
			TimestampUTC: ts,
			Source:       "dataservice",
		})
		if err != nil {
			s.cfg.Logger.Printf("mbox ingest %s: %v", m.ID, err)
		}
	}
}

func (s *Service) dispatchLoop(ctx context.Context) {
	t := time.NewTicker(s.cfg.PollInterval)
	defer t.Stop()
	s.drainOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.drainOnce(ctx)
		}
	}
}

// drainOnce processes unread private messages targeting this agent, one by
// one, pacing at ReplyPace between sends.
func (s *Service) drainOnce(ctx context.Context) {
	msgs, err := s.cfg.Mbox.List(mbox.Query{
		Channel:    "private",
		UnreadOnly: true,
		Limit:      s.cfg.HistoryLimit,
	})
	if err != nil {
		s.cfg.Logger.Printf("mbox list: %v", err)
		return
	}

	pending := s.filterAndDedupe(msgs)
	for _, m := range pending {
		if ctx.Err() != nil {
			return
		}
		s.handle(ctx, m)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.cfg.ReplyPace):
		}
	}
}

// filterAndDedupe keeps only messages addressed to us from someone else,
// drops duplicate (sender_id, content) pairs keeping the oldest, and
// returns them oldest-first. The seen map is persistent across drainOnce
// calls for the lifetime of the service to handle duplicates that arrive
// in separate ingest batches.
func (s *Service) filterAndDedupe(msgs []mbox.Message) []mbox.Message {
	// mbox.List returns newest-first. Reverse for FIFO processing.
	reversed := make([]mbox.Message, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		reversed = append(reversed, msgs[i])
	}

	s.seenMu.Lock()
	defer s.seenMu.Unlock()

	out := make([]mbox.Message, 0, len(reversed))
	for _, m := range reversed {
		if m.TargetID != s.cfg.AgentID {
			continue
		}
		if m.SenderID == s.cfg.AgentID {
			continue
		}
		key := m.SenderID + "\x00" + m.Content
		if s.seen[key] {
			if err := s.cfg.Mbox.MarkRead(m.ID); err != nil {
				s.cfg.Logger.Printf("mark read (dupe) %s: %v", m.ID, err)
			}
			continue
		}
		s.seen[key] = true
		out = append(out, m)
	}
	return out
}

// handle dispatches one message: produces a reply, sends it, marks read.
func (s *Service) handle(ctx context.Context, m mbox.Message) {
	reply, err := s.cfg.Registry.Dispatch(ctx, m.Content)
	if err != nil {
		s.cfg.Logger.Printf("dispatch %s: %v", m.ID, err)
		reply = "Error: internal failure while processing your request."
	}
	reply = TruncateReply(reply)
	if err := s.cfg.Replier.Reply(ctx, m.SenderID, reply); err != nil {
		s.cfg.Logger.Printf("reply %s: %v", m.ID, err)
		return
	}
	if err := s.cfg.Mbox.MarkRead(m.ID); err != nil {
		s.cfg.Logger.Printf("mark read %s: %v", m.ID, err)
	}
}

// parseTimestamp accepts RFC3339 or RFC3339Nano; falls back to now on failure.
func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now().UTC()
}
