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
	WirePlayerObserver(c, kb)

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
