package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client handles communication with Ollama
type Client struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// Config holds LLM client configuration
type Config struct {
	BaseURL string
	Model   string
	Timeout time.Duration
}

// New creates a new Ollama client
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "llama3.2"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}

	return &Client{
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// DecisionRequest represents a request for an agent decision
type DecisionRequest struct {
	AgentName   string
	Personality string
	CurrentState string
	Knowledge   string
	Experiences string
}

// DecisionResponse represents the LLM's decision
type DecisionResponse struct {
	Action     string  `json:"action"`
	Target     string  `json:"target,omitempty"`
	Reasoning  string  `json:"reasoning"`
	Confidence float64 `json:"confidence"`
}

// Decide prompts the LLM for an action decision
func (c *Client) Decide(ctx context.Context, prompt string) (*DecisionResponse, error) {
	payload := map[string]interface{}{
		"model":  c.model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]interface{}{
			"temperature": 0.7,
			"num_predict": 500,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse Ollama response
	var ollamaResp struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract structured decision from text response
	return c.parseDecision(ollamaResp.Response)
}

// parseDecision extracts structured decision from LLM text response
func (c *Client) parseDecision(text string) (*DecisionResponse, error) {
	// Try to extract JSON from the response
	// Look for JSON blocks in the text
	response := &DecisionResponse{}

	// Simple parsing - look for patterns
	// In production, this would be more robust

	// Extract action
	if err := extractField(text, "action", &response.Action); err != nil {
		return nil, fmt.Errorf("failed to extract action: %w", err)
	}

	// Extract target if present
	_ = extractField(text, "target", &response.Target)

	// Extract reasoning
	if err := extractField(text, "reasoning", &response.Reasoning); err != nil {
		response.Reasoning = text // Fallback to full text
	}

	// Extract confidence
	var confidenceStr string
	if err := extractField(text, "confidence", &confidenceStr); err == nil {
		_, _ = fmt.Sscanf(confidenceStr, "%f", &response.Confidence)
	} else {
		response.Confidence = 0.5 // Default confidence
	}

	return response, nil
}

// extractField extracts a field value from text
func extractField(text, field string, target *string) error {
	// For now, just do simple string search
	// This is simplified - would use regex in production
	search := fmt.Sprintf(`"%s":`, field)
	idx := findAfter(text, search, `"`)
	if idx != "" {
		*target = idx
		return nil
	}

	// Try without quotes
	search = fmt.Sprintf(`%s:`, field)
	idx = findAfter(text, search, ` `)
	if idx != "" {
		// Extract until comma or newline
		for i, c := range idx {
			if c == ',' || c == '\n' || c == '}' {
				*target = idx[:i]
				return nil
			}
		}
		*target = idx
		return nil
	}

	return fmt.Errorf("field %s not found", field)
}

// findAfter finds text after a marker until a delimiter
func findAfter(text, marker, delimiter string) string {
	idx := strings.Index(text, marker)
	if idx == -1 {
		return ""
	}

	// Start after marker
	start := idx + len(marker)
	for start < len(text) && (text[start] == ' ' || text[start] == '\t') {
		start++
	}

	// Find end delimiter
	end := strings.Index(text[start:], delimiter)
	if end == -1 {
		return ""
	}

	return text[start : start+end]
}

// BuildDecisionPrompt creates a prompt for the LLM
func BuildDecisionPrompt(agentName, role string, personality map[string]interface{}, state map[string]interface{}) string {
	return fmt.Sprintf(`
You are %s, a %s in the Spacemolt universe.

CURRENT SITUATION:
Location: %v
Fuel: %v
Hull: %v
Cargo: %v

Based on your role as a %s, decide what to do next.

Respond in JSON format:
{
  "action": "undock|dock|travel|mine|scan|wait",
  "target": "target_id_if_applicable",
  "reasoning": "your reasoning in 1-2 sentences",
  "confidence": 0.0-1.0
}

Your decision:
`, agentName, role,
	state["location"], state["fuel"], state["hull"], state["cargo"],
	role)
}

// TestConnection tests if Ollama is reachable
func (c *Client) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	return nil
}
