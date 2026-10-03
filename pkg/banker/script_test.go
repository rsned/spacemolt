package banker

import (
	"strings"
	"testing"
)

// Recipients are addressed by username, and those carry spaces, apostrophes and
// emoji. play_as SplitArgs is quote-aware, so double-quoting the name is both
// necessary and sufficient -- a bare name with a space would be read as two
// arguments and gift to the wrong player.
func TestScriptQuotesAwkwardUsernames(t *testing.T) {
	s := Script([]Grant{
		{Candidate: Candidate{AgentID: "fighter-3", Username: "Blade 'Battler' Blackwoo"}, Amount: 10000},
		{Candidate: Candidate{AgentID: "pirate-2", Username: "☠"}, Amount: 9500},
	})

	if !strings.Contains(s, `send_gift "Blade 'Battler' Blackwoo" credits 10000`) {
		t.Fatalf("a name with spaces must be double-quoted:\n%s", s)
	}
	if !strings.Contains(s, `send_gift "☠" credits 9500`) {
		t.Fatalf("an emoji name must still be quoted and sent:\n%s", s)
	}
}

// A username containing a double quote cannot be expressed in this script form
// and must be refused loudly rather than emitted broken -- a mangled recipient
// sends real credits to the wrong player.
func TestScriptRefusesAUsernameItCannotQuote(t *testing.T) {
	s := Script([]Grant{
		{Candidate: Candidate{AgentID: "odd", Username: `say "hi"`}, Amount: 100},
	})

	if strings.Contains(s, "send_gift") {
		t.Fatalf("an unquotable name must not produce a gift line:\n%s", s)
	}
	if !strings.Contains(s, "SKIPPED") {
		t.Fatalf("the refusal must be visible in the script:\n%s", s)
	}
}
