package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// intelFake serves faction_query_intel pages from a fixed pool.
type intelFake struct {
	game.GameClient
	pool    []serverapi.IntelEntry
	last    []byte
	offsets []int
	stale   bool // when true, every reply claims offset 0 (a stale cached reply)
}

func (f *intelFake) FactionQueryIntel(_ context.Context, p map[string]any) error {
	off, _ := p["offset"].(int)
	lim, _ := p["limit"].(int)
	f.offsets = append(f.offsets, off)
	end := min(off+lim, len(f.pool))
	page := f.pool[min(off, len(f.pool)):end]
	entries, _ := json.Marshal(page)
	replyOff := off
	if f.stale {
		replyOff = 0
	}
	f.last, _ = json.Marshal(map[string]any{
		"entries": json.RawMessage(entries), "count": len(page), "total": len(f.pool),
		"offset": replyOff, "limit": lim, "current_tick": 5000,
	})
	return nil
}

func (f *intelFake) GetRawJSON(key string) []byte {
	if key == "faction_intel" {
		return f.last
	}
	return nil
}

type intelSink struct {
	calls   int
	systems []string
	tick    int64
	faction string
}

func (s *intelSink) StoreFactionIntel(_ context.Context, factionID string, entries []serverapi.IntelEntry, tick int64, _ string) error {
	s.calls++
	s.faction, s.tick = factionID, tick
	for _, e := range entries {
		s.systems = append(s.systems, e.SystemID)
	}
	return nil
}

func intelPool(n int) []serverapi.IntelEntry {
	out := make([]serverapi.IntelEntry, n)
	for i := range n {
		out[i] = serverapi.IntelEntry{SystemID: fmt.Sprintf("sys_%03d", i)}
	}
	return out
}

func TestCaptureFactionIntelPagesThroughThePool(t *testing.T) {
	f := &intelFake{pool: intelPool(250)}
	sink := &intelSink{}
	n, err := captureFactionIntel(context.Background(), f, sink, "crft", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if n != 250 || len(sink.systems) != 250 || sink.systems[249] != "sys_249" {
		t.Fatalf("stored %d (%d systems), want 250", n, len(sink.systems))
	}
	if want := []int{0, 100, 200}; fmt.Sprint(f.offsets) != fmt.Sprint(want) {
		t.Errorf("offsets = %v, want %v", f.offsets, want)
	}
	if sink.faction != "crft" || sink.tick != 5000 {
		t.Errorf("faction %q tick %d, want crft 5000", sink.faction, sink.tick)
	}
}

func TestCaptureFactionIntelStopsOnAStaleReply(t *testing.T) {
	f := &intelFake{pool: intelPool(250), stale: true}
	sink := &intelSink{}
	if _, err := captureFactionIntel(context.Background(), f, sink, "crft", io.Discard); err == nil {
		t.Fatal("want an error when a page reply names the wrong offset")
	}
	if len(sink.systems) != 100 {
		t.Errorf("stored %d systems, want only the first page (100)", len(sink.systems))
	}
}

func TestCaptureFactionIntelNeedsAFaction(t *testing.T) {
	f := &intelFake{pool: intelPool(3)}
	n, err := captureFactionIntel(context.Background(), f, &intelSink{}, "", io.Discard)
	if err != nil || n != 0 || len(f.offsets) != 0 {
		t.Errorf("no faction: n=%d err=%v queries=%d, want a silent no-op", n, err, len(f.offsets))
	}
}
