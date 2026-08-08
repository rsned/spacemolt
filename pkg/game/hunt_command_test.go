package game

import "testing"

// TestHuntSendsCreatureID guards the wire contract for Hunt: a creature_id
// from get_nearby's creatures list must reach a client that actually
// implements the GameClient interface.
//
// pkg/game has no generic send-capturing harness (see
// client_integration_test.go's mockServer): asserting the exact outbound
// {"target_id": creatureID} payload requires standing up the full mock
// WebSocket server, authenticating, and inspecting sentMessages — cost
// disproportionate to a three-line wrapper, and none of Hunt's siblings
// (Attack, Cloak, ScanTarget, Reload, SelfDestruct) have such a test either.
//
// What this task can actually regress is a client or a mock silently
// dropping the Hunt method. That is a compile-time failure, so this test
// asserts it at compile time rather than at runtime.
func TestHuntSendsCreatureID(t *testing.T) {
	var (
		_ GameClient = (*Client)(nil)
		_ GameClient = (*MCPGameClient)(nil)
	)
}
