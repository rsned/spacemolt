package agent

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestWirePlayerObserver_RecordsThroughKB(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	c := &game.Client{}
	WirePlayerObserver(c, kb, nil)

	cb := c.PlayerObserver()
	if cb == nil {
		t.Fatal("WirePlayerObserver did not register an observer")
	}
	cb([]game.ObservedPlayer{{
		PlayerID:  "p1",
		Username:  "TraderUser6",
		ShipClass: "theoria",
		SystemID:  "sys-A",
		POIID:     "poi-X",
		Source:    "get_nearby",
		SeenAt:    time.Now().UTC(),
	}})

	got, err := kb.GetSeenPlayer("p1")
	if err != nil {
		t.Fatalf("GetSeenPlayer: %v", err)
	}
	if got == nil {
		t.Fatal("GetSeenPlayer returned nil — observer did not record")
	}
	if got.Username != "TraderUser6" {
		t.Errorf("Username = %q, want TraderUser6", got.Username)
	}
}

type fakeEnqueuer struct{ ids []string }

func (f *fakeEnqueuer) Enqueue(ids ...string) { f.ids = append(f.ids, ids...) }

func TestWirePlayerObserver_EnqueuesDistinctFactionIDs(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	enq := &fakeEnqueuer{}
	c := &game.Client{}
	WirePlayerObserver(c, kb, enq)

	c.PlayerObserver()([]game.ObservedPlayer{
		{PlayerID: "p1", Username: "A", FactionID: "fed", SeenAt: time.Now().UTC()},
		{PlayerID: "p2", Username: "B", FactionID: "fed", SeenAt: time.Now().UTC()}, // dup faction
		{PlayerID: "p3", Username: "C", FactionID: "pirates", SeenAt: time.Now().UTC()},
		{PlayerID: "p4", Username: "D", FactionID: "", SeenAt: time.Now().UTC()}, // no faction
	})

	seen := map[string]bool{}
	for _, id := range enq.ids {
		seen[id] = true
	}
	if !seen["fed"] || !seen["pirates"] {
		t.Errorf("enqueued %v, want fed and pirates", enq.ids)
	}
	if seen[""] {
		t.Error("empty faction id should not be enqueued")
	}
}
