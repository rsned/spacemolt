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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %s - %s", resp.Status, string(body))
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

// Login authenticates with the MCP server
func (c *MCPClient) Login(username, password string) (string, error) {
	params := map[string]any{
		"username": username,
		"password": password,
	}

	result, err := c.call("login", params)
	if err != nil {
		return "", fmt.Errorf("login failed: %w", err)
	}

	var response struct {
		SessionID string `json:"session_id"`
		Username  string `json:"username"`
	}

	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("failed to parse login response: %w", err)
	}

	return response.SessionID, nil
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

	// Try to get tools list
	fmt.Println("\n📋 Getting tools list...")
	toolsResult, err := client.callProtocol("tools/list", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get tools list: %v\n", err)
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
		params := map[string]any{"session_id": sessionID}

		result, err := client.call(method, params)
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
		params := map[string]any{}
		for k, v := range action.params {
			params[k] = v
		}
		params["session_id"] = sessionID

		result, err := client.call(method, params)
		if err != nil {
			fmt.Printf(" ✗ Error: %v\n", err)
			// Save error responses too
			errorResp := map[string]any{"error": err.Error(), "params": params}
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
		method := fmt.Sprintf("captains_log_get_index_%d", i)
		fmt.Printf("  Calling: captains_log_get (index=%d)...", i)

		params := map[string]any{
			"session_id": sessionID,
			"index":      i,
		}

		result, err := client.call("captains_log_get", params)
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
	result, err := client.call("help", map[string]any{"session_id": sessionID})
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
