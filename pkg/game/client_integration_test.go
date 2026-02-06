package game

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/internal/protocol"
)

// mockServer represents a mock WebSocket server for testing
type mockServer struct {
	server       *httptest.Server
	url          string
	mu           sync.Mutex
	connections  []*websocket.Conn
	sentMessages []protocol.Message
	handlers     map[string]func(protocol.Message) protocol.Response
}

// newMockServer creates a new mock WebSocket server
func newMockServer() *mockServer {
	ms := &mockServer{
		connections:  make([]*websocket.Conn, 0),
		sentMessages: make([]protocol.Message, 0),
		handlers:     make(map[string]func(protocol.Message) protocol.Response),
	}

	// Set up default handlers
	ms.setupDefaultHandlers()

	// Create HTTP server
	ms.server = httptest.NewServer(http.HandlerFunc(ms.handleWebSocket))
	ms.url = "ws" + strings.TrimPrefix(ms.server.URL, "http")

	return ms
}

// setupDefaultHandlers configures default response handlers
func (ms *mockServer) setupDefaultHandlers() {
	// Welcome message handler (sent on connect)
	ms.handlers["connect"] = func(msg protocol.Message) protocol.Response {
		return protocol.Response{
			Type: protocol.TypeWelcome,
			Payload: map[string]any{
				"version":      "test-v1.0",
				"current_tick": float64(1000),
			},
		}
	}

	// Login handler
	ms.handlers["login"] = func(msg protocol.Message) protocol.Response {
		username, _ := msg.Payload["username"].(string)
		token, _ := msg.Payload["password"].(string)

		if token == "valid_token" {
			return protocol.Response{
				Type: protocol.TypeLoggedIn,
				Payload: map[string]any{
					"username": username,
					"player": map[string]any{
						"credits":        float64(1000),
						"current_poi":    "test_station",
						"current_system": "test_system",
					},
					"ship": map[string]any{
						"fuel":     float64(100),
						"max_fuel": float64(100),
						"hull":     float64(100),
						"max_hull": float64(100),
					},
				},
			}
		}

		return protocol.Response{
			Type: protocol.TypeError,
			Payload: map[string]any{
				"message": "invalid token",
			},
		}
	}

	// Register handler
	ms.handlers["register"] = func(msg protocol.Message) protocol.Response {
		username, _ := msg.Payload["username"].(string)
		empire, _ := msg.Payload["empire"].(string)

		if username == "taken_username" {
			return protocol.Response{
				Type: protocol.TypeError,
				Payload: map[string]any{
					"message": "username already taken",
				},
			}
		}

		return protocol.Response{
			Type: protocol.TypeRegistered,
			Payload: map[string]any{
				"password": "new_token_" + username,
				"username": username,
				"empire":   empire,
			},
		}
	}
}

// handleWebSocket handles WebSocket connections
func (ms *mockServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	ms.mu.Lock()
	ms.connections = append(ms.connections, conn)
	ms.mu.Unlock()

	// Send welcome message
	if handler, ok := ms.handlers["connect"]; ok {
		resp := handler(protocol.Message{})
		ms.sendResponse(r.Context(), conn, resp)
	}

	// Handle messages
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			break
		}

		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		ms.mu.Lock()
		ms.sentMessages = append(ms.sentMessages, msg)
		ms.mu.Unlock()

		// Call handler if exists
		if handler, ok := ms.handlers[msg.Type]; ok {
			resp := handler(msg)
			ms.sendResponse(r.Context(), conn, resp)
		}
	}

	conn.Close(websocket.StatusNormalClosure, "")
}

// sendResponse sends a response to the client
func (ms *mockServer) sendResponse(ctx context.Context, conn *websocket.Conn, resp protocol.Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// close closes the mock server
func (ms *mockServer) close() {
	ms.mu.Lock()
	for _, conn := range ms.connections {
		conn.Close(websocket.StatusNormalClosure, "")
	}
	ms.mu.Unlock()
	ms.server.Close()
}

// setHandler sets a custom handler for a message type
func (ms *mockServer) setHandler(msgType string, handler func(protocol.Message) protocol.Response) {
	ms.mu.Lock()
	ms.handlers[msgType] = handler
	ms.mu.Unlock()
}

// getReceivedMessages returns all messages received by the server
func (ms *mockServer) getReceivedMessages() []protocol.Message {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return append([]protocol.Message{}, ms.sentMessages...)
}

// TestLogin_Success verifies successful login with valid credentials
func TestLogin_Success(t *testing.T) {
	server := newMockServer()
	defer server.close()

	client := NewClient(server.url, "testuser", "valid_token", nil)
	ctx := context.Background()

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for ready
	select {
	case <-client.Ready():
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ready")
	}

	// Login
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Verify state was updated
	state := client.GetState()
	state.Mu.Lock()
	credits := state.Credits
	state.Mu.Unlock()

	if credits != 1000 {
		t.Errorf("Expected credits=1000, got %f", credits)
	}
}

// TestLogin_Failure_InvalidToken verifies login fails with invalid token
func TestLogin_Failure_InvalidToken(t *testing.T) {
	server := newMockServer()
	defer server.close()

	client := NewClient(server.url, "testuser", "invalid_token", nil)
	ctx := context.Background()

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for ready
	<-client.Ready()

	// Login should fail
	err := client.Login(ctx)
	if err == nil {
		t.Fatal("Expected login to fail with invalid token")
	}

	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("Expected error to mention 'invalid token', got: %v", err)
	}
}

