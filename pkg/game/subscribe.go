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

// SetOnPushEvent registers handler for every untagged server push — the
// server-initiated frames that carry no request_id, which is the whole combat
// family (battle_started/update/damage/ended), player_died, the pirate and
// police events, skill_level_up and server_restart_warning.
//
// The client decodes all of these into serverapi structs and then logs them at
// debug level only, so a caller that does not subscribe sees nothing. Push
// subscriptions observe rather than consume: the typed listeners
// (SetOnChatMessage, SetOnCraftingUpdate, ...) and the command reply paths
// still see the same frames.
//
// The handler runs synchronously in the router's dispatch path — keep it fast,
// and never issue a game command from inside it. Returns an idempotent cancel.
func (c *Client) SetOnPushEvent(handler func(resp protocol.Response)) func() {
	return c.subscribePush(func(protocol.Response) bool { return true }, handler)
}
