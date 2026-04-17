package dataservice

import "strings"

// MaxReplyChars is the server-enforced chat message content limit.
const MaxReplyChars = 500

// truncateSuffix is appended to over-length replies.
const truncateSuffix = "…[truncated]"

// DetectFormat inspects the trimmed request content. If it begins with
// '{' the request is treated as JSON; otherwise plaintext. Detection
// is purely lexical — malformed JSON that starts with '{' is still
// reported as JSON so the caller can produce a JSON error reply.
func DetectFormat(content string) Format {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") {
		return FormatJSON
	}
	return FormatPlaintext
}

// TruncateReply enforces the MaxReplyChars limit. If s is already
// within the limit it is returned unchanged. Otherwise it is cut so
// that s + truncateSuffix fits within MaxReplyChars.
func TruncateReply(s string) string {
	if len(s) <= MaxReplyChars {
		return s
	}
	keep := max(MaxReplyChars-len(truncateSuffix), 0)
	return s[:keep] + truncateSuffix
}
