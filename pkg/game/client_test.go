package game

import (
	"context"
	"encoding/json"
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
