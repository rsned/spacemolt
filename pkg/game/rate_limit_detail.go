package game

import (
	"io"
	"net/http"
	"strings"
)

// rateLimitBodyLimit bounds how much of a rate-limit response body is folded
// into the error text. The duration we are after sits in the first line; the
// cap keeps a broken or hostile server from pushing an unbounded string into
// our logs and error chain.
const rateLimitBodyLimit = 1024

// rateLimitDetail renders the server's stated back-off from a rate-limited
// response so it survives into the error text.
//
// Connect used to close the 429 body unread, which threw away the one number
// that matters. rateLimitBlock then found no "try again in N seconds" in the
// bare dial error and fell back to the 60s default, so the gate published a
// 60s hold against a block lasting minutes. Live 2026-08-28 the gate opened at
// 00:57:49 while the real block ran to 01:04:27: the fleet was released 6.5
// minutes early, straight back into the block, which escalated it from 8 to
// 15.5 minutes.
//
// Both carriers are read: the standard Retry-After header, and the body, which
// is where this server actually states it ("Try again in N seconds", or a JSON
// retry_after). A nil response, or one with no body, yields "" rather than
// panicking -- a failed dial may have no response at all.
func rateLimitDetail(r *http.Response) string {
	if r == nil {
		return ""
	}
	var parts []string
	if ra := r.Header.Get("Retry-After"); ra != "" {
		parts = append(parts, "retry-after: "+ra)
	}
	if r.Body != nil {
		b, _ := io.ReadAll(io.LimitReader(r.Body, rateLimitBodyLimit))
		if s := strings.TrimSpace(string(b)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}
