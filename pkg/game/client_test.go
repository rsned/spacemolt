package game

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestClientReady_ClosesOnFirstMessage verifies that the Ready() channel
// closes when the first message is received
func TestClientReady_ClosesOnFirstMessage(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	// Ready channel should be open initially
	select {
	case <-client.Ready():
		t.Fatal("Ready channel should not be closed before first message")
	default:
		// Expected: channel is open
	}

	// Simulate receiving first message by calling readyOnce
	client.readyOnce.Do(func() {
		close(client.readyChan)
	})

	// Ready channel should now be closed
	select {
	case <-client.Ready():
		// Expected: channel is closed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Ready channel should be closed after first message")
	}
}

// TestClientReady_ClosesOnlyOnce verifies that Ready channel closes exactly once
// even with multiple messages
func TestClientReady_ClosesOnlyOnce(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	// Close multiple times - should not panic
	for i := 0; i < 5; i++ {
		client.readyOnce.Do(func() {
			close(client.readyChan)
		})
	}

	// Verify channel is closed
	select {
	case <-client.Ready():
		// Expected: closed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Ready channel should be closed")
	}
}

// TestWaitForResponse_Success verifies successful response waiting
// TestLogin_NoToken verifies Login fails immediately without token
func TestLogin_NoToken(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "", nil)
	ctx := context.Background()

	err := client.Login(ctx)
	if err == nil {
		t.Fatal("Expected error when logging in without password")
	}

	if err.Error() != "no password available" {
		t.Errorf("Expected 'no password available' error, got: %v", err)
	}
}

// TestRegister_TokenUpdate verifies token is saved after successful registration
// TestStateManagement verifies state is properly initialized
func TestStateManagement(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	if client.state == nil {
		t.Fatal("State should be initialized")
	}

	if client.state.MaxCargo != 10 {
		t.Errorf("Expected MaxCargo=10, got %d", client.state.MaxCargo)
	}

	if client.state.Doc != true {
		t.Error("Expected initial Doc=true")
	}

	if len(client.state.System.POIs) != 0 {
		t.Error("Expected empty POIs initially")
	}
}

// TestClientInitialization verifies proper client initialization
func TestClientInitialization(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	if client.url != "wss://test.example.com" {
		t.Errorf("Expected url='wss://test.example.com', got %s", client.url)
	}

	if client.username != "testuser" {
		t.Errorf("Expected username='testuser', got %s", client.username)
	}

	if client.password != "testtoken" {
		t.Errorf("Expected password='testtoken', got %s", client.password)
	}

	if client.readyChan == nil {
		t.Fatal("readyChan should be initialized")
	}

	if client.router == nil {
		t.Fatal("router should be initialized")
	}

	if client.stopCh == nil {
		t.Fatal("stopCh should be initialized")
	}
}

// TestTravel_BlocksUntilArrived verifies that Travel() blocks until
// state.Traveling becomes false and returns the arrived POI.
func TestTravel_BlocksUntilArrived(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	// Override send to capture the message without a real WebSocket
	var sentMsg protocol.Message
	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		sentMsg = msg
		return nil
	}

	targetPOI := "poi_asteroid_belt_1"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simulate the server response flow in a goroutine:
	// 1. Initial OK with action:"travel" and arrival_tick (travel accepted)
	// 2. State update setting Traveling=false and CurrentPOI (arrived)
	go func() {
		// Wait for Travel() to send the message and register waiters
		time.Sleep(100 * time.Millisecond)

		// Simulate initial OK response — sets Traveling=true
		client.mu.Lock()
		client.state.Traveling = true
		client.mu.Unlock()

		// Deliver the OK via the router.
		client.router.dispatch(protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"action":       "travel",
				"arrival_tick": float64(5),
			},
		})

		// Simulate arrival after a short delay
		time.Sleep(300 * time.Millisecond)
		client.mu.Lock()
		client.state.Traveling = false
		client.state.CurrentPOI = targetPOI
		client.mu.Unlock()
	}()

	result, err := client.Travel(ctx, targetPOI)
	if err != nil {
		t.Fatalf("Travel() returned error: %v", err)
	}

	if sentMsg.Type != "travel" {
		t.Errorf("expected sent message type 'travel', got %q", sentMsg.Type)
	}

	if result.POI != targetPOI {
		t.Errorf("expected POI %q, got %q", targetPOI, result.POI)
	}
	if result.Canceled {
		t.Error("expected Canceled=false")
	}
}

