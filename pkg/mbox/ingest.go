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

	blockMu   sync.RWMutex
	blocklist *Blocklist
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

// SetBlocklist attaches a spam blocklist. Incoming messages whose sender_id or
// sender display name is blocked are stored directly in the spam folder
// (SpamAt set) regardless of which path ingested them. Pass nil to disable.
func (ing *Ingester) SetBlocklist(bl *Blocklist) {
	ing.blockMu.Lock()
	ing.blocklist = bl
	ing.blockMu.Unlock()
}

func (ing *Ingester) isBlocked(senderID, sender string) bool {
	ing.blockMu.RLock()
	bl := ing.blocklist
	ing.blockMu.RUnlock()
	return bl.IsBlocked(senderID, sender)
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
	Channels      []string
	MaxPerChannel int
	// ResetCursor clears each channel's saved cursor before crawling so
	// the run starts from the most recent page (i.e. "from now") rather
	// than resuming from the oldest message previously ingested. Used to
	// re-pull current pages after a server-side schema change adds new
	// fields that older stored messages are missing.
	ResetCursor bool
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
// messages that are not already present in the Store. Messages it already
// holds are skipped, not terminal: pushes leave holes below them. It stops
// per channel when MaxPerChannel rows have been walked, when the server
// returns an empty page, or when a page makes no progress.
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
	// seen counts every row the crawl walked, known or new; it is what the
	// per-channel cap bounds, so a long run of known rows still ends the
	// crawl instead of letting it walk the whole history.
	seen := 0

	// Resume from the saved cursor (oldest message previously ingested) so
	// successive backfills walk further into history instead of re-requesting
	// the latest page and immediately stopping on duplicate IDs. When
	// ResetCursor is set, drop the saved cursor and start from "now"
	// instead — used to re-pull recent pages after a schema change.
	if opts.ResetCursor {
		if err := ing.store.ClearCursor(channel); err != nil {
			ing.logger.Printf("clear cursor %s: %v", channel, err)
		}
	} else if cursor, ok, err := ing.store.Cursor(channel); err != nil {
		ing.logger.Printf("cursor %s: %v", channel, err)
	} else if ok {
		before = cursor.UTC().Format(time.RFC3339Nano)
	}

	for {
		if ctx.Err() != nil {
			return cr, ctx.Err()
		}
		if seen >= opts.MaxPerChannel {
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

		// GetChatHistory now blocks via execQuery until the server reply
		// has landed — read _last directly.
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

		// A message already in the store is NOT the floor of what we know:
		// pushes arrive only while logged in, so history below a known row
		// is full of holes (craftsman-1 had a June DM sitting under an
		// August push, unreachable by any number of runs while the crawl
		// stopped on the first duplicate). Known rows are skipped, not
		// terminal; the crawl ends on an empty page, the cap, or a page
		// that made no progress — the guard against a server that ignores
		// `before` and hands back the same page forever.
		hitCap := false
		prevBefore := before
		for _, m := range resp.Messages {
			if seen >= opts.MaxPerChannel {
				cr.Capped = true
				hitCap = true
				break
			}
			seen++

			ts, err := time.Parse(time.RFC3339, m.TimestampUTC)
			if err != nil {
				ts, err = time.Parse(time.RFC3339Nano, m.TimestampUTC)
				if err != nil {
					ts = time.Now().UTC()
				}
			}

			msg := Message{
				ID:             m.ID,
				Channel:        m.Channel,
				SenderID:       m.SenderID,
				Sender:         m.Sender,
				Content:        m.Content,
				TargetID:       m.TargetID,
				TargetName:     m.TargetName,
				TimestampUTC:   ts,
				Source:         "backfill",
				EmpireOfficial: m.EmpireOfficial,
			}
			if msg.Channel == "" {
				msg.Channel = channel
			}

			inserted, err := ing.store.Ingest(msg)
			if err != nil {
				return cr, fmt.Errorf("ingest: %w", err)
			}
			if inserted {
				cr.Fetched++
			}

			// The cursor is the true crawl floor — known rows included — so
			// the next run resumes below the hole, not at it.
			if oldestSeen.IsZero() || ts.Before(oldestSeen) {
				oldestSeen = ts
			}
			before = m.TimestampUTC
		}

		if hitCap || before == prevBefore {
			break
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
		ID:             msg.ID,
		Channel:        msg.Channel,
		SenderID:       msg.SenderID,
		Sender:         msg.Sender,
		Content:        msg.Content,
		TargetID:       msg.TargetID,
		TargetName:     msg.TargetName,
		TimestampUTC:   ts,
		Source:         effectiveSource,
		EmpireOfficial: msg.EmpireOfficial,
	}
	// Messages from blocked senders land directly in the spam folder.
	if ing.isBlocked(msg.SenderID, msg.Sender) {
		now := time.Now().UTC()
		m.SpamAt = &now
	}
	if _, err := ing.store.Ingest(m); err != nil {
		ing.logger.Printf("%s ingest error: %v", effectiveSource, err)
	}
}
