package game

import "github.com/rsned/spacemolt/internal/protocol"

// Terminator decides whether a response should end a subscription or query
// wait loop. It complements Classifier: a Classifier selects which responses
// to deliver, while a Terminator decides when to stop waiting for more.
type Terminator func(resp protocol.Response) bool

// terminateOnTypes returns a Terminator that signals termination when the
// response Type matches any of the supplied types. With no types supplied,
// the returned Terminator never terminates.
func terminateOnTypes(types ...string) Terminator {
	set := make(map[string]struct{}, len(types))
	for _, t := range types {
		set[t] = struct{}{}
	}
	return func(resp protocol.Response) bool {
		_, ok := set[resp.Type]
		return ok
	}
}

// terminateNever is a Terminator that never signals termination. It is used
// for long-running push subscriptions that should stay open indefinitely.
func terminateNever(_ protocol.Response) bool { return false }

// terminateAlways is a Terminator that always signals termination. It is
// used for one-shot queries that finish on the very first matching response.
func terminateAlways(_ protocol.Response) bool { return true }
