package faction

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

type fakeCollector struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeCollector) CollectFaction(_ context.Context, _ game.GameClient, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	return nil
}

func (f *fakeCollector) sortedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.calls...)
	sort.Strings(out)
	return out
}

type fakeFreshness struct {
	captured map[string]time.Time
}

func (f *fakeFreshness) FactionCapturedAt(_ context.Context, id string) (time.Time, bool, error) {
	t, ok := f.captured[id]
	return t, ok, nil
}

func TestBackfillProcessFreshnessBranches(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	fresh := &fakeFreshness{captured: map[string]time.Time{
		"fresh": now.Add(-1 * time.Hour),   // within threshold -> skip
		"stale": now.Add(-48 * time.Hour),  // older than threshold -> fetch
	}}
	col := &fakeCollector{}
	b := NewFactionBackfiller(nil, col, fresh, 24*time.Hour, nil)
	b.now = func() time.Time { return now }

	ctx := context.Background()
	b.process(ctx, "fresh")
	b.process(ctx, "stale")
	b.process(ctx, "missing") // not in the freshness map -> fetch

	got := col.sortedCalls()
	want := []string{"missing", "stale"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("CollectFaction calls = %v, want %v (fresh skipped, stale+missing fetched)", got, want)
	}
}

func TestBackfillEnqueueDedupesAndDropsEmpty(t *testing.T) {
	b := NewFactionBackfiller(nil, &fakeCollector{}, &fakeFreshness{}, 24*time.Hour, nil)
	b.Enqueue("f1", "", "f1", "f2", "f1")

	// Drain the channel non-blockingly.
	var queued []string
	for {
		select {
		case id := <-b.ch:
			queued = append(queued, id)
		default:
			sort.Strings(queued)
			if len(queued) != 2 || queued[0] != "f1" || queued[1] != "f2" {
				t.Fatalf("queued = %v, want [f1 f2] (deduped, empty dropped)", queued)
			}
			return
		}
	}
}

func TestBackfillEnqueueAfterProcessDoesNotRequeue(t *testing.T) {
	b := NewFactionBackfiller(nil, &fakeCollector{}, &fakeFreshness{}, 24*time.Hour, nil)
	b.Enqueue("f1")
	<-b.ch // simulate the worker consuming it
	b.Enqueue("f1")

	select {
	case id := <-b.ch:
		t.Fatalf("f1 re-queued after first enqueue: got %q", id)
	default:
	}
}