// TestLogin_Failure_Timeout verifies login timeout handling
func TestLogin_Failure_Timeout(t *testing.T) {
	server := newMockServer()
	defer server.close()

	// Override login handler to never respond
	server.setHandler("login", func(msg protocol.Message) protocol.Response {
		// Don't send response - simulate timeout
		time.Sleep(15 * time.Second)
		return protocol.Response{}
	})

	client := NewClient(server.url, "testuser", "valid_token", nil)
	ctx := context.Background()

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for ready
	<-client.Ready()

	// Login should timeout
	start := time.Now()
	err := client.Login(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected login to timeout")
	}

	if elapsed < 9*time.Second || elapsed > 12*time.Second {
		t.Errorf("Expected ~10s timeout, took %v", elapsed)
	}

	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

// TestRegister_Success verifies successful registration
func TestRegister_Success(t *testing.T) {
	server := newMockServer()
	defer server.close()

	client := NewClient(server.url, "newuser", "", nil)
	ctx := context.Background()

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for ready
	<-client.Ready()

	// Register
	if err := client.Register(ctx, "voidborn"); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	// Verify token was saved in state
	state := client.GetState()
	state.Mu.Lock()
	stateToken := state.Password
	state.Mu.Unlock()

	if stateToken != "new_token_newuser" {
		t.Errorf("Expected state token 'new_token_newuser', got %s", stateToken)
	}
}

// TestRegister_Failure_UsernameTaken verifies registration fails with taken username
func TestRegister_Failure_UsernameTaken(t *testing.T) {
	server := newMockServer()
	defer server.close()

	client := NewClient(server.url, "taken_username", "", nil)
	ctx := context.Background()

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for ready
	<-client.Ready()

	// Register should fail
	err := client.Register(ctx, "voidborn")
	if err == nil {
		t.Fatal("Expected registration to fail with taken username")
	}

	if !strings.Contains(err.Error(), "already taken") {
		t.Errorf("Expected error about username taken, got: %v", err)
	}
}

// TestRegister_Failure_Timeout verifies registration timeout handling
func TestRegister_Failure_Timeout(t *testing.T) {
	server := newMockServer()
	defer server.close()

	// Override register handler to never respond
	server.setHandler("register", func(msg protocol.Message) protocol.Response {
		time.Sleep(15 * time.Second)
		return protocol.Response{}
	})

	client := NewClient(server.url, "testuser", "", nil)
	ctx := context.Background()

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for ready
	<-client.Ready()

	// Register should timeout
	start := time.Now()
	err := client.Register(ctx, "voidborn")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected registration to timeout")
	}

	if elapsed < 9*time.Second || elapsed > 12*time.Second {
		t.Errorf("Expected ~10s timeout, took %v", elapsed)
	}
}

// TestFullConnectionFlow_HappyPath verifies complete connection lifecycle
func TestFullConnectionFlow_HappyPath(t *testing.T) {
	server := newMockServer()
	defer server.close()

	// Add get_system handler
	server.setHandler("get_system", func(msg protocol.Message) protocol.Response {
		return protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"action": "get_system",
				"system": map[string]any{
					"name":        "Test System",
					"description": "A test system",
				},
				"pois": []any{
					map[string]any{
						"id":   "poi1",
						"name": "Test Station",
						"type": "station",
					},
				},
			},
		}
	})

	client := NewClient(server.url, "testuser", "valid_token", nil)
	ctx := context.Background()

	start := time.Now()

	// 1. Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// 2. Wait for ready
	select {
	case <-client.Ready():
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ready")
	}

	// 3. Login
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// 4. Get system info
	if err := client.GetSystem(ctx); err != nil {
		t.Fatalf("GetSystem failed: %v", err)
	}

	// Wait a bit for the async response to be processed
	time.Sleep(100 * time.Millisecond)

	elapsed := time.Since(start)

	// Verify total time is reasonable
	if elapsed > 2*time.Second {
		t.Errorf("Full flow took %v, expected < 2s", elapsed)
	}

	// Verify state
	state := client.GetState()
	state.Mu.Lock()
	systemName := state.System.Name
	poiCount := len(state.System.POIs)
	state.Mu.Unlock()

	if systemName != "Test System" {
		t.Errorf("Expected system name 'Test System', got '%s'", systemName)
	}

	if poiCount != 1 {
		t.Errorf("Expected 1 POI, got %d", poiCount)
	}
}