// TestTravel_TimeoutReturnsError verifies Travel() returns an error
// if the state never transitions.
func TestTravel_TimeoutReturnsError(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Simulate server accepting travel but never arriving
	go func() {
		time.Sleep(100 * time.Millisecond)
		client.mu.Lock()
		client.state.Traveling = true
		client.mu.Unlock()

		client.router.dispatch(protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"action":       "travel",
				"arrival_tick": float64(1),
			},
		})
		// Never set Traveling=false — should timeout
	}()

	_, err := client.Travel(ctx, "poi_nowhere")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestTravel_AlreadyAtDestination verifies Travel() returns immediately
// when server says already_there.
func TestTravel_AlreadyAtDestination(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.CurrentPOI = "poi_station_1"

	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)
		client.router.dispatch(protocol.Response{
			Type: protocol.TypeError,
			Payload: map[string]any{
				"code": "already_there",
			},
		})
	}()

	result, err := client.Travel(ctx, "poi_station_1")
	if err != nil {
		t.Fatalf("Travel() returned error: %v", err)
	}
	if result.POI != "poi_station_1" {
		t.Errorf("expected POI 'poi_station_1', got %q", result.POI)
	}
}

// TestJump_BlocksUntilArrived verifies that Jump() blocks until
// state.Traveling becomes false and returns the new system info.
func TestJump_BlocksUntilArrived(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)

		client.mu.Lock()
		client.state.Traveling = true
		client.mu.Unlock()

		client.router.dispatch(protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"action":       "jump",
				"arrival_tick": float64(3),
			},
		})

		// Simulate jump completion
		time.Sleep(300 * time.Millisecond)
		client.mu.Lock()
		client.state.Traveling = false
		client.state.System.ID = "crimson"
		client.state.System.Name = "Crimson"
		client.state.CurrentPOI = "jump_gate_1"
		client.mu.Unlock()
	}()

	result, err := client.Jump(ctx, "crimson")
	if err != nil {
		t.Fatalf("Jump() returned error: %v", err)
	}
	if result.SystemID != "crimson" {
		t.Errorf("expected SystemID 'crimson', got %q", result.SystemID)
	}
	if result.SystemName != "Crimson" {
		t.Errorf("expected SystemName 'Crimson', got %q", result.SystemName)
	}
	if result.Canceled {
		t.Error("expected Canceled=false")
	}
}

// TestJump_TimeoutReturnsError verifies Jump() returns an error on timeout.
func TestJump_TimeoutReturnsError(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)
		client.mu.Lock()
		client.state.Traveling = true
		client.mu.Unlock()

		client.router.dispatch(protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"action":       "jump",
				"arrival_tick": float64(1),
			},
		})
	}()

	_, err := client.Jump(ctx, "unknown_system")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// recvFrame mimics the WebSocket receive loop: it updates state via
// handleResponse, then fans the frame out through the router (the same
// order as the real readLoop at client.go ~1967). Tests that need the
// state parsers to drive Traveling/Doc/System must use this rather than
// dispatching to the router alone.
func (c *Client) recvFrame(resp protocol.Response) {
	c.handleResponse(resp)
	c.router.dispatch(resp)
}

