package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// AgentServerClient communicates with agent-server HTTP API
type AgentServerClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewAgentServerClient creates a new agent-server client
func NewAgentServerClient(baseURL string) *AgentServerClient {
	return &AgentServerClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// AgentInfoRemote represents basic agent information from API
type AgentInfoRemote struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// StreamEvent represents an SSE event from the agent-server
type StreamEvent struct {
	AgentID   string                 `json:"agent_id,omitempty"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// ListAgents fetches the list of active agents from the server
func (c *AgentServerClient) ListAgents() ([]AgentInfoRemote, error) {
	url := fmt.Sprintf("%s/api/agents", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var agents []AgentInfoRemote
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return agents, nil
}

// GetState fetches the game state for a specific agent
func (c *AgentServerClient) GetState(agentID string) (*game.State, error) {
	url := fmt.Sprintf("%s/api/agents/%s/state", c.baseURL, agentID)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch state: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var state game.State
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode state: %w", err)
	}

	return &state, nil
}

// GetHistory fetches action history for a specific agent
func (c *AgentServerClient) GetHistory(agentID string, limit int) ([]agent.HistoryEntry, error) {
	url := fmt.Sprintf("%s/api/agents/%s/history?limit=%d", c.baseURL, agentID, limit)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var history []agent.HistoryEntry
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		return nil, fmt.Errorf("failed to decode history: %w", err)
	}

	return history, nil
}

// StreamEvents subscribes to Server-Sent Events for an agent
func (c *AgentServerClient) StreamEvents(agentID string) (<-chan StreamEvent, error) {
	url := fmt.Sprintf("%s/api/agents/%s/stream", c.baseURL, agentID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	// Use a client without timeout for streaming
	streamClient := &http.Client{
		Timeout: 0, // No timeout for SSE
	}

	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Create event channel
	eventCh := make(chan StreamEvent, 100)

	// Start SSE reader in background
	go c.readSSE(resp.Body, eventCh)

	return eventCh, nil
}

// readSSE parses Server-Sent Events from response body
func (c *AgentServerClient) readSSE(body io.ReadCloser, eventCh chan<- StreamEvent) {
	defer close(eventCh)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var currentEvent StreamEvent
	var eventType string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line means end of event
			if eventType != "" {
				currentEvent.Type = eventType
				eventCh <- currentEvent
			}
			// Reset for next event
			currentEvent = StreamEvent{}
			eventType = ""
			continue
		}

		// Parse SSE field
		if len(line) > 6 && line[:6] == "event:" {
			eventType = line[7:]
		} else if len(line) > 5 && line[:5] == "data:" {
			data := line[6:]
			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				// Skip malformed events
				continue
			}
			currentEvent = event
		}
	}

	if err := scanner.Err(); err != nil {
		// Connection closed or error reading
		return
	}
}

// Ping checks if the agent-server is reachable
func (c *AgentServerClient) Ping() error {
	url := fmt.Sprintf("%s/api/agents", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	return nil
}
