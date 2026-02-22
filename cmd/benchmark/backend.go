package main

import (
	"context"
	"time"
)

// Backend defines the interface for all connection types.
type Backend interface {
	Name() string
	Connect(ctx context.Context) error
	Login(ctx context.Context, username, password string) error
	SendCommand(ctx context.Context, command string, payload map[string]any) (map[string]any, error)
	Close() error
	IsConnected() bool
}

// CommandResult captures the outcome of a single command invocation.
type CommandResult struct {
	Backend   string
	AgentID   string
	Command   string
	Latency   time.Duration
	Err       error
	ErrorKind ErrorKind
	ErrorCode string // server error code (e.g. "not_docked", "action_pending")
	Timestamp time.Time
}

// ErrorKind classifies errors into transport vs game-logic categories.
type ErrorKind int

const (
	NoError        ErrorKind = iota
	TransportError           // disconnect, timeout, connection refused
	RateLimitError           // 429, rate_limited, action_queued
	GameLogicError           // already_undocked, not_docked, etc.
)

// classifyError determines the ErrorKind and error code from an error and optional server response.
func classifyError(err error, resp map[string]any) (ErrorKind, string) {
	if err == nil {
		return NoError, ""
	}

	// Extract error code from response payload first (most precise)
	var code string
	if resp != nil {
		code, _ = resp["code"].(string)
	}

	// Classify by code if available
	if code != "" {
		switch code {
		case "rate_limited", "action_pending", "action_queued":
			return RateLimitError, code
		case "already_docked", "already_undocked", "not_docked",
			"already_there", "not_enough_cargo", "no_cargo",
			"insufficient_funds", "no_base",
			"session_required", "session_invalid", "session_limit":
			return GameLogicError, code
		default:
			// Unknown code — treat as game logic error
			return GameLogicError, code
		}
	}

	// Try to extract error code from "code: message" format in error string
	errMsg := err.Error()
	if code == "" {
		if idx := indexByte(errMsg, ':'); idx > 0 {
			candidate := errMsg[:idx]
			// Only treat as code if it looks like a snake_case identifier
			if isSnakeCase(candidate) {
				code = candidate
			}
		}
	}

	// Re-classify if we extracted a code from the error string
	if code != "" {
		switch code {
		case "rate_limited", "action_pending", "action_queued":
			return RateLimitError, code
		case "already_docked", "already_undocked", "not_docked",
			"already_there", "not_enough_cargo", "no_cargo",
			"insufficient_funds", "no_base",
			"session_required", "session_invalid", "session_limit":
			return GameLogicError, code
		default:
			return GameLogicError, code
		}
	}

	// Fall back to error string scanning
	for _, marker := range []string{"429", "rate_limit", "action_queued", "action_pending"} {
		if containsSubstring(errMsg, marker) {
			return RateLimitError, marker
		}
	}

	return TransportError, ""
}

func indexByte(s string, c byte) int {
	for i := range len(s) {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func isSnakeCase(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func containsSubstring(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := range len(s) - len(sub) + 1 {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