// TestJump_WhileDocked_AutoUndockNotMistakenForArrival reproduces the bug
// where jumping while docked returned a false-positive success. When docked,
// the server inserts a documented auto-undock step (costs one extra tick,
// carries auto_undocked) BEFORE the genuine jump confirmation:
//
//	1. OK {pending:true}                          — queued ack (no action)
//	2. OK {action:"undock", auto_undocked:true}   — auto-undock side effect
//	3. OK {action:"jump", arrival_tick:N}         — genuine jump confirmation
//	4. action_result {arrived, system_id:...}     — arrival
//
// waitForInitialResponse must NOT treat frame 2 as the jump's initial
// response: at that point Traveling is still false, so Jump() would call
// waitForStateChange(!Traveling) and return immediately — reporting success
// while the real multi-tick jump is still queued (the next command then
// collides with "another action already pending (jump)").
func TestJump_WhileDocked_AutoUndockNotMistakenForArrival(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.Doc = true
	client.state.System.ID = "haven"
	client.state.System.Name = "Haven"

	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)

		// Frame 1: queued ack — no action field, pending:true.
		client.recvFrame(protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"pending": true,
				"message": "Jump action pending. Will execute on next tick.",
			},
		})

		// Frame 2: auto-undock side effect. Must NOT end the jump wait.
		time.Sleep(50 * time.Millisecond)
		client.recvFrame(protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"action":        "undock",
				"auto_undocked": true,
				"message":       "Automatically undocked (required for jump)",
			},
		})

		// Frame 3: genuine jump confirmation — sets Traveling=true. Delayed
		// well past waitForStateChange's 500ms poll interval so that, if Jump
		// mistook the auto-undock frame for arrival, its first poll observes
		// Traveling=false (still in origin) and returns prematurely.
		time.Sleep(900 * time.Millisecond)
		client.recvFrame(protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"action":       "jump",
				"arrival_tick": float64(3),
			},
		})

		// Frame 4: arrival action_result — clears Traveling, sets system.
		time.Sleep(300 * time.Millisecond)
		client.recvFrame(protocol.Response{
			Type: protocol.TypeActionResult,
			Payload: map[string]any{
				"command": "jump",
				"tick":    float64(4),
				"result": map[string]any{
					"action":    "arrived",
					"system_id": "trader_rest",
					"system":    "Trader's Rest",
					"poi":       "jump_gate_1",
				},
			},
		})
	}()

	result, err := client.Jump(ctx, "trader_rest")
	if err != nil {
		t.Fatalf("Jump() returned error: %v", err)
	}
	// Without the fix, Jump returns after frame 2 while still in the origin
	// system — SystemID would be "haven" (or empty), never the destination.
	if result.SystemID != "trader_rest" {
		t.Errorf("Jump returned before real arrival: SystemID=%q, want %q (auto-undock frame was mistaken for arrival)", result.SystemID, "trader_rest")
	}
	if client.GetState().Traveling {
		t.Error("expected Traveling=false after jump completed")
	}
}

// TestBattle_PlainOKTerminates reproduces the hang where `battle retreat`
// timed out despite the server replying. Battle subactions (retreat, stance,
// target) reply with a plain non-pending TypeOK ("Retreating from the
// enemy.") rather than a TypeActionResult, so the default terminateOnAction
// never resolved them and the command timed out — blocking further actions.
func TestBattle_PlainOKTerminates(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- c.Battle(ctx, "retreat", nil) }()

	sent := <-sendCh
	if sent.Type != "battle" {
		t.Fatalf("sent type = %q, want %q", sent.Type, "battle")
	}

	// Server's synchronous retreat reply: a plain, non-pending OK.
	c.router.dispatch(protocol.Response{
		Type:      protocol.TypeOK,
		RequestID: sent.RequestID,
		Payload:   map[string]any{"action": "retreat", "message": "Retreating from the enemy."},
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Battle returned error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Battle did not return: plain OK was not treated as terminal")
	}
}

// TestBattle_ErrorStillTerminates ensures the battle terminator still surfaces
// server errors rather than swallowing them.
func TestBattle_ErrorStillTerminates(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- c.Battle(ctx, "engage", nil) }()

	sent := <-sendCh
	c.router.dispatch(protocol.Response{
		Type:      protocol.TypeActionError,
		RequestID: sent.RequestID,
		Payload:   map[string]any{"code": "not_in_battle", "message": "You are not in a battle."},
	})

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Battle should have returned the server error")
		}
	case <-ctx.Done():
		t.Fatal("Battle did not return on action_error")
	}
}

