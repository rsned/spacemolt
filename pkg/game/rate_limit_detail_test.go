package game

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func rlDetail(t *testing.T, status int, hdr map[string]string, body string) string {
	t.Helper()
	h := http.Header{}
	for k, v := range hdr {
		h.Set(k, v)
	}
	r := &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	defer func() { _ = r.Body.Close() }()
	return rateLimitDetail(r)
}

// The server states how long the block lasts. Connect used to close the 429
// body unread, so that number never reached rateLimitBlock and the gate
// recorded the 60s bare-429 default instead. Live 2026-08-28 the gate opened at
// 00:57:49 against a block that actually ran to 01:04:27 -- six and a half
// minutes early, straight back into the block, which then escalated from 8 to
// 15.5 minutes.
func TestRateLimitDetail_ExtractsRetryAfterHeader(t *testing.T) {
	d := rlDetail(t, 429, map[string]string{"Retry-After": "913"}, "")
	got, ok := rateLimitBlock(d, reconnectBlockDefault)
	if !ok {
		t.Fatalf("rateLimitBlock(%q) reported no block", d)
	}
	if got != 913*time.Second {
		t.Errorf("block = %v, want 913s (detail was %q)", got, d)
	}
}

func TestRateLimitDetail_ExtractsTryAgainFromBody(t *testing.T) {
	body := "Your IP has been temporarily blocked due to excessive rate limit violations. Try again in 444 seconds."
	d := rlDetail(t, 429, nil, body)
	got, ok := rateLimitBlock(d, reconnectBlockDefault)
	if !ok {
		t.Fatalf("rateLimitBlock(%q) reported no block", d)
	}
	if got != 444*time.Second {
		t.Errorf("block = %v, want 444s", got)
	}
}

func TestRateLimitDetail_ExtractsJSONRetryAfter(t *testing.T) {
	d := rlDetail(t, 429, nil, `{"error":"rate limited","retry_after":1757}`)
	got, ok := rateLimitBlock(d, reconnectBlockDefault)
	if !ok {
		t.Fatalf("rateLimitBlock(%q) reported no block", d)
	}
	if got != 1757*time.Second {
		t.Errorf("block = %v, want 1757s", got)
	}
}

// A hostile or broken server must not let us read an unbounded body into an
// error string.
func TestRateLimitDetail_BoundsTheBody(t *testing.T) {
	d := rlDetail(t, 429, nil, strings.Repeat("x", 1<<20))
	if len(d) > 2048 {
		t.Errorf("detail is %d bytes, want it bounded well under the body size", len(d))
	}
}

func TestRateLimitDetail_NilResponseIsSafe(t *testing.T) {
	if d := rateLimitDetail(nil); d != "" {
		t.Errorf("rateLimitDetail(nil) = %q, want empty", d)
	}
	if d := rateLimitDetail(&http.Response{StatusCode: 429}); d == "" {
		_ = d // a bodyless response is fine; it must simply not panic
	}
}

// The bare-429 fallback is what applies when the server states no duration. 60s
// was an order of magnitude short of every block actually observed (444s to
// 1757s on 2026-08-27/28), so it guaranteed the fleet re-entered the block it
// was meant to be waiting out.
func TestBareBlockDefault_IsNotWildlyShorterThanRealBlocks(t *testing.T) {
	if reconnectBlockDefault < 5*time.Minute {
		t.Errorf("reconnectBlockDefault = %v; every observed per-IP block was >= 444s, so this releases the fleet mid-block",
			reconnectBlockDefault)
	}
}
