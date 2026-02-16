package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	mcpServerURL = "https://game.spacemolt.com/mcp"
	outputDir    = "data/mcp/calls"
)

// Credentials holds login information
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
}

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCPClient handles communication with the MCP server via SSE
type MCPClient struct {
	serverURL   string
	client      *http.Client
	requestID   int
	initialized bool
}

// NewMCPClient creates a new MCP client
func NewMCPClient(serverURL string) *MCPClient {
	return &MCPClient{
		serverURL: serverURL,
		client: &http.Client{
			Timeout: 0, // No timeout for SSE
		},
		requestID:   1,
		initialized: false,
	}
}

// Initialize the MCP session
func (c *MCPClient) Initialize() error {
	params := map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "mcp-scraper",
			"version": "1.0.0",
		},
	}

	_, err := c.call("initialize", params)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	// Send initialized notification as required by MCP protocol
	_, err = c.call("notifications/initialized", map[string]any{})
	if err != nil {
		return fmt.Errorf("notifications/initialized failed: %w", err)
	}

	c.initialized = true
	return nil
}

// call makes a JSON-RPC call via SSE
func (c *MCPClient) call(method string, params map[string]any) (json.RawMessage, error) {
	// For now, just use method as-is (no wrapping, no prefix)
	callMethod := method
	callParams := params

	// Build request
	var paramsRaw json.RawMessage
	if callParams != nil {
		var err error
		paramsRaw, err = json.Marshal(callParams)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.requestID,
		Method:  callMethod,
		Params:  paramsRaw,
	}
	c.requestID++

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request with headers
	httpReq, err := http.NewRequest("POST", c.serverURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// For SSE, we need to POST and read the stream
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the entire response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check if it's an SSE response (starts with "data: ")
	bodyStr := string(body)
	if strings.HasPrefix(bodyStr, "data: ") {
		// Parse SSE format
		lines := strings.Split(bodyStr, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if data, found := strings.CutPrefix(line, "data: "); found {
				// Parse JSON from data line
				var rpcResp JSONRPCResponse
				if err := json.Unmarshal([]byte(data), &rpcResp); err == nil {
					if rpcResp.Error != nil {
						return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
					}
					return rpcResp.Result, nil
				}
			}
		}
		return nil, fmt.Errorf("no valid data event in SSE response")
	}

	// Try as plain JSON response
	// Accept 200 OK and 202 Accepted (for notifications)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("HTTP error: %s - %s", resp.Status, string(body))
	}

	// 202 Accepted typically means the request was accepted but no response body
	if resp.StatusCode == http.StatusAccepted {
		// Return empty result for 202 responses
		return json.RawMessage("{}"), nil
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// Login authenticates with the MCP server using tools/call
func (c *MCPClient) Login(username, password string) (string, error) {
	// Use tools/call to invoke the login tool
	params := map[string]any{
		"name": "login",
		"arguments": map[string]any{
			"username": username,
			"password": password,
		},
	}

	result, err := c.call("tools/call", params)
	if err != nil {
		return "", fmt.Errorf("login failed: %w", err)
	}

	// Parse the MCP tool response - try to extract session_id
	// The login tool returns JSON with session_id field
	var loginResponse struct {
		SessionID string `json:"session_id"`
		Username  string `json:"username"`
		Message   string `json:"message"`
		// There might be other fields we ignore
	}

	// First, try to parse the result directly as JSON
	if err := json.Unmarshal(result, &loginResponse); err != nil {
		// If that fails, try parsing as MCP tool response with content array
		var mcpResponse struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Data any    `json:"data,omitempty"`
			} `json:"content"`
			IsError bool `json:"isError"`
		}

		if err := json.Unmarshal(result, &mcpResponse); err != nil {
			return "", fmt.Errorf("failed to parse login response: %w", err)
		}

		// Check for errors
		if mcpResponse.IsError {
			if len(mcpResponse.Content) > 0 {
				return "", fmt.Errorf("login error: %s", mcpResponse.Content[0].Text)
			}
			return "", fmt.Errorf("login returned error")
		}

		// Try to get data from content
		if len(mcpResponse.Content) == 0 {
			return "", fmt.Errorf("empty login response")
		}

		// If content has data field, try to parse it
		if mcpResponse.Content[0].Data != nil {
			dataJSON, _ := json.Marshal(mcpResponse.Content[0].Data)
			if err := json.Unmarshal(dataJSON, &loginResponse); err != nil {
				// Last resort: try to extract session_id from text
				text := mcpResponse.Content[0].Text
				return extractSessionIDFromText(text)
			}
		} else {
			// Extract from text field
			return extractSessionIDFromText(mcpResponse.Content[0].Text)
		}
	}

	// Successfully parsed the login response
	if loginResponse.SessionID == "" {
		return "", fmt.Errorf("session_id not found in login response")
	}

	return loginResponse.SessionID, nil
}