// TestFullConnectionFlow_SlowServer verifies handling of slow server responses
func TestFullConnectionFlow_SlowServer(t *testing.T) {
	server := newMockServer()
	defer server.close()

	// Add delay to all responses
	slowDelay := 500 * time.Millisecond
	originalLoginHandler := server.handlers["login"]
	server.setHandler("login", func(msg protocol.Message) protocol.Response {
		time.Sleep(slowDelay)
		return originalLoginHandler(msg)
	})

	client := NewClient(server.url, "testuser", "valid_token", nil)
	ctx := context.Background()

	start := time.Now()

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for ready (welcome message should still be fast)
	<-client.Ready()

	// Login (should succeed despite delay)
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login failed with slow server: %v", err)
	}

	elapsed := time.Since(start)

	// Verify login took at least the delay
	if elapsed < slowDelay {
		t.Errorf("Expected login to take at least %v, took %v", slowDelay, elapsed)
	}

	// But not timeout
	if elapsed > 5*time.Second {
		t.Errorf("Login took too long: %v", elapsed)
	}
}

// TestMessageHandling_StateUpdates verifies state updates from messages
func TestMessageHandling_StateUpdates(t *testing.T) {
	server := newMockServer()
	defer server.close()

	client := NewClient(server.url, "testuser", "valid_token", nil)
	ctx := context.Background()

	// Connect and login
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	<-client.Ready()

	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Verify login response updated state
	state := client.GetState()
	state.Mu.Lock()
	credits := state.Credits
	currentPOI := state.CurrentPOI
	currentSystem := state.CurrentSystem
	fuel := state.Fuel
	hull := state.Hull
	state.Mu.Unlock()

	// Check player state
	if credits != 1000 {
		t.Errorf("Expected credits=1000, got %f", credits)
	}

	if currentPOI != "test_station" {
		t.Errorf("Expected current_poi='test_station', got %s", currentPOI)
	}

	if currentSystem != "test_system" {
		t.Errorf("Expected current_system='test_system', got %s", currentSystem)
	}

	// Check ship state
	if fuel != 100 {
		t.Errorf("Expected fuel=100, got %f", fuel)
	}

	if hull != 100 {
		t.Errorf("Expected hull=100, got %f", hull)
	}
}

// TestConcurrentClients verifies multiple clients can connect simultaneously
func TestConcurrentClients(t *testing.T) {
	server := newMockServer()
	defer server.close()

	numClients := 5
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	ctx := context.Background()

	for i := range numClients {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			username := "user" + string(rune('0'+id))
			client := NewClient(server.url, username, "", nil)

			// Connect
			if err := client.Connect(ctx); err != nil {
				errors <- err
				return
			}
			defer client.Close()

			// Wait for ready
			<-client.Ready()

			// Register
			if err := client.Register(ctx, "voidborn"); err != nil {
				errors <- err
				return
			}

			errors <- nil
		}(i)
	}

	wg.Wait()
	close(errors)

	// Verify all succeeded
	successCount := 0
	for err := range errors {
		if err != nil {
			t.Errorf("Client failed: %v", err)
		} else {
			successCount++
		}
	}

	if successCount != numClients {
		t.Errorf("Expected %d successful clients, got %d", numClients, successCount)
	}
}

// TestDisconnectDuringAuth verifies handling of disconnect during authentication
func TestDisconnectDuringAuth(t *testing.T) {
	server := newMockServer()
	defer server.close()

	// Override login handler to close connection
	server.setHandler("login", func(msg protocol.Message) protocol.Response {
		// Close all connections
		server.mu.Lock()
		for _, conn := range server.connections {
			conn.Close(websocket.StatusInternalError, "simulated disconnect")
		}
		server.mu.Unlock()
		return protocol.Response{}
	})

	client := NewClient(server.url, "testuser", "valid_token", nil)
	ctx := context.Background()

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Wait for ready
	<-client.Ready()

	// Login should fail due to disconnect
	err := client.Login(ctx)
	if err == nil {
		t.Fatal("Expected error when disconnected during login")
	}

	// Should be some kind of error (timeout or connection error)
	t.Logf("Got expected error: %v", err)
}