// TestBattleEventsHandled verifies the battle_started / battle_update /
// battle_damage push events update combat state and emit player sightings,
// rather than falling through to the unhandled-type path.
func TestBattleEventsHandled(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.Player.ID = "me-123"
	client.state.System.ID = "moonshadow"

	var mu sync.Mutex
	var seen []ObservedPlayer
	client.SetPlayerObserver(func(obs []ObservedPlayer) {
		mu.Lock()
		seen = append(seen, obs...)
		mu.Unlock()
	})
	seenCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(seen)
	}

	// battle_started: player is a participant (side_id is an integer).
	client.recvFrame(protocol.Response{
		Type: protocol.TypeBattleStarted,
		Payload: map[string]any{
			"battle_id": "b1",
			"system_id": "moonshadow",
			"participants": []any{
				map[string]any{"player_id": "p1", "ship_class": "close_enough", "ship_name": "Close Enough", "side_id": float64(1), "stance": "fire", "username": "Apex-Blade", "zone": "outer"},
				map[string]any{"player_id": "me-123", "ship_class": "surplus", "ship_name": "Surplus", "side_id": float64(2), "stance": "fire", "username": "Me", "zone": "outer"},
			},
			"sides": []any{
				map[string]any{"player_count": float64(1), "side_id": float64(1)},
				map[string]any{"player_count": float64(1), "side_id": float64(2)},
			},
		},
	})

	if st := client.GetState(); !st.InCombat || !st.InBattle {
		t.Errorf("battle_started: want InCombat && InBattle, got InCombat=%v InBattle=%v", st.InCombat, st.InBattle)
	}
	if got := seenCount(); got != 2 {
		t.Errorf("battle_started: expected 2 player sightings, got %d", got)
	}

	// battle_damage targeting us records the damage taken.
	client.recvFrame(protocol.Response{
		Type: protocol.TypeBattleDamage,
		Payload: map[string]any{
			"attacker_id": "p1", "attacker_name": "Apex-Blade",
			"target_id": "me-123", "target_name": "Me",
			"damage_type": "kinetic", "hit_success": true,
			"hull_hit": float64(12), "shield_hit": float64(3),
			"total_damage": float64(15), "weapons_fired": []any{"Railgun II"},
			"tick": float64(917152),
		},
	})
	if st := client.GetState(); st.LastDamage != 15 {
		t.Errorf("battle_damage to self: want LastDamage=15, got %v", st.LastDamage)
	}

	// battle_damage aimed at someone else must NOT change our LastDamage.
	client.recvFrame(protocol.Response{
		Type: protocol.TypeBattleDamage,
		Payload: map[string]any{
			"attacker_id": "me-123", "target_id": "p1",
			"total_damage": float64(99), "hit_success": true,
		},
	})
	if st := client.GetState(); st.LastDamage != 15 {
		t.Errorf("battle_damage to other: LastDamage should stay 15, got %v", st.LastDamage)
	}

	// battle_update: authoritative snapshot (participants carry hull/shield pct).
	client.recvFrame(protocol.Response{
		Type: protocol.TypeBattleUpdate,
		Payload: map[string]any{
			"auto_pilot": true, "battle_id": "b1", "tick": float64(917153),
			"participants": []any{
				map[string]any{"hull_pct": float64(100), "player_id": "p1", "shield_pct": float64(90), "ship_class": "close_enough", "side_id": float64(1), "stance": "fire", "username": "Apex-Blade", "zone": "mid"},
			},
			"sides":          []any{map[string]any{"player_count": float64(2), "side_id": float64(1)}},
			"your_side_id":   float64(2),
			"your_stance":    "fire",
			"your_target_id": "p1",
			"your_zone":      "outer",
		},
	})
	if st := client.GetState(); !st.InCombat || !st.InBattle {
		t.Error("battle_update: want InCombat && InBattle to remain true")
	}
	if got := seenCount(); got != 3 {
		t.Errorf("battle_update: expected 3 total sightings, got %d", got)
	}
}

// TestJSON_RoundTrip verifies Response JSON marshaling/unmarshaling
func TestJSON_RoundTrip(t *testing.T) {
	original := protocol.Response{
		Type: protocol.TypeLoggedIn,
		Payload: map[string]any{
			"username": "testuser",
			"password": "abc123",
			"player": map[string]any{
				"credits": float64(1000),
			},
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var decoded protocol.Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify
	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: expected %s, got %s", original.Type, decoded.Type)
	}

	if decoded.Payload["username"] != "testuser" {
		t.Error("Username not preserved")
	}

	if decoded.Payload["password"] != "abc123" {
		t.Error("Token not preserved")
	}
}

func TestGetBattleStatus_PopulatesBattleState(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.Player.ID = "me-123"

	client.recvFrame(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action":         "get_battle_status",
			"battle_id":      "b1",
			"system_id":      "ross_128",
			"is_participant": true,
			"participants": []any{
				map[string]any{"player_id": "me-123", "username": "Me", "ship_class": "axiom", "side_id": "1", "zone": "mid", "stance": "fire", "hull_pct": float64(80), "shield_pct": float64(50), "target_id": "p2"},
				map[string]any{"player_id": "p2", "username": "Foe", "ship_class": "axiom", "side_id": "2", "zone": "mid", "stance": "brace", "hull_pct": float64(100), "shield_pct": float64(100)},
			},
		},
	})

	st := client.GetState()
	if st.BattleState == nil {
		t.Fatal("BattleState nil; want populated")
	}
	if st.BattleState.BattleID != "b1" || len(st.BattleState.Participants) != 2 {
		t.Fatalf("unexpected BattleState: %+v", st.BattleState)
	}
	if st.BattleState.Participants[0].HullPct != 80 {
		t.Errorf("want HullPct 80, got %d", st.BattleState.Participants[0].HullPct)
	}
	if st.BattleState.Participants[0].TargetID != "p2" {
		t.Errorf("want TargetID p2, got %q", st.BattleState.Participants[0].TargetID)
	}
	if !st.InBattle {
		t.Error("want InBattle true")
	}
}