// extractSessionIDFromText attempts to extract a session_id from text content
func extractSessionIDFromText(text string) (string, error) {
	text = strings.TrimSpace(text)

	// Try to parse as JSON first
	var jsonData map[string]any
	if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
		if sessionID, ok := jsonData["session_id"].(string); ok && sessionID != "" {
			return sessionID, nil
		}
	}

	// Fallback: look for hex strings in the text
	words := strings.Fields(text)
	for _, word := range words {
		word = strings.TrimSuffix(word, ".")
		word = strings.TrimSuffix(word, ",")
		word = strings.TrimSuffix(word, ")")
		word = strings.TrimPrefix(word, "(")
		// Session IDs are 32-character hex strings
		if len(word) == 32 && isHexString(word) {
			return word, nil
		}
	}

	return "", fmt.Errorf("could not extract session_id from: %s", text)
}

// isHexString checks if a string is a valid hex string
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// callProtocol makes a direct MCP protocol call (no prefix, no wrapping)
func (c *MCPClient) callProtocol(method string, params map[string]any) (json.RawMessage, error) {
	// Save current state
	wasInitialized := c.initialized
	c.initialized = false // Force direct call

	result, err := c.call(method, params)

	// Restore state
	c.initialized = wasInitialized

	return result, err
}

// callTool invokes an MCP tool by name
func (c *MCPClient) callTool(toolName string, args map[string]any) (json.RawMessage, error) {
	params := map[string]any{
		"name":      toolName,
		"arguments": args,
	}

	result, err := c.call("tools/call", params)
	if err != nil {
		return nil, err
	}

	// Parse the MCP tool response
	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Data any    `json:"data,omitempty"` // Some tools return data directly
		} `json:"content"`
		IsError bool `json:"isError"`
	}

	if err := json.Unmarshal(result, &response); err != nil {
		// If we can't parse as MCP response, try to extract JSON from text field
		// Some tools return raw JSON in the result
		return result, nil
	}

	// Check for errors
	if response.IsError {
		if len(response.Content) > 0 {
			return nil, fmt.Errorf("tool returned error: %s", response.Content[0].Text)
		}
		return nil, fmt.Errorf("tool returned error (no message)")
	}

	// Extract the actual data from the content
	if len(response.Content) == 0 {
		return result, nil // Return raw response if no content
	}

	// Check if content has data field (some tools return structured data)
	if response.Content[0].Data != nil {
		dataJSON, err := json.Marshal(response.Content[0].Data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal data field: %w", err)
		}
		return json.RawMessage(dataJSON), nil
	}

	// Try to parse JSON from the text field
	text := response.Content[0].Text
	text = strings.TrimSpace(text)

	// Check if it's JSON
	var jsonData any
	if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
		// It's valid JSON, return it
		return json.RawMessage(text), nil
	}

	// Not JSON, return the raw text wrapped in JSON
	wrapped := fmt.Sprintf(`{"text": %s}`, string(jsonEscape(text)))
	return json.RawMessage(wrapped), nil
}

// jsonEscape escapes a string for JSON encoding
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// saveResponse saves a response to a file
func saveResponse(method string, result json.RawMessage) error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Sanitize method name for filename
	filename := strings.ReplaceAll(method, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")
	filename = strings.ReplaceAll(filename, ":", "")
	filename = strings.ToLower(strings.TrimSpace(filename))

	// Save as JSON
	outputPath := filepath.Join(outputDir, filename+".json")
	if err := os.WriteFile(outputPath, result, 0644); err != nil {
		return fmt.Errorf("failed to write response file: %w", err)
	}

	fmt.Printf("  ✓ Saved %s\n", outputPath)
	return nil
}

// All MCP methods that don't require special parameters
var queryMethods = []string{
	// Info and status
	"get_status",
	"get_commands",
	"get_version",
	"get_cargo",
	"get_ship",
	"get_poi",
	"get_system",
	"get_map",
	"get_nearby",
	"get_notifications",
	"get_skills",
	"get_recipes",
	"get_listings",
	"get_trades",
	"get_wrecks",
	"get_base_wrecks",
	"get_drones",
	"get_base",
	"get_base_cost",
	"claim_insurance",

	// Faction
	"faction_info",
	"faction_list",
	"faction_get_invites",

	// Social
	"captains_log_list",

	// Forum
	"forum_list",

	// Search
	"search_systems",
	"find_route",

	// Help
	"help",
}

// Methods that should be called (even if they might fail) for testing
var actionMethods = []struct {
	method string
	params map[string]any
}{
	// Captain's log
	{"captains_log_get", map[string]any{"index": 0}},

	// Faction info with no faction_id (should return own faction)
	{"faction_info", map[string]any{}},

	// Search with query
	{"search_systems", map[string]any{"query": "sol"}},
	{"find_route", map[string]any{"target_system": "alpha_centauri"}},

	// Help with category
	{"help", map[string]any{"category": "movement"}},
	{"help", map[string]any{"command": "mine"}},
}

