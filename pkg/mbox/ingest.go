package mbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Ingester converts incoming server push events into stored Messages.
type Ingester struct {
	store  *Store
	logger *log.Logger

	selfMu sync.RWMutex
	selfID string
}

// NewIngester creates an Ingester backed by the given Store.
func NewIngester(store *Store) *Ingester {
	return &Ingester{
		store:  store,
		logger: log.New(log.Default().Writer(), "[mbox] ", log.LstdFlags),
	}
}

// SetSelfID registers the current player's internal ID so outbound echoes
// (messages where SenderID == selfID) can be tagged Source="sent" at
// ingest time. Pass an empty string to disable tagging.
func (ing *Ingester) SetSelfID(id string) {
	ing.selfMu.Lock()
	ing.selfID = id
	ing.selfMu.Unlock()
}

func (ing *Ingester) isSelf(id string) bool {
	ing.selfMu.RLock()
	defer ing.selfMu.RUnlock()
	return ing.selfID != "" && id == ing.selfID
}

// BackfillClient is the minimal interface required to perform a channel backfill.
type BackfillClient interface {
	GetChatHistory(ctx context.Context, channel string, payload map[string]any) error
	GetRawJSON(key string) []byte
}

// BackfillOptions controls the behaviour of a Backfill run.
type BackfillOptions struct {
	Channels        []string
	MaxPerChannel   int
	RequestInterval time.Duration
}

// BackfillReport summarises the results of a Backfill run.
type BackfillReport struct {
	Channels map[string]ChannelReport
}

// ChannelReport holds per-channel backfill statistics.
type ChannelReport struct {
	Fetched int
	Capped  bool
}

// Backfill crawls history for each channel in opts.Channels, storing any
// messages that are not already present in the Store.  It stops per channel
// when it encounters a message ID that already exists, when MaxPerChannel is
// reached, or when the server returns an empty page.
func (ing *Ingester) Backfill(ctx context.Context, client BackfillClient, opts BackfillOptions) (BackfillReport, error) {
	if opts.MaxPerChannel <= 0 {
		opts.MaxPerChannel = 500
	}
	report := BackfillReport{Channels: make(map[string]ChannelReport)}
	for _, ch := range opts.Channels {
		cr, err := ing.backfillChannel(ctx, client, ch, opts)
		if err != nil {
			ing.logger.Printf("backfill %s error: %v", ch, err)
		}
		report.Channels[ch] = cr
	}
	return report, nil
}

func (ing *Ingester) backfillChannel(ctx context.Context, client BackfillClient, channel string, opts BackfillOptions) (ChannelReport, error) {
	var cr ChannelReport
	var before string
	var oldestSeen time.Time

	for {
		if ctx.Err() != nil {
			return cr, ctx.Err()
		}
		if cr.Fetched >= opts.MaxPerChannel {
			cr.Capped = true
			break
		}

		payload := map[string]any{"limit": 100}
		if before != "" {
			payload["before"] = before
		}

		if err := client.GetChatHistory(ctx, channel, payload); err != nil {
			return cr, fmt.Errorf("get_chat_history(%s): %w", channel, err)
		}

		raw := client.GetRawJSON("_last")
		if len(raw) == 0 {
			break
		}

		var resp struct {
			Messages []serverapi.ChatMessage `json:"messages"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return cr, fmt.Errorf("unmarshal %s: %w", channel, err)
		}
		if len(resp.Messages) == 0 {
			break
		}

		hitKnown := false
		for _, m := range resp.Messages {
			if cr.Fetched >= opts.MaxPerChannel {
				cr.Capped = true
				hitKnown = true
				break
			}

			ts, err := time.Parse(time.RFC3339, m.TimestampUTC)
			if err != nil {
				ts, err = time.Parse(time.RFC3339Nano, m.TimestampUTC)
				if err != nil {
					ts = time.Now().UTC()
				}
			}

			msg := Message{
				ID:           m.ID,
				Channel:      m.Channel,
				SenderID:     m.SenderID,
				Sender:       m.Sender,
				Content:      m.Content,
				TargetID:     m.TargetID,
				TargetName:   m.TargetName,
				TimestampUTC: ts,
				Source:       "backfill",
			}
			if msg.Channel == "" {
				msg.Channel = channel
			}

			inserted, err := ing.store.Ingest(msg)
			if err != nil {
				return cr, fmt.Errorf("ingest: %w", err)
			}
			if !inserted {
				hitKnown = true
				break
			}
			cr.Fetched++

			if oldestSeen.IsZero() || ts.Before(oldestSeen) {
				oldestSeen = ts
			}
			before = m.TimestampUTC
		}

		if hitKnown {
			break
		}

		if opts.RequestInterval > 0 {
			time.Sleep(opts.RequestInterval)
		}
	}

	if !oldestSeen.IsZero() {
		if err := ing.store.SetCursor(channel, oldestSeen, cr.Capped); err != nil {
			ing.logger.Printf("set cursor %s: %v", channel, err)
		}
	}

	return cr, nil
}

// HandlePush converts a server-pushed ChatMessage and stores it in the mbox.
// Duplicate messages (same ID) are silently ignored.
// HandlePush ingests a message received via server push event.
func (ing *Ingester) HandlePush(msg serverapi.ChatMessage) {
	ing.ingestAPI(msg, "push")
}

// HandlePolled ingests a message discovered via polling get_chat_history.
func (ing *Ingester) HandlePolled(msg serverapi.ChatMessage) {
	ing.ingestAPI(msg, "reconcile")
}

func (ing *Ingester) ingestAPI(msg serverapi.ChatMessage, source string) {
	ts, err := time.Parse(time.RFC3339, msg.TimestampUTC)
	if err != nil {
		ts, err = time.Parse(time.RFC3339Nano, msg.TimestampUTC)
		if err != nil {
			ts = time.Now().UTC()
		}
	}

	// When the sender is us, this is an outbound message the server
	// echoed back. Tag it "sent" so callers can filter/display
	// direction without comparing IDs themselves.
	effectiveSource := source
	if ing.isSelf(msg.SenderID) {
		effectiveSource = "sent"
	}

	m := Message{
		ID:           msg.ID,
		Channel:      msg.Channel,
		SenderID:     msg.SenderID,
		Sender:       msg.Sender,
		Content:      msg.Content,
		TargetID:     msg.TargetID,
		TargetName:   msg.TargetName,
		TimestampUTC: ts,
		Source:       effectiveSource,
	}
	if _, err := ing.store.Ingest(m); err != nil {
		ing.logger.Printf("%s ingest error: %v", effectiveSource, err)
	}
}
