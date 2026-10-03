package banker

import "testing"

// A candidate with no known username must NOT fall back to its agent id:
// send_gift would address a different player or fail outright.
func TestAttachLeavesAnUnknownAgentUnaddressable(t *testing.T) {
	got := Attach([]Candidate{{AgentID: "craftsman-1"}, {AgentID: "mystery"}},
		Usernames{"craftsman-1": "Arthur 'Artificer' Artis"})

	if got[0].Username != "Arthur 'Artificer' Artis" {
		t.Fatalf("known username must be attached: %+v", got[0])
	}
	if got[1].Username != "" {
		t.Fatalf("unknown agent must stay unaddressable, got %q", got[1].Username)
	}
}