func TestGetBattleStatus_ClearsInBattleWhenNotParticipant(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.InBattle = true

	client.recvFrame(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action":         "get_battle_status",
			"battle_id":      "b1",
			"is_participant": false,
			"participants": []any{
				map[string]any{"player_id": "p2", "side_id": "2", "hull_pct": float64(100)},
			},
		},
	})

	if client.GetState().InBattle {
		t.Error("want InBattle false after a non-participant status poll")
	}
}

// A get_battle_status reply that omits "action" must still land. Both the
// BattleState parse and the raw store used to be reachable only through the
// action switch, which is the failure already recorded on
// browse_ships/owned_ships: one live reply without the field and every
// consumer sees an empty battle picture, which reads as "the fight is over".
func TestGetBattleStatus_DetectedByShapeWithoutAction(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.Player.ID = "me-123"

	client.recvFrame(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"battle_id":      "b7",
			"system_id":      "ross_128",
			"is_participant": true,
			"participants": []any{
				map[string]any{"player_id": "me-123", "side_id": "1", "zone": "outer", "hull_pct": float64(90)},
				map[string]any{"player_id": "beast", "side_id": "2", "zone": "outer", "hull_pct": float64(100)},
			},
		},
	})

	st := client.GetState()
	if st.BattleState == nil {
		t.Fatal("BattleState nil; an action-less get_battle_status reply must still be parsed")
	}
	if st.BattleState.BattleID != "b7" || len(st.BattleState.Participants) != 2 {
		t.Fatalf("unexpected BattleState: %+v", st.BattleState)
	}
	if !st.InBattle {
		t.Error("want InBattle true")
	}
	if len(client.GetRawJSON("battle_status")) == 0 {
		t.Error(`GetRawJSON("battle_status") empty; the raw store must be reachable by shape too`)
	}
}

// Once the fight ends the server answers with a bare "not a participant".
// Returning early on that reply left the last live snapshot in place, so a
// loop polling for the battle to end never saw it end.
func TestGetBattleStatus_BareNotParticipantResolvesTheBattle(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.InBattle = true
	client.state.BattleState = &BattleState{BattleID: "b7", IsParticipant: true}

	client.recvFrame(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"action": "get_battle_status", "is_participant": false},
	})

	st := client.GetState()
	if st.InBattle {
		t.Error("want InBattle false")
	}
	if st.BattleState != nil && st.BattleState.IsParticipant {
		t.Error("want the cached battle picture marked resolved, not left live")
	}
}

// TestParseActionResult_CloakTogglesState verifies that a cloak action_result
// (which omits the "action" field and keys off command + an enabled flag)
// updates the cached Player.IsCloaked rather than being logged as unhandled.
func TestParseActionResult_CloakTogglesState(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	client.parseActionResult(map[string]any{
		"command": "cloak",
		"tick":    float64(1085943),
		"result": map[string]any{
			"enabled":        true,
			"cloak_strength": float64(40),
			"message":        "Cloaking device engaged.",
		},
	})
	if !client.GetState().Player.IsCloaked {
		t.Fatal("expected IsCloaked=true after cloak engage")
	}

	client.parseActionResult(map[string]any{
		"command": "cloak",
		"result": map[string]any{
			"enabled": false,
			"message": "Cloaking device disengaged.",
		},
	})
	if client.GetState().Player.IsCloaked {
		t.Error("expected IsCloaked=false after cloak disengage")
	}
}

