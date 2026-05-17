package game

import (
	"sync"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestSetPlayerObserver_StoresCallback(t *testing.T) {
	c := &Client{}

	var mu sync.Mutex
	var fired bool
	c.SetPlayerObserver(func(_ []ObservedPlayer) {
		mu.Lock()
		fired = true
		mu.Unlock()
	})

	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()

	if cb == nil {
		t.Fatal("playerObserver not registered")
	}
	cb(nil)

	mu.Lock()
	defer mu.Unlock()
	if !fired {
		t.Error("callback not invoked")
	}
}

func captureObserver(t *testing.T, c *Client) *[]ObservedPlayer {
	t.Helper()
	var captured []ObservedPlayer
	c.SetPlayerObserver(func(obs []ObservedPlayer) {
		captured = append(captured, obs...)
	})
	return &captured
}

func TestNotifyPlayers_StampsContextFields(t *testing.T) {
	c := &Client{state: &State{CurrentSystem: "sys-treasure"}}
	got := captureObserver(t, c)

	players := []serverapi.NearbyPlayer{
		{PlayerID: "p1", Username: "u1", ShipClass: "theoria", FactionTag: "STRG"},
		{PlayerID: "p2", Username: "u2", ShipClass: "viper"},
	}
	c.notifyPlayers("get_nearby", players, "poi-haven")

	if len(*got) != 2 {
		t.Fatalf("got %d observations, want 2", len(*got))
	}
	for _, o := range *got {
		if o.SystemID != "sys-treasure" {
			t.Errorf("SystemID=%q, want sys-treasure", o.SystemID)
		}
		if o.POIID != "poi-haven" {
			t.Errorf("POIID=%q, want poi-haven", o.POIID)
		}
		if o.Source != "get_nearby" {
			t.Errorf("Source=%q, want get_nearby", o.Source)
		}
		if o.SeenAt.IsZero() {
			t.Error("SeenAt is zero")
		}
	}
}

func TestNotifyPlayers_NoObserverIsNoOp(t *testing.T) {
	c := &Client{state: &State{}}
	// Should not panic.
	c.notifyPlayers("get_nearby", []serverapi.NearbyPlayer{{PlayerID: "p1"}}, "")
}

func TestNotifyPlayersFromBattle_MarksInCombat(t *testing.T) {
	c := &Client{state: &State{CurrentSystem: "sys-A"}}
	got := captureObserver(t, c)

	// BattleParticipant has no FactionTag (faction is on BattleSide, not
	// per-participant); battle helper only populates identity + InCombat.
	parts := []serverapi.BattleParticipant{
		{PlayerID: "p1", Username: "u1", ShipClass: "theoria"},
	}
	c.notifyPlayersFromBattle("battle_alert", parts)

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	if !(*got)[0].InCombat {
		t.Error("InCombat=false, want true for battle source")
	}
	if (*got)[0].Source != "battle_alert" {
		t.Errorf("Source=%q, want battle_alert", (*got)[0].Source)
	}
}

func TestNotifyPlayerFromChat_NoShipNoPOI(t *testing.T) {
	c := &Client{state: &State{CurrentSystem: "sys-A"}}
	got := captureObserver(t, c)

	// ChatMessage has no FactionTag — only SenderID, Sender, Channel,
	// Content, SystemID/POIID, EmpireOfficial, timestamps.
	c.notifyPlayerFromChat(serverapi.ChatMessage{
		SenderID: "p1",
		Sender:   "Director-General Darya Lim",
	})

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	o := (*got)[0]
	if o.ShipClass != "" {
		t.Errorf("ShipClass=%q, want empty", o.ShipClass)
	}
	if o.POIID != "" {
		t.Errorf("POIID=%q, want empty", o.POIID)
	}
	if o.SystemID != "" {
		t.Errorf("SystemID=%q, want empty (identity-only)", o.SystemID)
	}
	if o.Source != "chat_message" {
		t.Errorf("Source=%q, want chat_message", o.Source)
	}
}
