package game

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// mcpJSONRPCRequest represents a JSON-RPC 2.0 request for the MCP protocol.
type mcpJSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// mcpJSONRPCResponse represents a JSON-RPC 2.0 response from the MCP server.
type mcpJSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int64            `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *mcpJSONRPCError `json:"error,omitempty"`
}

// mcpJSONRPCError represents a JSON-RPC 2.0 error.
type mcpJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// mcpToolResult represents the result of a tools/call response.
type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// mcpContent represents a content block in an MCP tool result.
type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// mcpInitializeResult represents the result of an MCP initialize response.
type mcpInitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
}

// parseSSEResponse reads an SSE response body and extracts the JSON-RPC response.
// The MCP Streamable HTTP transport sends responses as Server-Sent Events with
// data lines containing JSON-RPC 2.0 response objects.
func parseSSEResponse(body io.Reader) (*mcpJSONRPCResponse, error) {
	scanner := bufio.NewScanner(body)
	// Allow for large responses (up to 10MB).
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var lastData string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			lastData = strings.TrimPrefix(line, "data: ")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	if lastData == "" {
		return nil, fmt.Errorf("no data lines found in SSE response")
	}

	var resp mcpJSONRPCResponse
	if err := json.Unmarshal([]byte(lastData), &resp); err != nil {
		return nil, fmt.Errorf("parsing JSON-RPC response: %w", err)
	}

	return &resp, nil
}

// parseToolResultText extracts the text content from an MCP tool result.
// MCP tools return results as content arrays with type "text".
// Returns the combined text from all text content blocks.
func parseToolResultText(result json.RawMessage) (string, error) {
	var toolResult mcpToolResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return "", fmt.Errorf("parsing tool result: %w", err)
	}

	if toolResult.IsError {
		// Collect error text from content blocks.
		var errTexts []string
		for _, c := range toolResult.Content {
			if c.Type == "text" {
				errTexts = append(errTexts, c.Text)
			}
		}
		return "", fmt.Errorf("tool error: %s", strings.Join(errTexts, "; "))
	}

	var texts []string
	for _, c := range toolResult.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}

	return strings.Join(texts, ""), nil
}

// parseToolResultJSON extracts text content from an MCP tool result and
// unmarshals it as JSON into the provided target.
func parseToolResultJSON(result json.RawMessage, target any) error {
	text, err := parseToolResultText(result)
	if err != nil {
		return err
	}

	if err := json.Unmarshal([]byte(text), target); err != nil {
		return fmt.Errorf("parsing tool result JSON: %w (text: %s)", err, truncate(text, 200))
	}

	return nil
}

// buildJSONRPCRequest creates a JSON-RPC 2.0 request body.
func buildJSONRPCRequest(id int64, method string, params any) ([]byte, error) {
	req := mcpJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	return json.Marshal(req)
}

// isSSEContentType checks if the Content-Type indicates SSE.
func isSSEContentType(contentType string) bool {
	return strings.Contains(contentType, "text/event-stream")
}

// detectAndParseResponse handles both SSE and direct JSON responses based on Content-Type.
func detectAndParseResponse(contentType string, body io.Reader) (*mcpJSONRPCResponse, error) {
	if isSSEContentType(contentType) {
		return parseSSEResponse(body)
	}

	// Buffer the body to detect format.
	data, err := io.ReadAll(io.LimitReader(body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Try direct JSON first.
	var resp mcpJSONRPCResponse
	if err := json.Unmarshal(data, &resp); err == nil && resp.JSONRPC == "2.0" {
		return &resp, nil
	}

	// Try as SSE.
	return parseSSEResponse(bytes.NewReader(data))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