// TestParseActionResult_DockRecordsBase verifies that docking records WHERE we
// docked, not just that we are docked. docked_at_base only ever arrives on a
// full player payload, so a worker that logs in undocked and then docks used to
// sit at a station with DockedAtBase == "". Everything keyed off that id —
// handoff passes, mission station pins — then silently did nothing.
func TestParseActionResult_DockRecordsBase(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	client.parseActionResult(map[string]any{
		"command": "dock",
		"tick":    float64(1460001),
		"result": map[string]any{
			"action": "dock",
			"base": map[string]any{
				"id":        "grand_exchange_station",
				"name":      "Grand Exchange Station",
				"poi_id":    "grand_exchange",
				"system_id": "haven",
			},
		},
	})
	if st := client.GetState(); !st.Doc || st.Player.DockedAtBase != "grand_exchange_station" {
		t.Fatalf("after dock want doc=true base=grand_exchange_station, got doc=%t base=%q", st.Doc, st.Player.DockedAtBase)
	}

	client.parseActionResult(map[string]any{
		"command": "undock",
		"result":  map[string]any{"action": "undock"},
	})
	if st := client.GetState(); st.Doc || st.Player.DockedAtBase != "" {
		t.Errorf("after undock want doc=false base empty, got doc=%t base=%q", st.Doc, st.Player.DockedAtBase)
	}
}

// TestMergeSystemData_EmpireDoesNotBleedAcrossSystems guards against
// get_system's "empire" field (regional space affiliation) persisting on
// c.state.System.Empire after the ship moves to a system that omits it.
// Prior to the fix, the non-empty guard on line ~3492 never reset Empire
// between systems, so a system with a real empire value followed by one
// that omits the field (or belongs to a different, unset region) would
// keep showing the previous system's empire.
func TestMergeSystemData_EmpireDoesNotBleedAcrossSystems(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	// First system: has an empire value.
	client.handleResponse(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"system": map[string]any{
				"id":     "sys-a",
				"name":   "Alpha",
				"empire": "crimson",
			},
		},
	})
	if got := client.GetState().System.Empire; got != "crimson" {
		t.Fatalf("after first system, Empire = %q, want %q", got, "crimson")
	}

	// Ship moves to a different system that omits the empire field entirely.
	client.handleResponse(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"system": map[string]any{
				"id":   "sys-b",
				"name": "Beta",
			},
		},
	})
	if got := client.GetState().System.Empire; got != "" {
		t.Errorf("after moving to a different system omitting empire, Empire = %q, want \"\" (must not bleed from previous system)", got)
	}
}

// TestMergeSystemData_EmpirePreservedOnSameSystemPartialUpdate ensures the
// fix above did not regress the original purpose of the non-empty guard:
// a partial get_system update for the SAME system that happens to omit the
// empire field (a common shape) must not clear an already-known value.
func TestMergeSystemData_EmpirePreservedOnSameSystemPartialUpdate(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	client.handleResponse(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"system": map[string]any{
				"id":     "sys-a",
				"name":   "Alpha",
				"empire": "crimson",
			},
		},
	})
	if got := client.GetState().System.Empire; got != "crimson" {
		t.Fatalf("after first update, Empire = %q, want %q", got, "crimson")
	}

	// A second update for the SAME system, omitting empire (e.g. a partial
	// refresh) — must preserve the previously known value.
	client.handleResponse(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"system": map[string]any{
				"id":   "sys-a",
				"name": "Alpha",
			},
		},
	})
	if got := client.GetState().System.Empire; got != "crimson" {
		t.Errorf("after same-system partial update omitting empire, Empire = %q, want %q (preserved)", got, "crimson")
	}
}

// Standings ride only on a FULL player payload. Many responses carry a partial
// one, and `c.state.Player = player` would replace a populated standings map
// with nil — silently erasing the pirate baseline that gates stronghold access
// and selects a mission worker's smuggling policy. Same failure shape as the
// Skills merge this sits beside.
func TestParsePlayerData_PartialPayloadKeepsStandings(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	client.parsePlayerData(map[string]any{"player": map[string]any{
		"id": "p1", "username": "testuser", "credits": float64(1000),
		"standings": map[string]any{
			"pirates":  map[string]any{"reputation": float64(10), "baseline": float64(10)},
			"solarian": map[string]any{"reputation": float64(20), "baseline": float64(20)},
		},
	}})
	if got := client.GetState().Player.Standings["pirates"].Baseline; got != 10 {
		t.Fatalf("pirate baseline after full payload = %d, want 10", got)
	}

	// A later partial payload (credits only) must not wipe it.
	client.parsePlayerData(map[string]any{"player": map[string]any{
		"id": "p1", "username": "testuser", "credits": float64(1234),
	}})
	st := client.GetState()
	if st.Credits != 1234 {
		t.Errorf("credits should still update from the partial payload, got %v", st.Credits)
	}
	if got := st.Player.Standings["pirates"].Baseline; got != 10 {
		t.Errorf("pirate baseline after partial payload = %d, want 10 preserved", got)
	}
}
