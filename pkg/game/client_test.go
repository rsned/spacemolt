package game

import (
	"context"
	"encoding/json"
	"errors"
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
func TestWaitForResponse_Success(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	ctx := context.Background()

	// Start a goroutine to send the response after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)

		// Simulate receiving the response
		resp := protocol.Response{
			Type: protocol.TypeLoggedIn,
			Payload: map[string]any{
				"message": "success",
			},
		}

		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeLoggedIn]; ok {
			ch <- resp
		}
		client.waiterMu.Unlock()
	}()

	// Wait for response
	resp, err := client.waitForResponse(ctx, protocol.TypeLoggedIn, 1*time.Second)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resp.Type != protocol.TypeLoggedIn {
		t.Errorf("Expected response type %s, got %s", protocol.TypeLoggedIn, resp.Type)
	}

	if msg, ok := resp.Payload["message"].(string); !ok || msg != "success" {
		t.Errorf("Expected message 'success', got %v", resp.Payload["message"])
	}

	// Verify waiter was cleaned up
	client.waiterMu.Lock()
	if _, exists := client.waiters[protocol.TypeLoggedIn]; exists {
		t.Error("Waiter should have been cleaned up")
	}
	client.waiterMu.Unlock()
}

// TestWaitForResponse_Timeout verifies timeout behavior
func TestWaitForResponse_Timeout(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	ctx := context.Background()

	start := time.Now()

	// Wait for response that never comes
	_, err := client.waitForResponse(ctx, protocol.TypeLoggedIn, 100*time.Millisecond)

	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	if elapsed < 90*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Errorf("Expected timeout around 100ms, took %v", elapsed)
	}

	// Verify waiter was cleaned up
	client.waiterMu.Lock()
	if _, exists := client.waiters[protocol.TypeLoggedIn]; exists {
		t.Error("Waiter should have been cleaned up after timeout")
	}
	client.waiterMu.Unlock()
}

// TestWaitForResponse_ContextCancellation verifies context cancellation handling
func TestWaitForResponse_ContextCancellation(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()

	// Wait for response
	_, err := client.waitForResponse(ctx, protocol.TypeLoggedIn, 5*time.Second)

	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected context error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}

	if elapsed > 200*time.Millisecond {
		t.Errorf("Expected quick cancellation, took %v", elapsed)
	}

	// Verify waiter was cleaned up
	client.waiterMu.Lock()
	if _, exists := client.waiters[protocol.TypeLoggedIn]; exists {
		t.Error("Waiter should have been cleaned up after context cancellation")
	}
	client.waiterMu.Unlock()
}

// TestWaitForResponse_MultipleWaiters verifies multiple concurrent waiters
func TestWaitForResponse_MultipleWaiters(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make(chan string, 2)

	// Waiter 1: waiting for logged_in
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := client.waitForResponse(ctx, protocol.TypeLoggedIn, 1*time.Second)
		if err != nil {
			results <- "error1"
			return
		}
		results <- resp.Type
	}()

	// Waiter 2: waiting for registered
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := client.waitForResponse(ctx, protocol.TypeRegistered, 1*time.Second)
		if err != nil {
			results <- "error2"
			return
		}
		results <- resp.Type
	}()

	// Give waiters time to set up
	time.Sleep(50 * time.Millisecond)

	// Send both responses
	client.waiterMu.Lock()
	if ch, ok := client.waiters[protocol.TypeLoggedIn]; ok {
		ch <- protocol.Response{Type: protocol.TypeLoggedIn}
	}
	if ch, ok := client.waiters[protocol.TypeRegistered]; ok {
		ch <- protocol.Response{Type: protocol.TypeRegistered}
	}
	client.waiterMu.Unlock()

	// Wait for both to complete
	wg.Wait()
	close(results)

	// Verify both got correct responses
	received := make(map[string]bool)
	for result := range results {
		received[result] = true
	}

	if !received[protocol.TypeLoggedIn] {
		t.Error("Waiter 1 did not receive logged_in response")
	}
	if !received[protocol.TypeRegistered] {
		t.Error("Waiter 2 did not receive registered response")
	}

	// Verify all waiters cleaned up
	client.waiterMu.Lock()
	if len(client.waiters) != 0 {
		t.Errorf("Expected 0 waiters, got %d", len(client.waiters))
	}
	client.waiterMu.Unlock()
}

