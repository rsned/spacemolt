package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client is used by tools to register with the status registry
type Client struct {
	baseURL    string
	toolID     string
	httpClient *http.Client
	mu         sync.Mutex
}

// NewClient creates a new registry client for a tool
func NewClient(registryURL, toolID string) *Client {
	return &Client{
		baseURL: registryURL,
		toolID:  toolID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Register registers this tool with the registry
func (c *Client) Register(reg ToolRegistration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	reg.ToolID = c.toolID

	url := fmt.Sprintf("%s/api/tools/register", c.baseURL)
	data, err := json.Marshal(reg)
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// Heartbeat sends a heartbeat update to the registry
func (c *Client) Heartbeat(status, action string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	url := fmt.Sprintf("%s/api/tools/%s/heartbeat", c.baseURL, c.toolID)

	req := HeartbeatRequest{
		Status:        status,
		CurrentAction: action,
		Health:        "healthy",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Don't log errors on heartbeat failure - it's expected if registry is down
		return fmt.Errorf("heartbeat failed (HTTP %d)", resp.StatusCode)
	}

	return nil
}

// Deregister removes this tool from the registry
func (c *Client) Deregister() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	url := fmt.Sprintf("%s/api/tools/%s", c.baseURL, c.toolID)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to deregister: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("deregistration failed (HTTP %d)", resp.StatusCode)
	}

	return nil
}

// StartHeartbeat begins sending periodic heartbeats
func (c *Client) StartHeartbeat(ctx context.Context, interval time.Duration, getStatus func() (status, action string)) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				status, action := getStatus()
				if err := c.Heartbeat(status, action); err != nil {
					// Registry unavailable - continue silently
					// The registry will timeout and remove the tool if it stays down
					_ = err // Suppress errcheck error
				}
			}
		}
	}()
}

// UpdateStatus sends a status update immediately
func (c *Client) UpdateStatus(status, action string) error {
	return c.Heartbeat(status, action)
}
