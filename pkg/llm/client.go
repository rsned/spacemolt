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
<<<<<<< pr/enhance-agent-prompts-with-game-mechanics-and-fix-
	BaseURL       string
	Model         string
	Timeout       time.Duration
	PromptsDir    string
=======
	BaseURL      string
	Model        string
	Timeout      time.Duration
	PromptsDir   string
>>>>>>> main
	PromptsConfig string
}

// New creates a new Ollama client
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "llama3.2"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
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

	// DEBUG: Log raw LLM response
	fmt.Printf("[LLM] Raw response text:\n%s\n", ollamaResp.Response)

	// Extract structured decision from text response
	return c.parseDecision(ollamaResp.Response)
}

// parseDecision extracts structured decision from LLM text response
func (c *Client) parseDecision(text string) (*DecisionResponse, error) {
	// Find JSON block in response
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON block found in response")
	}

	jsonStr := text[start : end+1]

	// DEBUG: Log extracted JSON
	fmt.Printf("[LLM] Extracted JSON string:\n%s\n", jsonStr)

	var decision DecisionResponse
	if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
		fmt.Printf("[LLM] ERROR: Failed to parse JSON: %v\n", err)
		return nil, fmt.Errorf("failed to parse decision JSON: %w", err)
	}

<<<<<<< pr/enhance-agent-prompts-with-game-mechanics-and-fix-
	/*
		// DEBUG: Log parsed fields
		fmt.Printf("[LLM] Parsed DecisionResponse:\n")
		fmt.Printf("  Action: '%s'\n", decision.Action)
		fmt.Printf("  Target: '%s'\n", decision.Target)
		fmt.Printf("  Reasoning: '%s'\n", decision.Reasoning)
		fmt.Printf("  Confidence: %.2f\n", decision.Confidence)
	*/
=======
	// DEBUG: Log parsed fields
	fmt.Printf("[LLM] Parsed DecisionResponse:\n")
	fmt.Printf("  Action: '%s'\n", decision.Action)
	fmt.Printf("  Target: '%s'\n", decision.Target)
	fmt.Printf("  Reasoning: '%s'\n", decision.Reasoning)
	fmt.Printf("  Confidence: %.2f\n", decision.Confidence)
>>>>>>> main

	return &decision, nil
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
Docked: %v
<<<<<<< pr/enhance-agent-prompts-with-game-mechanics-and-fix-

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

=======

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

>>>>>>> main
IMPORTANT: Respond with valid JSON containing exactly these fields:
{
  "action": "one_action_name",
  "target": "target_id_or_name_if_required",
  "reasoning": "your reasoning in 1-2 sentences",
  "confidence": 0.85
}

EXAMPLES:
<<<<<<< pr/enhance-agent-prompts-with-game-mechanics-and-fix-
- To travel to a POI: {"action": "travel", "target": "sol_belt", "reasoning": "Mining asteroids for resources", "confidence": 0.9}
- To jump to system: {"action": "jump", "target": "alpha_centauri", "reasoning": "Exploring new system", "confidence": 0.85}
- To undock: {"action": "undock", "target": "", "reasoning": "Leaving station to explore", "confidence": 0.8}
- To wait: {"action": "wait", "target": "", "reasoning": "Waiting for better opportunity", "confidence": 0.7}

CRITICAL: For "travel" action, use the EXACT POI ID from the available POIs list.

=======
- To travel to a POI: {"action": "travel", "target": "Sol-AsteroidField", "reasoning": "Mining asteroids for resources", "confidence": 0.9}
- To undock: {"action": "undock", "target": "", "reasoning": "Leaving station to explore", "confidence": 0.8}
- To wait: {"action": "wait", "target": "", "reasoning": "Waiting for better opportunity", "confidence": 0.7}

>>>>>>> main
Your decision:
`, agentName, role,
		state["location"], state["fuel"], state["hull"], state["cargo"], state["docked"],
		role)
}

// RenderPrompt renders a prompt template with the given context
func (c *Client) RenderPrompt(promptType string, role string, ctx *prompts.TemplateContext) (string, error) {
	// If prompt manager is not available, return error
	if c.promptManager == nil || c.selector == nil {
		return "", fmt.Errorf("prompt manager not initialized")
	}

	// Select version
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
