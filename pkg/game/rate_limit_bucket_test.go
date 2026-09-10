package game

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// The precursor signal. A rate_limited error names the bucket it hit in
// details.limit; piling these up across the fleet's shared IP is what earns the
// block, and the server exposes no other view of what tripped it. Without this
// the bucket is dropped on the floor and an incident can only be guessed at.
func TestRateLimitBucketFromPayload(t *testing.T) {
	got, ok := rateLimitBucketFrom(map[string]any{
		"code":    "rate_limited",
		"message": "Slow down.",
		"details": map[string]any{
			"limit":         "game_query",
			"limit_per_min": float64(120),
			"current":       float64(131),
		},
		"retry_after": float64(7),
	})
	if !ok {
		t.Fatal("rateLimitBucketFrom did not recognise a rate_limited payload")
	}
	if got.Bucket != "game_query" {
		t.Fatalf("Bucket = %q, want game_query", got.Bucket)
	}
	if got.LimitPerMin != 120 || got.Current != 131 {
		t.Fatalf("limit/current = %d/%d, want 120/131", got.LimitPerMin, got.Current)
	}
	if got.RetryAfter != 7 {
		t.Fatalf("RetryAfter = %d, want 7", got.RetryAfter)
	}
}

// retry_after rides at the payload root on some endpoints and inside the error
// object on others, so both placements must resolve.
func TestRateLimitBucketRetryAfterNestedInError(t *testing.T) {
	got, ok := rateLimitBucketFrom(map[string]any{
		"code":    "rate_limited",
		"details": map[string]any{"limit": "game_mutation"},
		"error":   map[string]any{"retry_after": float64(15)},
	})
	if !ok {
		t.Fatal("not recognised")
	}
	if got.RetryAfter != 15 {
		t.Fatalf("RetryAfter = %d, want 15 from the nested error object", got.RetryAfter)
	}
}

// Only the precursor is a bucket event. ip_timed_out is the block itself and is
// handled elsewhere — counting it here would double-count the incident and tell
// us nothing about which bucket caused it.
func TestRateLimitBucketIgnoresIPTimeout(t *testing.T) {
	if _, ok := rateLimitBucketFrom(map[string]any{
		"code":    "ip_timed_out",
		"message": "Your IP has been temporarily blocked. Try again in 905 seconds.",
	}); ok {
		t.Fatal("ip_timed_out must not be reported as a bucket event")
	}
}

// An ordinary gameplay error is not a rate-limit event.
func TestRateLimitBucketIgnoresOtherErrors(t *testing.T) {
	if _, ok := rateLimitBucketFrom(map[string]any{
		"code":    "not_docked",
		"message": "You must be docked.",
	}); ok {
		t.Fatal("a gameplay error must not be reported as a bucket event")
	}
}

// MCP tool errors carry only the message text — no code, no details — so the
// bucket has to be recovered from the string or the event is lost entirely.
func TestRateLimitBucketFromMessageOnly(t *testing.T) {
	got, ok := rateLimitBucketFrom(map[string]any{
		"message": "rate_limited: game_mutation limit exceeded, retry in 12s",
	})
	if !ok {
		t.Fatal("a message-only rate_limited error was not recognised")
	}
	if got.Bucket != "game_mutation" {
		t.Fatalf("Bucket = %q, want game_mutation", got.Bucket)
	}
}

// A rate_limited error with no bucket we can name must still be recorded, as
// "unknown" — dropping it would under-count the tally that identifies the
// incident.
func TestRateLimitBucketUnknownStillCounts(t *testing.T) {
	got, ok := rateLimitBucketFrom(map[string]any{"code": "rate_limited"})
	if !ok {
		t.Fatal("a bucketless rate_limited error must still count")
	}
	if got.Bucket != "unknown" {
		t.Fatalf("Bucket = %q, want unknown", got.Bucket)
	}
}

