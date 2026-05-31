package faction

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// factionCollector fetches and persists a single faction's details. Satisfied
// by *Collector.CollectFaction.
type factionCollector interface {
	CollectFaction(ctx context.Context, client game.GameClient, factionID string) error
}

// factionFreshness reports when a faction was last captured. Satisfied by
// *knowledge.SQLiteKB.
type factionFreshness interface {
	FactionCapturedAt(ctx context.Context, factionID string) (time.Time, bool, error)
}

// backfillQueueSize bounds the pending-fetch channel. Enqueue is non-blocking;
// ids beyond this are dropped (best-effort — the faction will be picked up the
// next time it's observed).
const backfillQueueSize = 256

// FactionBackfiller fetches faction_info for observed faction_ids in the
// background and persists the details, so the factions tables fill in beyond
// the inline id+tag carried on sightings. Enqueue is called from the player
// observer (on the client's response goroutine) and must not block; the actual
// faction_info network calls happen on the backfiller's own goroutine.
type FactionBackfiller struct {
	client    game.GameClient
	collector factionCollector
	fresh     factionFreshness
	threshold time.Duration
	logger    *log.Logger

	ch   chan string
	mu   sync.Mutex
	seen map[string]bool // ids already enqueued this session

	now func() time.Time // injectable clock for tests
}

// NewFactionBackfiller builds a backfiller. threshold is how long a stored
// faction is considered fresh before it's re-fetched.
func NewFactionBackfiller(client game.GameClient, collector factionCollector, fresh factionFreshness, threshold time.Duration, logger *log.Logger) *FactionBackfiller {
	if logger == nil {
		logger = log.Default()
	}
	return &FactionBackfiller{
		client:    client,
		collector: collector,
		fresh:     fresh,
		threshold: threshold,
		logger:    logger,
		ch:        make(chan string, backfillQueueSize),
		seen:      make(map[string]bool),
		now:       time.Now,
	}
}

// Enqueue schedules backfill for the given faction ids. Empty ids and ids
// already enqueued this session are dropped. Non-blocking: if the queue is
// full the id is skipped.
func (b *FactionBackfiller) Enqueue(ids ...string) {
	for _, id := range ids {
		if id == "" {
			continue
		}
		b.mu.Lock()
		if b.seen[id] {
			b.mu.Unlock()
			continue
		}
		b.seen[id] = true
		b.mu.Unlock()

		select {
		case b.ch <- id:
		default:
			// Queue full; drop. Allow a future observation to retry by
			// forgetting we enqueued it.
			b.mu.Lock()
			delete(b.seen, id)
			b.mu.Unlock()
		}
	}
}

// Start launches the background worker until ctx is cancelled.
func (b *FactionBackfiller) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case id := <-b.ch:
				b.process(ctx, id)
				// Space out fetches so a burst of new factions doesn't hammer
				// the server.
				select {
				case <-ctx.Done():
					return
				case <-time.After(game.SleepQuick):
				}
			}
		}
	}()
}

// process fetches and stores a faction's details unless a sufficiently fresh
// record already exists.
func (b *FactionBackfiller) process(ctx context.Context, id string) {
	if captured, ok, err := b.fresh.FactionCapturedAt(ctx, id); err != nil {
		b.logger.Printf("[faction-backfill] freshness check %s: %v", id, err)
	} else if ok && b.now().Sub(captured) < b.threshold {
		return // already fresh
	}
	if err := b.collector.CollectFaction(ctx, b.client, id); err != nil {
		b.logger.Printf("[faction-backfill] collect %s: %v", id, err)
	}
}
