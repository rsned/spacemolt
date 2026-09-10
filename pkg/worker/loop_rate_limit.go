package worker

import (
	"errors"
	"regexp"

	"github.com/rsned/spacemolt/pkg/game"
)

// ErrRateLimited marks an error as the server refusing a command because a
// rate limit is exhausted, or because the IP is already inside a block.
//
// A loop aborts on it even under -f. That is a deliberate exception to what
// force means: -f tolerates FAILURES (a dry belt, a missed jump), but a rate
// limit is not a failure to retry past — it is the server telling us to stop.
// Continuing is what turns a 2-minute block into a 30-minute one, since the
// limiter escalates on accumulated violations. See
// docs/memory reference_rate_limit_buckets_and_escalation.
var ErrRateLimited = errors.New("rate limited by the server")

// rateLimitCodes are the structured server codes that mean "stop sending".
// rate_limited is the precursor (a bucket is exhausted); ip_timed_out is the
// block itself, during which every command returns the same countdown.
var rateLimitCodes = map[string]struct{}{
	"rate_limited": {},
	"ip_timed_out": {},
}

// rateLimitText recognises a rate limit that arrived as bare text. The MCP
// transport delivers tool errors with a message and nothing else — no code, no
// details — so a code-only classifier would be blind on exactly the transport
// where we have the least other signal. Kept deliberately broad: a false
// positive costs one paused loop, a false negative costs an escalated block.
var rateLimitText = regexp.MustCompile(`(?i)rate[ _-]?limit|temporarily blocked|ip_timed_out`)

// isRateLimit reports whether err is the server refusing us for rate reasons.
func isRateLimit(err error) bool {
	if err == nil {
		return false
	}
	var se *game.ServerError
	if errors.As(err, &se) {
		if _, ok := rateLimitCodes[se.Code]; ok {
			return true
		}
	}

	return rateLimitText.MatchString(err.Error())
}
