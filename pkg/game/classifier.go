package game

import "github.com/rsned/spacemolt/internal/protocol"

// Classifier decides whether a given response should be delivered to a
// subscriber. Used by queries, pushes, and as the "match mine" gate for
// mutations. Zero allocations by convention: classifiers are cheap closures.
type Classifier func(resp protocol.Response) bool

// matchType matches responses whose top-level Type equals t.
func matchType(t string) Classifier {
	return func(resp protocol.Response) bool {
		return resp.Type == t
	}
}

// matchAction matches responses whose Payload["action"] equals name.
// Returns false for nil/missing payload or non-string action values.
func matchAction(name string) Classifier {
	return func(resp protocol.Response) bool {
		v, ok := resp.Payload["action"].(string)
		return ok && v == name
	}
}

// matchCommand matches responses whose Payload["command"] equals name.
// Used to correlate mutation replies to the issuing command.
func matchCommand(name string) Classifier {
	return func(resp protocol.Response) bool {
		v, ok := resp.Payload["command"].(string)
		return ok && v == name
	}
}

// matchChannel matches responses whose Payload["channel"] equals channel.
// Used for per-channel correlation (chat_history, chat_message).
func matchChannel(channel string) Classifier {
	return func(resp protocol.Response) bool {
		v, ok := resp.Payload["channel"].(string)
		return ok && v == channel
	}
}

// matchPayloadKey matches responses whose Payload contains key. Used when the
// response carries neither an "action" nor a "command" field, so we fall back
// to payload shape (e.g., get_cargo is identified by the "cargo" key).
func matchPayloadKey(key string) Classifier {
	return func(resp protocol.Response) bool {
		_, ok := resp.Payload[key]
		return ok
	}
}

// matchAll returns a Classifier that matches only when every supplied
// classifier matches. Short-circuits on the first non-match.
func matchAll(cs ...Classifier) Classifier {
	return func(resp protocol.Response) bool {
		for _, c := range cs {
			if !c(resp) {
				return false
			}
		}
		return true
	}
}
