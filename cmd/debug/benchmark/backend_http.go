package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// HTTPBackend implements Backend over the HTTP API.
type HTTPBackend struct {
	baseURL   string
	client    *http.Client
	sessionID string
	mu        sync.Mutex
	connected bool
}

type httpResponse struct {
	Result  map[string]any `json:"result"`
	Error   any            `json:"error"`
	Session map[string]any `json:"session"`
}

// NewHTTPBackend creates an HTTP API backend.
func NewHTTPBackend(baseURL string, sharedTransport *http.Transport) *HTTPBackend {
	transport := sharedTransport
	if transport == nil {
		transport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}
	}

	return &HTTPBackend{
		baseURL: baseURL,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (b *HTTPBackend) Name() string { return "http" }

func (b *HTTPBackend) Connect(ctx context.Context) error {
	// Create a session
	req, err := http.NewRequestWithContext(ctx, "POST", b.baseURL+"/api/v1/session", nil)
	if err != nil {
		return fmt.Errorf("create session request: %w", err)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("session request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("session request returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode session response: %w", err)
	}

	if result.Session.ID == "" {
		return fmt.Errorf("no session ID in response")
	}

	b.mu.Lock()
	b.sessionID = result.Session.ID
	b.connected = true
	b.mu.Unlock()

	return nil
}

func (b *HTTPBackend) Login(ctx context.Context, username, password string) error {
	payload := map[string]any{
		"username": username,
		"password": password,
	}
	_, err := b.doCommand(ctx, "login", payload)
	return err
}

func (b *HTTPBackend) SendCommand(ctx context.Context, command string, payload map[string]any) (map[string]any, error) {
	return b.doCommand(ctx, command, payload)
}

func (b *HTTPBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = false
	b.sessionID = ""
	return nil
}

func (b *HTTPBackend) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

func (b *HTTPBackend) doCommand(ctx context.Context, command string, payload map[string]any) (map[string]any, error) {
	b.mu.Lock()
	sessionID := b.sessionID
	b.mu.Unlock()

	if sessionID == "" {
		return nil, fmt.Errorf("no session")
	}

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.baseURL+"/api/v1/"+command, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Session-Id", sessionID)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("429 rate limited")
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		// Try to parse error response for game-logic error codes
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Code != "" {
			return map[string]any{"code": errResp.Error.Code}, fmt.Errorf("%s: %s", errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	var result httpResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != nil {
		errStr := fmt.Sprintf("%v", result.Error)
		return result.Result, fmt.Errorf("api error: %s", errStr)
	}

	return result.Result, nil
}