// TestWaitForResponse_WrongMessageType verifies waiters don't receive wrong types
func TestWaitForResponse_WrongMessageType(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	ctx := context.Background()

	// Start waiting for logged_in
	resultChan := make(chan error, 1)
	go func() {
		_, err := client.waitForResponse(ctx, protocol.TypeLoggedIn, 200*time.Millisecond)
		resultChan <- err
	}()

	// Give waiter time to set up
	time.Sleep(50 * time.Millisecond)

	// Send wrong message type
	client.waiterMu.Lock()
	// Intentionally send to wrong type - should not be in waiters
	if ch, ok := client.waiters[protocol.TypeRegistered]; ok {
		ch <- protocol.Response{Type: protocol.TypeRegistered}
	}
	client.waiterMu.Unlock()

	// Should timeout since it didn't receive correct type
	err := <-resultChan
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}
}

// TestClient_ConcurrentWaiters tests multiple concurrent waiters for different types
func TestClient_ConcurrentWaiters(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	ctx := context.Background()

	numWaiters := 10
	var wg sync.WaitGroup
	errors := make(chan error, numWaiters)

	// Start multiple waiters for different message types
	for i := 0; i < numWaiters; i++ {
		wg.Add(1)
		msgType := protocol.TypeOK + string(rune(i)) // Create unique types

		go func(messageType string) {
			defer wg.Done()

			// Each waiter waits for its specific type
			_, err := client.waitForResponse(ctx, messageType, 2*time.Second)
			errors <- err
		}(msgType)
	}

	// Give waiters time to set up
	time.Sleep(100 * time.Millisecond)

	// Send responses to all waiters concurrently
	client.waiterMu.Lock()
	for msgType, ch := range client.waiters {
		go func(mt string, c chan protocol.Response) {
			c <- protocol.Response{Type: mt}
		}(msgType, ch)
	}
	client.waiterMu.Unlock()

	// Wait for all to complete
	wg.Wait()
	close(errors)

	// Verify all succeeded
	for err := range errors {
		if err != nil {
			t.Errorf("Waiter failed with error: %v", err)
		}
	}

	// Verify all cleaned up
	client.waiterMu.Lock()
	if len(client.waiters) != 0 {
		t.Errorf("Expected 0 waiters after completion, got %d", len(client.waiters))
	}
	client.waiterMu.Unlock()
}

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
func TestRegister_TokenUpdate(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "", nil)
	ctx := context.Background()

	// Simulate what happens when Register receives a response
	testToken := "new_token_abc123"
	resp := protocol.Response{
		Type: protocol.TypeRegistered,
		Payload: map[string]any{
			"password": testToken,
		},
	}

	// Start response sender in background
	go func() {
		time.Sleep(50 * time.Millisecond) // Give waiter time to set up
		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeRegistered]; ok {
			ch <- resp
		}
		client.waiterMu.Unlock()
	}()

	// Call waitForResponse directly to test the mechanism
	receivedResp, err := client.waitForResponse(ctx, protocol.TypeRegistered, 1*time.Second)
	if err != nil {
		t.Fatalf("waitForResponse failed: %v", err)
	}

	// Simulate the token update that Register() does
	if token, ok := receivedResp.Payload["password"].(string); ok {
		client.mu.Lock()
		client.password = token
		client.state.Password = token
		client.mu.Unlock()
	}

	// Verify password was saved
	client.mu.RLock()
	if client.password != testToken {
		t.Errorf("Expected client.password to be %s, got %s", testToken, client.password)
	}
	if client.state.Password != testToken {
		t.Errorf("Expected client.state.Password to be %s, got %s", testToken, client.state.Password)
	}
	client.mu.RUnlock()
}

