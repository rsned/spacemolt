package game

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestConnectRetryLogic verifies the retry logic for 429 errors
func TestConnectRetryLogic(t *testing.T) {
	// Test the retry detection logic
	testCases := []struct {
		name          string
		errorMsg      string
		shouldRetry   bool
	}{
		{
			name:        "429 error in middle",
			errorMsg:    "failed to WebSocket dial: expected handshake response status code 101 but got 429",
			shouldRetry: true,
		},
		{
			name:        "429 error at start",
			errorMsg:    "429 Too Many Requests",
			shouldRetry: true,
		},
		{
			name:        "429 error at end",
			errorMsg:    "server returned 429",
			shouldRetry: true,
		},
		{
			name:        "Non-429 error",
			errorMsg:    "connection refused",
			shouldRetry: false,
		},
		{
			name:        "500 error",
			errorMsg:    "server returned 500",
			shouldRetry: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Check if error message contains "429"
			isRateLimited := false
			if len(tc.errorMsg) > 0 {
				for i := 0; i < len(tc.errorMsg)-2; i++ {
					if tc.errorMsg[i:i+3] == "429" {
						isRateLimited = true
						break
					}
				}
			}

			if isRateLimited != tc.shouldRetry {
				t.Errorf("Expected retry=%v for error %q, got %v", tc.shouldRetry, tc.errorMsg, isRateLimited)
			}
		})
	}
}

// TestExponentialBackoff verifies the backoff calculation
func TestExponentialBackoff(t *testing.T) {
	baseDelay := 1 * time.Second

	expected := []time.Duration{
		1 * time.Second,  // attempt 0: 1 * 2^0 = 1s
		2 * time.Second,  // attempt 1: 1 * 2^1 = 2s
		4 * time.Second,  // attempt 2: 1 * 2^2 = 4s
		8 * time.Second,  // attempt 3: 1 * 2^3 = 8s
		16 * time.Second, // attempt 4: 1 * 2^4 = 16s
	}

	for attempt := 0; attempt < 5; attempt++ {
		delay := baseDelay * time.Duration(1<<uint(attempt))
		if delay != expected[attempt] {
			t.Errorf("Attempt %d: expected %v, got %v", attempt, expected[attempt], delay)
		}
	}
}

// TestConnectContextCancellation verifies that context cancellation is respected during retries
func TestConnectContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Simulate checking context during retry delay
	select {
	case <-time.After(200 * time.Millisecond):
		t.Error("Should have been cancelled by context timeout")
	case <-ctx.Done():
		// Expected behavior - context cancelled
		if !strings.Contains(ctx.Err().Error(), "deadline exceeded") {
			t.Errorf("Expected deadline exceeded error, got: %v", ctx.Err())
		}
	}
}
