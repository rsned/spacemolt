package game

import (
	"fmt"
	"regexp"
	"strings"
)

// rateLimitEvent is one `rate_limited` error: the bucket the fleet exhausted,
// the server's stated ceiling, what it observed, and how long it wants us to
// wait.
//
// These are the PRECURSORS to an IP block, and they are the only view of it we
// get. The server exposes no endpoint describing what tripped a block, and
// get_action_log records only actions that succeeded — never rate-limit hits or
// request counts. Once the block lands, every command returns the same
// ip_timed_out countdown and there is nothing left to learn. So a tally of
// these events, logged as they happen, IS the incident record. Without it a
// post-mortem can only rule causes out (2026-09-09: heartbeats, dock/refuel
// suppression, reconnects and backfills were each excluded by log archaeology,
// leaving "170 workers is simply too many" as an inference rather than a
// measurement).
type rateLimitEvent struct {
	Bucket      string // game_query, game_mutation, public_api, session_auth, connection
	LimitPerMin int
	Current     int
	RetryAfter  int // seconds
}

// String renders the event for the worker log. Format is deliberately greppable
// and stable: `rate_limited bucket=<b> ...`, so an operator can tally an
// incident with grep -o and sort | uniq -c.
func (e rateLimitEvent) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "rate_limited bucket=%s", e.Bucket)
	if e.LimitPerMin > 0 {
		fmt.Fprintf(&b, " limit_per_min=%d", e.LimitPerMin)
	}
	if e.Current > 0 {
		fmt.Fprintf(&b, " current=%d", e.Current)
	}
	if e.RetryAfter > 0 {
		fmt.Fprintf(&b, " retry_after=%ds", e.RetryAfter)
	}

	return b.String()
}

// rateLimitBucketNames are the buckets the server meters separately. Used to
// recover a bucket from message text when no structured details arrive.
var rateLimitBucketNames = []string{
	"game_query", "game_mutation", "public_api", "session_auth", "connection",
}

// rateLimitedMessageRe recognises a rate-limit error that arrived as bare text.
// MCP tool errors carry only a message — no code, no details — so without this
// every MCP-transport rate limit would be invisible to the tally.
var rateLimitedMessageRe = regexp.MustCompile(`(?i)rate[ _-]?limit`)

// rateLimitBucketFrom reports the bucket event carried by an error payload.
//
// ok is false for anything that is not a rate-limit precursor. In particular
// ip_timed_out is EXCLUDED: that is the block itself, handled by
// parseErrorState, and counting it here would double-count one incident while
// saying nothing about which bucket caused it.
//
// A rate_limited error whose bucket cannot be named still returns ok with
// Bucket "unknown" — dropping it would under-count the very tally this exists
// to produce.
func rateLimitBucketFrom(payload map[string]any) (rateLimitEvent, bool) {
	code, _ := payload["code"].(string)
	msg, _ := payload["message"].(string)

	if code == "ip_timed_out" {
		return rateLimitEvent{}, false
	}
	isRateLimited := code == "rate_limited" || (code == "" && rateLimitedMessageRe.MatchString(msg))
	if !isRateLimited {
		return rateLimitEvent{}, false
	}

	ev := rateLimitEvent{Bucket: "unknown"}
	details, _ := payload["details"].(map[string]any)
	if limit, ok := details["limit"].(string); ok && limit != "" {
		ev.Bucket = limit
	} else if b := bucketFromText(msg); b != "" {
		ev.Bucket = b
	}
	ev.LimitPerMin = intFromAny(details["limit_per_min"])
	ev.Current = intFromAny(details["current"])

	// retry_after rides at the payload root on some endpoints and inside the
	// error object on others; take whichever is present.
	ev.RetryAfter = intFromAny(payload["retry_after"])
	if ev.RetryAfter == 0 {
		if errObj, ok := payload["error"].(map[string]any); ok {
			ev.RetryAfter = intFromAny(errObj["retry_after"])
		}
	}

	return ev, true
}

// bucketFromText recovers a bucket name mentioned in free text, for the
// message-only errors the MCP transport delivers.
func bucketFromText(msg string) string {
	for _, name := range rateLimitBucketNames {
		if strings.Contains(msg, name) {
			return name
		}
	}

	return ""
}

// intFromAny reads a JSON number that may have decoded as float64 or int.
func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}

	return 0
}
