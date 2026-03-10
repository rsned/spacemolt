package game

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockMCPServer creates a test HTTP server that responds to MCP requests.
func mockMCPServer(t *testing.T, handler func(req mcpJSONRPCRequest) (any, *mcpJSONRPCError)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}

		var req mcpJSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		result, mcpErr := handler(req)

		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
		}
		if mcpErr != nil {
			resp["error"] = mcpErr
		} else {
			resultJSON, _ := json.Marshal(result)
			resp["result"] = json.RawMessage(resultJSON)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "test-session-123")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// toolResult creates an MCP tool result with text content.
func toolResult(text string) mcpToolResult {
	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: text}},
	}
}

func TestMCPGameClient_Initialize(t *testing.T) {
	server := mockMCPServer(t, func(req mcpJSONRPCRequest) (any, *mcpJSONRPCError) {
		if req.Method == "initialize" {
			return mcpInitializeResult{
				ProtocolVersion: "2025-03-26",
				ServerInfo: struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				}{Name: "spacemolt", Version: "0.201.2"},
			}, nil
		}
		if req.Method == "notifications/initialized" {
			return nil, nil
		}
		return nil, &mcpJSONRPCError{Code: -32601, Message: "unknown method"}
	})
	defer server.Close()

	client := NewMCPGameClient(server.URL, "testuser", "testpass", nil)
	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if !client.IsConnected() {
		t.Error("expected connected after Connect()")
	}

	if client.mcpSessionID != "test-session-123" {
		t.Errorf("expected mcp session 'test-session-123', got %q", client.mcpSessionID)
	}
}

func TestMCPGameClient_Login(t *testing.T) {
	loginResp := map[string]any{
		"session_id": "game-session-456",
		"player": map[string]any{
			"id":             "player-1",
			"username":       "testuser",
			"credits":        1000.0,
			"current_system": "sol",
			"current_poi":    "earth",
			"empire":         "solarian",
		},
		"ship": map[string]any{
			"id":             "ship-1",
			"class_id":       "scout",
			"hull":           100.0,
			"max_hull":       100.0,
			"fuel":           50.0,
			"max_fuel":       100.0,
			"cargo_capacity": 20.0,
		},
	}
	loginJSON, _ := json.Marshal(loginResp)

	var mu sync.Mutex
	toolCalls := []string{}

	server := mockMCPServer(t, func(req mcpJSONRPCRequest) (any, *mcpJSONRPCError) {
		switch req.Method {
		case "initialize":
			return mcpInitializeResult{ProtocolVersion: "2025-03-26"}, nil
		case "notifications/initialized":
			return nil, nil
		case "tools/call":
			params, _ := json.Marshal(req.Params)
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(params, &p)

			mu.Lock()
			toolCalls = append(toolCalls, p.Name)
			mu.Unlock()

			switch p.Name {
			case "login":
				return toolResult(string(loginJSON)), nil
			case "get_status":
				return toolResult(string(loginJSON)), nil
			default:
				return toolResult(`{"message": "ok"}`), nil
			}
		}
		return nil, &mcpJSONRPCError{Code: -32601, Message: "unknown method"}
	})
	defer server.Close()

	client := NewMCPGameClient(server.URL, "testuser", "testpass", nil)
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Verify ready channel is closed.
	select {
	case <-client.Ready():
		// Good.
	default:
		t.Error("Ready channel should be closed after Login")
	}

	// Verify state was populated.
	state := client.GetState()
	if state.Player.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %q", state.Player.Username)
	}
	if state.Credits != 1000.0 {
		t.Errorf("expected credits 1000, got %f", state.Credits)
	}
	if state.CurrentSystem != "sol" {
		t.Errorf("expected system 'sol', got %q", state.CurrentSystem)
	}
	if state.Ship.Hull != 100.0 {
		t.Errorf("expected hull 100, got %f", state.Ship.Hull)
	}
	if client.sessionID != "game-session-456" {
		t.Errorf("expected session 'game-session-456', got %q", client.sessionID)
	}
}

