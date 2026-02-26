package game

import (
	"errors"
	"testing"
)

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"not connected", errors.New("not connected"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"connection closed", errors.New("connection closed"), true},
		{"broken pipe", errors.New("broken pipe"), true},
		{"connection timeout", errors.New("connection timeout"), true},
		{"websocket closed", errors.New("websocket closed"), true},
		{"failed to send message", errors.New("failed to send message"), true},
		{"unrelated error", errors.New("invalid argument"), false},
		{"empty error", errors.New(""), false},
		{"partial match in middle", errors.New("error: not connected to server"), true},
		{"case sensitive no match", errors.New("NOT CONNECTED"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConnectionError(tt.err); got != tt.want {
				t.Errorf("isConnectionError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"timeout", errors.New("timeout"), true},
		{"deadline exceeded", errors.New("deadline exceeded"), true},
		{"temporary failure", errors.New("temporary failure"), true},
		{"rate limited", errors.New("rate limited"), true},
		{"already in transit", errors.New("already in transit"), true},
		{"permanent error", errors.New("permission denied"), false},
		{"empty error", errors.New(""), false},
		{"timeout in context", errors.New("operation: context timeout occurred"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.err); got != tt.want {
				t.Errorf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{"exact match", "hello", "hello", true},
		{"prefix match", "hello world", "hello", true},
		{"suffix match", "hello world", "world", true},
		{"middle match", "hello beautiful world", "beautiful", true},
		{"no match", "hello", "xyz", false},
		{"empty substr", "hello", "", true},
		{"both empty", "", "", true},
		{"substr longer than s", "hi", "hello", false},
		{"single char match", "a", "a", true},
		{"single char no match", "a", "b", false},
		{"overlapping patterns", "aaa", "aa", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(tt.s, tt.substr); got != tt.want {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}

func TestContainsMiddle(t *testing.T) {
	tests := []struct {
		s, substr string
		want      bool
	}{
		{"hello world", "lo wo", true},
		{"hello", "xyz", false},
		{"abc", "abc", true},
		{"abcdef", "cde", true},
		{"a", "a", true},
		{"ab", "ba", false},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.substr, func(t *testing.T) {
			if got := containsMiddle(tt.s, tt.substr); got != tt.want {
				t.Errorf("containsMiddle(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}

func TestNewRobustCommandExecutor(t *testing.T) {
	client := NewClient("wss://test.example.com", "user", "pass", nil)
	exec := NewRobustCommandExecutor(client)

	if exec.client != client {
		t.Error("client not set")
	}
	if exec.maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", exec.maxRetries)
	}
	if exec.retryDelay != 2*SleepRetry {
		t.Errorf("retryDelay = %v, want %v", exec.retryDelay, 2*SleepRetry)
	}
}

func TestNewSafeCommandBuilder(t *testing.T) {
	client := NewClient("wss://test.example.com", "user", "pass", nil)
	builder := NewSafeCommandBuilder(client)

	if builder.client != client {
		t.Error("client not set")
	}
	if builder.exec == nil {
		t.Error("executor not set")
	}
}