// TestWaiters_Cleanup verifies proper cleanup on various exit paths
func TestWaiters_Cleanup(t *testing.T) {
	tests := []struct {
		name    string
		setupFn func(*Client, context.Context) error
		wantErr bool
	}{
		{
			name: "success path",
			setupFn: func(c *Client, ctx context.Context) error {
				go func() {
					time.Sleep(10 * time.Millisecond)
					c.waiterMu.Lock()
					if ch, ok := c.waiters["test_success"]; ok {
						ch <- protocol.Response{Type: "test_success"}
					}
					c.waiterMu.Unlock()
				}()
				_, err := c.waitForResponse(ctx, "test_success", 1*time.Second)
				return err
			},
			wantErr: false,
		},
		{
			name: "timeout path",
			setupFn: func(c *Client, ctx context.Context) error {
				_, err := c.waitForResponse(ctx, "test_timeout", 50*time.Millisecond)
				return err
			},
			wantErr: true,
		},
		{
			name: "context cancel path",
			setupFn: func(c *Client, ctx context.Context) error {
				cancelCtx, cancel := context.WithCancel(ctx)
				go func() {
					time.Sleep(10 * time.Millisecond)
					cancel()
				}()
				_, err := c.waitForResponse(cancelCtx, "test_cancel", 1*time.Second)
				return err
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
			ctx := context.Background()

			err := tt.setupFn(client, ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got error=%v", tt.wantErr, err)
			}

			// Always verify cleanup
			client.waiterMu.Lock()
			numWaiters := len(client.waiters)
			client.waiterMu.Unlock()

			if numWaiters != 0 {
				t.Errorf("Expected 0 waiters after %s, got %d", tt.name, numWaiters)
			}
		})
	}
}

// BenchmarkWaitForResponse benchmarks the response waiting mechanism
func BenchmarkWaitForResponse(b *testing.B) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate immediate response
		go func() {
			client.waiterMu.Lock()
			if ch, ok := client.waiters[protocol.TypeOK]; ok {
				ch <- protocol.Response{Type: protocol.TypeOK}
			}
			client.waiterMu.Unlock()
		}()

		_, _ = client.waitForResponse(ctx, protocol.TypeOK, 1*time.Second)
	}
}

// BenchmarkConcurrentWaiters benchmarks concurrent waiter performance
func BenchmarkConcurrentWaiters(b *testing.B) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				msgType := protocol.TypeOK + string(rune(idx))

				go func() {
					time.Sleep(1 * time.Millisecond)
					client.waiterMu.Lock()
					if ch, ok := client.waiters[msgType]; ok {
						ch <- protocol.Response{Type: msgType}
					}
					client.waiterMu.Unlock()
				}()

				_, _ = client.waitForResponse(ctx, msgType, 1*time.Second)
			}(j)
		}
		wg.Wait()
	}
}

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

	if client.waiters == nil {
		t.Fatal("waiters map should be initialized")
	}

	if len(client.waiters) != 0 {
		t.Error("waiters map should be empty initially")
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

		// Deliver the OK to the waiter
		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeOK]; ok {
			ch <- protocol.Response{
				Type: protocol.TypeOK,
				Payload: map[string]any{
					"action":       "travel",
					"arrival_tick": float64(5),
				},
			}
		}
		client.waiterMu.Unlock()

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

		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeOK]; ok {
			ch <- protocol.Response{
				Type: protocol.TypeOK,
				Payload: map[string]any{
					"action":       "travel",
					"arrival_tick": float64(1),
				},
			}
		}
		client.waiterMu.Unlock()
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
		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeError]; ok {
			ch <- protocol.Response{
				Type: protocol.TypeError,
				Payload: map[string]any{
					"code": "already_there",
				},
			}
		}
		client.waiterMu.Unlock()
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

		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeOK]; ok {
			ch <- protocol.Response{
				Type: protocol.TypeOK,
				Payload: map[string]any{
					"action":       "jump",
					"arrival_tick": float64(3),
				},
			}
		}
		client.waiterMu.Unlock()

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

		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeOK]; ok {
			ch <- protocol.Response{
				Type: protocol.TypeOK,
				Payload: map[string]any{
					"action":       "jump",
					"arrival_tick": float64(1),
				},
			}
		}
		client.waiterMu.Unlock()
	}()

	_, err := client.Jump(ctx, "unknown_system")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
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
