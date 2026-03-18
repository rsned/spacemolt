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

	"github.com/rsned/spacemolt/pkg/prompts"
)

// Client handles communication with Ollama
type Client struct {
	baseURL       string
	model         string
	httpClient    *http.Client
	promptManager *prompts.Manager
	selector      *prompts.Selector
}

// Config holds LLM client configuration
type Config struct {
	BaseURL       string
	Model         string
	Timeout       time.Duration
	PromptsDir    string
	PromptsConfig string
}

// New creates a new Ollama client
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "qwen3:14b"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	if cfg.PromptsDir == "" {
		cfg.PromptsDir = "data/prompts/templates"
	}
	if cfg.PromptsConfig == "" {
		cfg.PromptsConfig = "data/prompts/config.yaml"
	}

	// Initialize prompt manager
	manager, err := prompts.NewManager(prompts.Config{
		TemplatesDir: cfg.PromptsDir,
		ConfigPath:   cfg.PromptsConfig,
	})
	if err != nil {
		// Log warning but continue with nil manager (will use fallback prompts)
		fmt.Printf("Warning: Failed to initialize prompt manager: %v\n", err)
		fmt.Println("Continuing with hardcoded prompts as fallback")
	}

	// Load prompt configuration
	var selector *prompts.Selector
	if manager != nil {
		promptCfg, err := prompts.LoadConfig(cfg.PromptsConfig)
		if err != nil {
			fmt.Printf("Warning: Failed to load prompt config: %v\n", err)
		} else {
			// Create registry
			registry, err := prompts.NewRegistry(cfg.PromptsDir)
			if err != nil {
				fmt.Printf("Warning: Failed to create registry: %v\n", err)
			} else {
				selector = prompts.NewSelector(registry, promptCfg)
			}
		}
	}

	return &Client{
		baseURL:       cfg.BaseURL,
		model:         cfg.Model,
		promptManager: manager,
		selector:      selector,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

// DecisionRequest represents a request for an agent decision
type DecisionRequest struct {
	AgentName    string
	Personality  string
	CurrentState string
	Knowledge    string
	Experiences  string
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
	payload := map[string]any{
		"model":  c.model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]any{
			"temperature": 0.7,
			"num_predict": 4096,
			"num_ctx":     16384,
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
	// Find all complete JSON objects in the response
	// LLMs sometimes return examples followed by the actual response
	var jsonObjects []string
	inObject := false
	braceCount := 0
	currentStart := -1

	for i, ch := range text {
		switch ch {
		case '{':
			if braceCount == 0 {
				currentStart = i
				inObject = true
			}
			braceCount++
		case '}':
			braceCount--
			if braceCount == 0 && inObject {
				jsonObjects = append(jsonObjects, text[currentStart:i+1])
				inObject = false
			}
		}
	}

	if len(jsonObjects) == 0 {
		return nil, fmt.Errorf("no JSON block found in response")
	}

	// Try to parse JSON objects, preferring the last valid one
	// (LLMs often put examples first, then the actual decision)
	var lastError error
	for i := len(jsonObjects) - 1; i >= 0; i-- {
		jsonStr := jsonObjects[i]

		var decision DecisionResponse
		if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
			lastError = err
			continue
		}

		return &decision, nil
	}

	return nil, fmt.Errorf("failed to parse any JSON object: %w", lastError)
}

// BuildDecisionPrompt creates a prompt for the LLM
func BuildDecisionPrompt(agentName, role string, personality map[string]any, state map[string]any) string {
	return fmt.Sprintf(`
You are %s, a %s in the Spacemolt universe.

CURRENT SITUATION:
Location: %v
Fuel: %v
Hull: %v
Cargo: %v
Docked: %v

Based on your role as a %s, decide what to do next. You may only pick one action at a time.

AVAILABLE ACTIONS:
- "undock" - Leave the current station (no target needed)
- "dock" - Dock at the current POI (no target needed)
- "travel" - Travel to a POI in the current system (requires target: POI ID)
- "jump" - Jump to another system (requires target: system name)
- "mine" - Mine resources at current location (no target needed)
- "scan" - Scan the current area (no target needed)
- "get_status" - Get player status (no target needed)
- "get_system" - Get current system info (no target needed)
- "wait" - Wait and do nothing (no target needed)

IMPORTANT: Respond with valid JSON containing exactly these fields:
{
  "action": "one_action_name",
  "target": "target_id_or_name_if_required",
  "reasoning": "your reasoning in 1-2 sentences",
  "confidence": 0.85
}

EXAMPLES:
- To travel to a POI: {"action": "travel", "target": "sol_belt", "reasoning": "Mining asteroids for resources", "confidence": 0.9}
- To jump to system: {"action": "jump", "target": "alpha_centauri", "reasoning": "Exploring new system", "confidence": 0.85}
- To undock: {"action": "undock", "target": "", "reasoning": "Leaving station to explore", "confidence": 0.8}
- To wait: {"action": "wait", "target": "", "reasoning": "Waiting for better opportunity", "confidence": 0.7}

CRITICAL: For "travel" action, use the EXACT POI ID from the available POIs list.

- To travel to a POI: {"action": "travel", "target": "Sol-AsteroidField", "reasoning": "Mining asteroids for resources", "confidence": 0.9}
- To undock: {"action": "undock", "target": "", "reasoning": "Leaving station to explore", "confidence": 0.8}
- To wait: {"action": "wait", "target": "", "reasoning": "Waiting for better opportunity", "confidence": 0.7}

Your decision:
`, agentName, role,
		state["location"], state["fuel"], state["hull"], state["cargo"], state["docked"],
		role)
}

// RenderPrompt renders a prompt template with the given context.
// It first tries a role-specific template (e.g., "decision.craftsman") and
// falls back to the generic template (e.g., "decision") if not found.
func (c *Client) RenderPrompt(promptType string, role string, ctx *prompts.TemplateContext) (string, error) {
	// If prompt manager is not available, return error
	if c.promptManager == nil || c.selector == nil {
		return "", fmt.Errorf("prompt manager not initialized")
	}

	// Try role-specific template first (e.g., "decision.craftsman")
	if role != "" {
		roleType := promptType + "." + strings.ToLower(role)
		selCtx := prompts.SelectionContext{
			PromptType: roleType,
			Role:       role,
		}

		if version, err := c.selector.SelectVersion(selCtx); err == nil {
			if prompt, err := c.promptManager.RenderPrompt(roleType, version, ctx); err == nil {
				return prompt, nil
			}
		}
	}

	// Fall back to generic template
	selCtx := prompts.SelectionContext{
		PromptType: promptType,
		Role:       role,
	}

	version, err := c.selector.SelectVersion(selCtx)
	if err != nil {
		return "", fmt.Errorf("failed to select version: %w", err)
	}

	// Render prompt
	prompt, err := c.promptManager.RenderPrompt(promptType, version, ctx)
	if err != nil {
		return "", fmt.Errorf("failed to render prompt: %w", err)
	}

	return prompt, nil
}

// HasPromptManager returns whether the prompt manager is initialized
func (c *Client) HasPromptManager() bool {
	return c.promptManager != nil && c.selector != nil
}

// Generate sends a raw prompt to the LLM and returns the response text.
// Used by the Thought Engine for multi-call decision pipelines.
// Uses the /api/chat endpoint which handles thinking models (qwen3) properly.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.7,
			"num_predict": 4096,
			"num_ctx":     16384,
		},
		"format": "json",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var chatResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Done bool `json:"done"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return chatResp.Message.Content, nil
}

// Model returns the configured model name.
func (c *Client) Model() string {
	return c.model
}

// SetModel changes the LLM model used for subsequent requests.
func (c *Client) SetModel(model string) {
	c.model = model
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
