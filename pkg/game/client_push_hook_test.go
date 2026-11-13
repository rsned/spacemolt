package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// Pathfinder sweep ships land in uncharted systems that broadcast encoded
// messages (2026-11-13); fleet workers dropped every push the client had no
// dedicated handler for. SetOnPush hands each non-routine push to a recorder:
// unknown types included, command replies and per-tick noise excluded.
func TestSetOnPushForwardsEventsNotReplies(t *testing.T) {
	c := newHandleResponseTestClient("bare_lamp")
	var got []string
	c.SetOnPush(func(typ string, payload map[string]any) { got = append(got, typ) })
	for _, typ := range []string{
		protocol.TypeTick, protocol.TypeOK, protocol.TypeActionResult, protocol.TypeStateUpdate,
		protocol.TypeChatMessage, // has its own hook (SetOnChatMessage)
		protocol.TypeAchievementUnlocked,
		"signal_fragment", // a type this client has never heard of
	} {
		c.handleResponse(protocol.Response{Type: typ, Payload: map[string]any{"message": "x"}})
	}
	want := []string{protocol.TypeAchievementUnlocked, "signal_fragment"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("forwarded %v, want %v", got, want)
	}
}
