package serverapi

import (
	"encoding/json"
	"testing"
)

// A distress broadcast arrives as a chat_message on the "emergency" channel and
// carries the rescue MISSION it generated (DistressSignalResponse.MissionsSent
// is the sender's side of the same event). ChatMessage decoded none of the
// distress fields: the mission_id, the distress_type, and the human-readable
// system were all dropped, so a listener could see the shout but had no way to
// accept the job it advertises.
//
// Observed live 2026-09-20:
//
//	MAYDAY: Wexler WGM-WG is stranded at The Levy Customs Station in
//	The Levy with 3/120 fuel! Any pilots nearby, please help!
func TestChatMessageDecodesDistressFields(t *testing.T) {
	const payload = `{
		"channel": "emergency",
		"content": "MAYDAY: Wexler WGM-WG is stranded at The Levy Customs Station in The Levy with 3/120 fuel! Any pilots nearby, please help!",
		"distress_type": "fuel",
		"mission_id": "c76bc49152c25cc2f43d2402f2522290",
		"sender": "Wexler WGM-WG",
		"system": "The Levy",
		"system_id": "the_levy"
	}`

	var got ChatMessage
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.MissionID != "c76bc49152c25cc2f43d2402f2522290" {
		t.Errorf("MissionID = %q, want the rescue mission id", got.MissionID)
	}
	if got.DistressType != "fuel" {
		t.Errorf("DistressType = %q, want %q", got.DistressType, "fuel")
	}
	if got.System != "The Levy" {
		t.Errorf("System = %q, want %q", got.System, "The Levy")
	}
	// Fields that already worked must keep working.
	if got.Channel != "emergency" || got.SystemID != "the_levy" || got.Sender != "Wexler WGM-WG" {
		t.Errorf("channel/system_id/sender = %q/%q/%q", got.Channel, got.SystemID, got.Sender)
	}
}