// TestContextCancellation verifies context cancellation during operations
func TestContextCancellation(t *testing.T) {
	server := newMockServer()
	defer server.close()

	// Make login slow
	server.setHandler("login", func(msg protocol.Message) protocol.Response {
		time.Sleep(5 * time.Second)
		return protocol.Response{Type: protocol.TypeLoggedIn}
	})

	client := NewClient(server.url, "testuser", "valid_token", nil)
	ctx, cancel := context.WithCancel(context.Background())

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for ready
	<-client.Ready()

	// Start login and cancel context
	errorChan := make(chan error, 1)
	go func() {
		errorChan <- client.Login(ctx)
	}()

	// Cancel after 100ms
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Should get context canceled error quickly
	select {
	case err := <-errorChan:
		if err == nil {
			t.Fatal("Expected error from canceled context")
		}
		if err != context.Canceled && !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("Expected context.Canceled, got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for context cancellation")
	}
}

// TestIsConnected verifies connection state tracking
func TestIsConnected(t *testing.T) {
	server := newMockServer()
	defer server.close()

	client := NewClient(server.url, "testuser", "valid_token", nil)
	ctx := context.Background()

	// Initially not connected
	if client.IsConnected() {
		t.Error("Expected not connected initially")
	}

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Should be connected
	if !client.IsConnected() {
		t.Error("Expected to be connected after Connect()")
	}

	// Close
	client.Close()

	// Should not be connected
	if client.IsConnected() {
		t.Error("Expected not connected after Close()")
	}
}

// TestMultipleJSONObjectsInSingleMessage verifies handling of multiple
// concatenated JSON objects in a single WebSocket message
func TestMultipleJSONObjectsInSingleMessage(t *testing.T) {
	ms := &mockServer{
		connections:  make([]*websocket.Conn, 0),
		sentMessages: make([]protocol.Message, 0),
		handlers:     make(map[string]func(protocol.Message) protocol.Response),
	}

	ms.setupDefaultHandlers()
	ms.server = httptest.NewServer(http.HandlerFunc(ms.handleWebSocket))
	ms.url = "ws" + strings.TrimPrefix(ms.server.URL, "http")

	// Override the connect handler to send multiple JSON objects
	ms.handlers["connect"] = func(msg protocol.Message) protocol.Response {
		return protocol.Response{
			Type: protocol.TypeWelcome,
			Payload: map[string]any{
				"version":      "test-v1.0",
				"current_tick": float64(1000),
			},
		}
	}

	// Track received messages
	receivedCount := 0
	var mu sync.Mutex

	client := NewClient(ms.url, "testuser", "valid_token", nil)
	client.SetHandler(&testMessageHandler{
		onMessage: func(resp protocol.Response) {
			mu.Lock()
			receivedCount++
			mu.Unlock()
		},
	})

	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()
	defer ms.close()

	// Wait for ready
	select {
	case <-client.Ready():
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ready")
	}

	// Now manually send multiple JSON objects in a single message
	messages := []protocol.Response{
		{Type: protocol.TypeTick, Payload: map[string]any{"tick": float64(1001)}},
		{Type: protocol.TypeTick, Payload: map[string]any{"tick": float64(1002)}},
		{Type: protocol.TypeTick, Payload: map[string]any{"tick": float64(1003)}},
	}

	// Concatenate all JSON objects into a single message
	var concatenated []byte
	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("Failed to marshal message: %v", err)
		}
		concatenated = append(concatenated, data...)
	}

	ms.mu.Lock()
	if len(ms.connections) > 0 {
		conn := ms.connections[0]
		if err := conn.Write(ctx, websocket.MessageText, concatenated); err != nil {
			t.Fatalf("Failed to send concatenated message: %v", err)
		}
	}
	ms.mu.Unlock()

	// Wait a bit for messages to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify all three messages were received
	mu.Lock()
	finalCount := receivedCount
	mu.Unlock()

	if finalCount < 3 {
		t.Errorf("Expected at least 3 messages received, got %d", finalCount)
	}
}

// testMessageHandler is a simple handler for testing
type testMessageHandler struct {
	onMessage func(resp protocol.Response)
}

func (h *testMessageHandler) OnConnected(state *State) {}
func (h *testMessageHandler) OnMessage(resp protocol.Response) {
	if h.onMessage != nil {
		h.onMessage(resp)
	}
}
func (h *testMessageHandler) OnDisconnected(err error) {}