// The log line is the artifact an operator greps and tallies after an incident,
// so it must carry the bucket and the observed rate.
func TestRateLimitEventString(t *testing.T) {
	s := rateLimitEvent{Bucket: "game_query", LimitPerMin: 120, Current: 131, RetryAfter: 7}.String()
	for _, want := range []string{"game_query", "120", "131", "7"} {
		if !strings.Contains(s, want) {
			t.Fatalf("event string %q is missing %q", s, want)
		}
	}
}

// The record must exist when debug is OFF. SetDebugLogging(false) points debugLogger at
// io.Discard, so a precursor logged there would be captured only when someone
// had already enabled --debug — exactly the case where a post-mortem is
// impossible. This is the regression that matters.
func TestRateLimitPrecursorLoggedWithDebugOff(t *testing.T) {
	var buf bytes.Buffer
	c := NewClient("ws://test", "u", "p", log.New(&buf, "", 0))
	c.SetDebugLogging(false)

	c.parseErrorState(map[string]any{
		"code":    "rate_limited",
		"details": map[string]any{"limit": "game_query", "limit_per_min": float64(120), "current": float64(131)},
	})

	if got := buf.String(); !strings.Contains(got, "rate_limited bucket=game_query") {
		t.Fatalf("precursor was not logged with debug off; log = %q", got)
	}
}

// The block itself must NOT be counted as a bucket event: it would double-count
// the incident and name no bucket.
func TestIPTimeoutNotLoggedAsPrecursor(t *testing.T) {
	var buf bytes.Buffer
	c := NewClient("ws://test", "u", "p", log.New(&buf, "", 0))
	c.SetDebugLogging(false)

	c.parseErrorState(map[string]any{
		"code":    "ip_timed_out",
		"message": "Your IP has been temporarily blocked. Try again in 905 seconds.",
	})

	if strings.Contains(buf.String(), "rate_limited bucket=") {
		t.Fatalf("ip_timed_out was logged as a bucket precursor: %q", buf.String())
	}
}

// The server names its buckets in PROSE — "OpenAPI spec fetches", "catalog dump
// downloads", "public API requests" — not as the snake_case identifiers a
// details.limit carries. Those match no pattern we could have guessed, and an
// MCP tool error carries the text and nothing else, so the message must survive
// into the log verbatim or the event is unreadable.
func TestRateLimitKeepsProseMessageVerbatim(t *testing.T) {
	ev, ok := rateLimitBucketFrom(map[string]any{
		"message": "Rate limit exceeded for OpenAPI spec fetches. Retry in 30s.",
	})
	if !ok {
		t.Fatal("a prose rate-limit message was not recognised")
	}
	if ev.Bucket != "unknown" {
		t.Fatalf("Bucket = %q; prose names are not identifiers and must not be forced into one", ev.Bucket)
	}
	if !strings.Contains(ev.String(), "OpenAPI spec fetches") {
		t.Fatalf("the log line lost the server's own wording: %q", ev.String())
	}
}

// A structured bucket still logs its message alongside, so the hint in the text
// is never traded away for the identifier.
func TestRateLimitKeepsMessageAlongsideStructuredBucket(t *testing.T) {
	ev, _ := rateLimitBucketFrom(map[string]any{
		"code":    "rate_limited",
		"message": "Slow down: catalog dump downloads.",
		"details": map[string]any{"limit": "public_api"},
	})
	line := ev.String()
	if !strings.Contains(line, "bucket=public_api") || !strings.Contains(line, "catalog dump downloads") {
		t.Fatalf("line %q must carry BOTH the identifier and the wording", line)
	}
}

// An unbounded message must be truncated rather than pushed whole into the log.
func TestRateLimitMessageTruncated(t *testing.T) {
	ev, _ := rateLimitBucketFrom(map[string]any{
		"code":    "rate_limited",
		"message": strings.Repeat("x", 5000),
	})
	if len(ev.String()) > rateLimitMessageMax+200 {
		t.Fatalf("log line was not truncated: %d chars", len(ev.String()))
	}
}