func TestMCPGameClient_Travel(t *testing.T) {
	travelResp := map[string]any{
		"player": map[string]any{
			"id":             "player-1",
			"username":       "testuser",
			"current_system": "sol",
			"current_poi":    "mars",
		},
		"ship": map[string]any{
			"id":       "ship-1",
			"fuel":     40.0,
			"max_fuel": 100.0,
		},
	}
	travelJSON, _ := json.Marshal(travelResp)

	server := mockMCPServer(t, func(req mcpJSONRPCRequest) (any, *mcpJSONRPCError) {
		switch req.Method {
		case "initialize":
			return mcpInitializeResult{ProtocolVersion: "2025-03-26"}, nil
		case "notifications/initialized":
			return nil, nil
		case "tools/call":
			params, _ := json.Marshal(req.Params)
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(params, &p)

			switch p.Name {
			case "login":
				loginJSON, _ := json.Marshal(map[string]any{"session_id": "s1"})
				return toolResult(string(loginJSON)), nil
			case "travel":
				// Verify target_poi was passed.
				if poi, ok := p.Arguments["target_poi"]; !ok || poi != "mars" {
					return nil, &mcpJSONRPCError{Code: -1, Message: fmt.Sprintf("expected target_poi=mars, got %v", poi)}
				}
				return toolResult(string(travelJSON)), nil
			case "get_status":
				return toolResult(string(travelJSON)), nil
			}
			return toolResult(`{"message": "ok"}`), nil
		}
		return nil, nil
	})
	defer server.Close()

	client := NewMCPGameClient(server.URL, "testuser", "testpass", nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	result, err := client.Travel(ctx, "mars")
	if err != nil {
		t.Fatalf("Travel failed: %v", err)
	}
	if result.POI != "mars" {
		t.Errorf("expected POI 'mars', got %q", result.POI)
	}
}

func TestMCPGameClient_SessionRelogin(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	server := mockMCPServer(t, func(req mcpJSONRPCRequest) (any, *mcpJSONRPCError) {
		switch req.Method {
		case "initialize":
			return mcpInitializeResult{ProtocolVersion: "2025-03-26"}, nil
		case "notifications/initialized":
			return nil, nil
		case "tools/call":
			params, _ := json.Marshal(req.Params)
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(params, &p)

			switch p.Name {
			case "login":
				loginJSON, _ := json.Marshal(map[string]any{"session_id": "new-session"})
				return toolResult(string(loginJSON)), nil
			case "get_status":
				mu.Lock()
				callCount++
				count := callCount
				mu.Unlock()

				if count == 1 {
					// First call fails with session_invalid.
					return nil, &mcpJSONRPCError{Code: -32000, Message: "session_invalid"}
				}
				// Second call succeeds after relogin.
				statusJSON, _ := json.Marshal(map[string]any{
					"player": map[string]any{"username": "testuser", "credits": 500.0},
				})
				return toolResult(string(statusJSON)), nil
			}
			return toolResult(`{"message": "ok"}`), nil
		}
		return nil, nil
	})
	defer server.Close()

	client := NewMCPGameClient(server.URL, "testuser", "testpass", nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	// Set initial session.
	client.sessionID = "old-session"

	// GetStatus should fail, trigger relogin, and retry.
	err := client.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus should have succeeded after relogin, got: %v", err)
	}

	if client.sessionID != "new-session" {
		t.Errorf("expected session 'new-session' after relogin, got %q", client.sessionID)
	}
}

func TestParseSSEResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  int64
		wantErr bool
	}{
		{
			name:   "simple data line",
			input:  "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}\n\n",
			wantID: 1,
		},
		{
			name:   "multiple SSE events picks result",
			input:  "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":null}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"final\"}]}}\n\n",
			wantID: 2,
		},
		{
			name:   "notification then result",
			input:  "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"done\"}]}}\n\n",
			wantID: 3,
		},
		{
			name:    "empty body",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no data lines",
			input:   "event: message\n\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := parseSSEResponse(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.ID != tt.wantID {
				t.Errorf("expected ID %d, got %d", tt.wantID, resp.ID)
			}
		})
	}
}

func TestParseToolResultText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "single text content",
			input: `{"content":[{"type":"text","text":"hello world"}]}`,
			want:  "hello world",
		},
		{
			name:  "multiple text blocks",
			input: `{"content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}`,
			want:  "hello world",
		},
		{
			name:    "isError flag",
			input:   `{"content":[{"type":"text","text":"something went wrong"}],"isError":true}`,
			wantErr: true,
		},
		{
			name:  "non-text content ignored",
			input: `{"content":[{"type":"image","text":"ignored"},{"type":"text","text":"kept"}]}`,
			want:  "kept",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseToolResultText(json.RawMessage(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMCPGameClient_BackgroundPoller(t *testing.T) {
	var pollCount int
	var mu sync.Mutex

	server := mockMCPServer(t, func(req mcpJSONRPCRequest) (any, *mcpJSONRPCError) {
		switch req.Method {
		case "initialize":
			return mcpInitializeResult{ProtocolVersion: "2025-03-26"}, nil
		case "notifications/initialized":
			return nil, nil
		case "tools/call":
			params, _ := json.Marshal(req.Params)
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(params, &p)

			if p.Name == "login" {
				loginJSON, _ := json.Marshal(map[string]any{"session_id": "s1"})
				return toolResult(string(loginJSON)), nil
			}
			if p.Name == "get_status" {
				mu.Lock()
				pollCount++
				credits := float64(pollCount * 100)
				mu.Unlock()

				statusJSON, _ := json.Marshal(map[string]any{
					"player":       map[string]any{"username": "testuser", "credits": credits},
					"current_tick": pollCount,
				})
				return toolResult(string(statusJSON)), nil
			}
			return toolResult(`{"message": "ok"}`), nil
		}
		return nil, nil
	})
	defer server.Close()

	// Use a shorter poll interval for testing.
	client := NewMCPGameClient(server.URL, "testuser", "testpass", nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Wait for at least one poll cycle.
	time.Sleep(SleepTick + 2*time.Second)

	mu.Lock()
	count := pollCount
	mu.Unlock()

	if count < 1 {
		t.Errorf("expected at least 1 poll, got %d", count)
	}

	// Verify state was updated by poller.
	state := client.GetState()
	if state.Credits <= 0 {
		t.Error("expected credits to be updated by poller")
	}
}

func TestIsSessionInvalidError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("random error"), false},
		{fmt.Errorf("MCP error: session_invalid"), true},
		{fmt.Errorf("session_expired: please re-login"), true},
		{fmt.Errorf("Not Authenticated"), true},
		{fmt.Errorf("Unauthorized access"), true},
	}

	for _, tt := range tests {
		got := isSessionInvalidError(tt.err)
		if got != tt.want {
			t.Errorf("isSessionInvalidError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestDetectAndParseResponse_JSON(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`
	resp, err := detectAndParseResponse("application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != 1 {
		t.Errorf("expected ID 1, got %d", resp.ID)
	}
}

func TestDetectAndParseResponse_SSE(t *testing.T) {
	body := "data: {\"jsonrpc\":\"2.0\",\"id\":5,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n\n"
	resp, err := detectAndParseResponse("text/event-stream", strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != 5 {
		t.Errorf("expected ID 5, got %d", resp.ID)
	}
}
