package registry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WatcherClient is used by the watcher to discover and monitor tools
type WatcherClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewWatcherClient creates a new client for watching tools
func NewWatcherClient(registryURL string) *WatcherClient {
	return &WatcherClient{
		baseURL: registryURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ListTools returns all registered tools
func (c *WatcherClient) ListTools() ([]*ToolRegistration, error) {
	url := fmt.Sprintf("%s/api/tools", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tools []*ToolRegistration
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return tools, nil
}

// StreamStatus subscribes to SSE status updates for all tools
func (c *WatcherClient) StreamStatus() (<-chan Event, error) {
	url := fmt.Sprintf("%s/api/status/stream", c.baseURL)

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
	eventCh := make(chan Event, 100)

	// Start SSE reader in background
	go c.readSSE(resp.Body, eventCh)

	return eventCh, nil
}

// readSSE parses Server-Sent Events from response body
func (c *WatcherClient) readSSE(body io.ReadCloser, eventCh chan<- Event) {
	defer close(eventCh)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var currentEvent Event
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
			currentEvent = Event{}
			eventType = ""
			continue
		}

		// Parse SSE field
		if len(line) > 6 && line[:6] == "event:" {
			eventType = line[7:]
		} else if len(line) > 5 && line[:5] == "data:" {
			data := line[6:]
			var event Event
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

// Ping checks if the registry is reachable
func (c *WatcherClient) Ping() error {
	url := fmt.Sprintf("%s/api/tools", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("registry unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}

	return nil
}

// GetTool retrieves details for a specific tool
func (c *WatcherClient) GetTool(toolID string) (*ToolRegistration, error) {
	url := fmt.Sprintf("%s/api/tools/%s", c.baseURL, toolID)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tool ToolRegistration
	if err := json.NewDecoder(resp.Body).Decode(&tool); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &tool, nil
}
