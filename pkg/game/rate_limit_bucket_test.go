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

// v0.601.6 pinned down where the wait actually lives: details.retry_after.
// (It also confirmed `wait_seconds` was NEVER sent by the server -- we do not
// read it anywhere, so nothing to undo there.)
//
// We only looked at the payload root and the error object, and the proof is in
// the 950 precursor lines captured during the 2026-09-11 incident: every one
// read
//
//	rate_limited bucket=game_mutation limit_per_min=30 current=30 msg="..."
//
// with no retry_after at all. The one number that says how long to wait was
// being dropped on the floor of the only record we have of a block.
func TestRateLimitRetryAfterFromDetails(t *testing.T) {
	ev, ok := rateLimitBucketFrom(map[string]any{
		"code":    "rate_limited",
		"message": "Rate limit reached: game actions are capped at 30/min for this session",
		"details": map[string]any{
			"limit":         "game_mutation",
			"limit_per_min": float64(30),
			"current":       float64(30),
			"retry_after":   float64(55),
		},
	})
	if !ok {
		t.Fatal("must classify as a rate-limit precursor")
	}
	if ev.RetryAfter != 55 {
		t.Errorf("RetryAfter = %d, want 55 read from details", ev.RetryAfter)
	}
	if ev.Bucket != "game_mutation" || ev.LimitPerMin != 30 || ev.Current != 30 {
		t.Errorf("other fields regressed: %+v", ev)
	}
}

// The root and error-object placements still have to work: the docs describe
// details as canonical, but HTTP and WebSocket have historically differed and
// dropping a wait we can already read would be a regression.
func TestRateLimitRetryAfterStillReadFromRootAndErrorObject(t *testing.T) {
	root, ok := rateLimitBucketFrom(map[string]any{
		"code": "rate_limited", "retry_after": float64(7),
	})
	if !ok || root.RetryAfter != 7 {
		t.Errorf("root placement: got %+v", root)
	}
	nested, ok := rateLimitBucketFrom(map[string]any{
		"code":  "rate_limited",
		"error": map[string]any{"retry_after": float64(9)},
	})
	if !ok || nested.RetryAfter != 9 {
		t.Errorf("error-object placement: got %+v", nested)
	}
}

// details wins when more than one is present: v0.601.6 names it as the field
// that carries the wait.
func TestRateLimitDetailsRetryAfterWins(t *testing.T) {
	ev, _ := rateLimitBucketFrom(map[string]any{
		"code":        "rate_limited",
		"retry_after": float64(1),
		"details":     map[string]any{"retry_after": float64(42)},
	})
	if ev.RetryAfter != 42 {
		t.Errorf("RetryAfter = %d, want 42 (details is canonical)", ev.RetryAfter)
	}
}

// action_pending is a SEPARATE code (v0.601.6): it means this tick's action is
// already queued, not that we are being throttled. Counting it as a rate-limit
// precursor would inflate every incident tally -- and we saw a lot of it on
// 2026-09-11 while gifting from actively-mining workers.
func TestActionPendingIsNotARateLimit(t *testing.T) {
	if _, ok := rateLimitBucketFrom(map[string]any{
		"code":    "action_pending",
		"message": "Another action is already pending (mine). Wait for it to complete.",
	}); ok {
		t.Error("action_pending must not be classified as a rate-limit precursor")
	}
}
