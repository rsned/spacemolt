package game

import (
	"encoding/json"
	"io"
	"log"
	"sync"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
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

// newHandleResponseTestClient builds a *Client with the minimum state
// (latestRawJSON map + state) needed for handleResponse to run without
// panicking on a nil map write.
func newHandleResponseTestClient(systemID string) *Client {
	return &Client{
		state:         &State{CurrentSystem: systemID},
		latestRawJSON: make(map[string][]byte),
		debugLogger:   log.New(io.Discard, "", 0),
	}
}

// payloadMarshal JSON-roundtrips a value through map[string]any so it
// matches the shape handleResponse receives from a real server message.
func payloadMarshal(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func TestHandleResponse_FiresOnGetNearby(t *testing.T) {
	c := newHandleResponseTestClient("sys-A")
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"action": "get_nearby",
		"poi_id": "poi-haven",
		"nearby": []serverapi.NearbyPlayer{
			{PlayerID: "p1", Username: "u1", ShipClass: "theoria"},
		},
	})
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("observer got %d, want 1", len(*got))
	}
	if (*got)[0].POIID != "poi-haven" {
		t.Errorf("POIID=%q, want poi-haven", (*got)[0].POIID)
	}
	if (*got)[0].Source != "get_nearby" {
		t.Errorf("Source=%q, want get_nearby", (*got)[0].Source)
	}
	if (*got)[0].SystemID != "sys-A" {
		t.Errorf("SystemID=%q, want sys-A", (*got)[0].SystemID)
	}
}

func TestHandleResponse_FiresOnGetSystemAgents(t *testing.T) {
	c := newHandleResponseTestClient("sys-A")
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"agents": []serverapi.NearbyPlayer{
			{PlayerID: "p1", Username: "u1", ShipClass: "viper"},
			{PlayerID: "p2", Username: "u2", ShipClass: "theoria"},
		},
		"system_id": "sys-A",
		"count":     2,
	})
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 2 {
		t.Fatalf("observer got %d, want 2", len(*got))
	}
	if (*got)[0].Source != "get_system_agents" {
		t.Errorf("Source=%q, want get_system_agents", (*got)[0].Source)
	}
	if (*got)[0].POIID != "" {
		t.Errorf("POIID=%q, want empty (system-scope)", (*got)[0].POIID)
	}
}

func TestHandleResponse_FiresOnBattleAlert(t *testing.T) {
	c := newHandleResponseTestClient("sys-A")
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"battle_id": "b1",
		"system_id": "sys-A",
		"participants": []serverapi.BattleParticipant{
			{PlayerID: "p1", Username: "u1", ShipClass: "viper"},
		},
	})
	c.handleResponse(protocol.Response{Type: protocol.TypeBattleAlert, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	if !(*got)[0].InCombat {
		t.Error("expected InCombat=true")
	}
	if (*got)[0].Source != "battle_alert" {
		t.Errorf("Source=%q, want battle_alert", (*got)[0].Source)
	}
}

func TestHandleResponse_FiresOnChatMessage(t *testing.T) {
	c := newHandleResponseTestClient("sys-A")
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"channel":   "system",
		"sender":    "Director-General Darya Lim",
		"sender_id": "p1",
		"content":   "Federation notice ...",
	})
	c.handleResponse(protocol.Response{Type: protocol.TypeChatMessage, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	if (*got)[0].ShipClass != "" {
		t.Errorf("ShipClass=%q, want empty for chat", (*got)[0].ShipClass)
	}
	if (*got)[0].SystemID != "" {
		t.Errorf("SystemID=%q, want empty for identity-only chat", (*got)[0].SystemID)
	}
	if (*got)[0].Source != "chat_message" {
		t.Errorf("Source=%q, want chat_message", (*got)[0].Source)
	}
}

func TestHandleResponse_FiresOnGetSystemOnlinePlayers(t *testing.T) {
	c := newHandleResponseTestClient("sys-A")
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"system_id": "sys-A",
		"name":      "Treasure Cache",
		"online_players": []serverapi.NearbyPlayer{
			{PlayerID: "p1", Username: "u1", ShipClass: "theoria"},
		},
	})
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	if (*got)[0].Source != "get_system" {
		t.Errorf("Source=%q, want get_system", (*got)[0].Source)
	}
	if (*got)[0].POIID != "" {
		t.Errorf("POIID=%q, want empty (system-scope)", (*got)[0].POIID)
	}
}