func main() {
	// Load credentials
	credsPath := "data/agents/random-2/credentials.json"
	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading credentials: %v\n", err)
		os.Exit(1)
	}

	var creds Credentials
	if err := json.Unmarshal(credsData, &creds); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing credentials: %v\n", err)
		os.Exit(1)
	}

	// Create client
	client := NewMCPClient(mcpServerURL)

	fmt.Println("🚀 MCP API Scraper (SSE)")
	fmt.Println("========================")
	fmt.Printf("Server: %s\n", mcpServerURL)
	fmt.Printf("User: %s\n\n", creds.Username)

	// Initialize
	fmt.Println("🔐 Initializing MCP session...")
	if err := client.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Initialize failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Initialized")

	// Try to get tools list (note: may require session)
	fmt.Println("\n📋 Getting tools list...")
	toolsResult, err := client.callProtocol("tools/list", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get tools list: %v (continuing anyway)\n", err)
		// Don't exit - tools/list might not be available without session
	} else {
		fmt.Printf("✓ Got tools list (saved)\n")
		saveResponse("tools_list", toolsResult)
	}
	fmt.Println()

	// Login
	fmt.Println("🔑 Logging in...")
	sessionID, err := client.Login(creds.Username, creds.Password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Session ID: %s\n\n", sessionID)

	// Call all query methods (methods that only need session_id)
	fmt.Println("📡 Calling query methods (methods that only require session_id)...")
	for _, method := range queryMethods {
		fmt.Printf("  Calling: %s...", method)
		args := map[string]any{"session_id": sessionID}

		result, err := client.callTool(method, args)
		if err != nil {
			fmt.Printf(" ✗ Error: %v\n", err)
			// Save error responses too
			errorResp := map[string]any{"error": err.Error()}
			errorJSON, _ := json.MarshalIndent(errorResp, "", "  ")
			saveResponse(method, errorJSON)
			continue
		}

		// Pretty print and save
		var prettyJSON bytes.Buffer
		json.Indent(&prettyJSON, result, "", "  ")
		if err := saveResponse(method, prettyJSON.Bytes()); err != nil {
			fmt.Printf(" ✗ Failed to save: %v\n", err)
		}
	}

	// Call action methods with specific parameters
	fmt.Println("\n📡 Calling action methods (with specific parameters)...")
	for _, action := range actionMethods {
		method := action.method
		fmt.Printf("  Calling: %s...", method)

		// Add session_id to params
		args := map[string]any{}
		for k, v := range action.params {
			args[k] = v
		}
		args["session_id"] = sessionID

		result, err := client.callTool(method, args)
		if err != nil {
			fmt.Printf(" ✗ Error: %v\n", err)
			// Save error responses too
			errorResp := map[string]any{"error": err.Error(), "params": args}
			errorJSON, _ := json.MarshalIndent(errorResp, "", "  ")
			saveResponse(method, errorJSON)
			continue
		}

		// Pretty print and save
		var prettyJSON bytes.Buffer
		json.Indent(&prettyJSON, result, "", "  ")
		if err := saveResponse(method, prettyJSON.Bytes()); err != nil {
			fmt.Printf(" ✗ Failed to save: %v\n", err)
		}
	}

	// Try to get all captain's log entries (multiple indices)
	fmt.Println("\n📜 Fetching captain's log entries (indices 0-9)...")
	for i := range int(10) {
		method := fmt.Sprintf("captions_log_get_index_%d", i)
		fmt.Printf("  Calling: captains_log_get (index=%d)...", i)

		args := map[string]any{
			"session_id": sessionID,
			"index":      i,
		}

		result, err := client.callTool("captains_log_get", args)
		if err != nil {
			// Stop if we get an error (likely no more entries)
			fmt.Printf(" ✗ Error: %v (stopping)\n", err)
			break
		}

		// Pretty print and save
		var prettyJSON bytes.Buffer
		json.Indent(&prettyJSON, result, "", "  ")
		if err := saveResponse(method, prettyJSON.Bytes()); err != nil {
			fmt.Printf(" ✗ Failed to save: %v\n", err)
		}
	}

	// Try help with no parameters (should show all categories)
	fmt.Println("\n📖 Calling help with no parameters...")
	fmt.Printf("  Calling: help (no params)...")
	result, err := client.callTool("help", map[string]any{"session_id": sessionID})
	if err != nil {
		fmt.Printf(" ✗ Error: %v\n", err)
	} else {
		var prettyJSON bytes.Buffer
		json.Indent(&prettyJSON, result, "", "  ")
		saveResponse("help_no_params", prettyJSON.Bytes())
	}

	fmt.Println("\n✅ Done! All responses saved to:", outputDir)

	// Count files
	files, err := os.ReadDir(outputDir)
	if err == nil {
		fmt.Printf("📊 Total files created: %d\n", len(files))
	}
}
