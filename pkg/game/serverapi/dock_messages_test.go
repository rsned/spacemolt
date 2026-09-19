package serverapi

import (
	"encoding/json"
	"testing"
)

// The dock reply carries a station mailbox the client silently dropped: on
// 2026-09-19 a live dock returned messages/messages_count that DockResponse had
// no fields for, and the two notices it held were Station Authority rent
// warnings ("1/260 cycles, 172 credits owed ... at 260 consecutive missed
// cycles the station will repossess them"). Those are unrecoverable-loss
// warnings on a countdown, delivered nowhere else, and we were discarding them.
func TestDockResponseDecodesStationMessages(t *testing.T) {
	const payload = `{
		"action": "dock",
		"messages": [
			{"body": "Rent payment missed on 1 of your facilities at Grand Exchange Station (1/260 cycles, 172 credits owed).",
			 "from": "Station Authority",
			 "timestamp": "2026-09-19T00:41:34.834167703Z"},
			{"body": "Rent payment missed on 1 of your facilities at Grand Exchange Station (1/260 cycles, 172 credits owed).",
			 "from": "Station Authority",
			 "timestamp": "2026-09-19T02:04:55.139789279Z"}
		],
		"messages_count": 2
	}`

	var got DockResponse
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.MessagesCount != 2 {
		t.Errorf("MessagesCount = %d, want 2", got.MessagesCount)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(got.Messages))
	}
	first := got.Messages[0]
	if first.From != "Station Authority" {
		t.Errorf("From = %q, want %q", first.From, "Station Authority")
	}
	if first.Timestamp != "2026-09-19T00:41:34.834167703Z" {
		t.Errorf("Timestamp = %q", first.Timestamp)
	}
	if first.Body == "" || !contains(first.Body, "172 credits owed") {
		t.Errorf("Body = %q, want the rent warning text", first.Body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
