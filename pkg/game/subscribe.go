package game

import (
	"github.com/rsned/spacemolt/internal/protocol"
)

// subscribePush registers a long-lived handler for responses
// satisfying match. Handlers run synchronously in the router's
// dispatch path — keep them fast. Returns a cancel function the
// caller must invoke to stop delivery; idempotent.
//
// Used by OnChatMessage, OnStorageUpdate, and other event listeners.
// Untagged server pushes (no request_id) flow through this path.
func (c *Client) subscribePush(
	match Classifier,
	handler func(protocol.Response),
) func() {
	sub := c.router.registerPush(match, handler)
	return func() {
		c.router.unregister(sub)
	}
}
