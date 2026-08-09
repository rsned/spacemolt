package serverapi

import (
	"encoding/json"
	"testing"
)

// FlexID exists because a real get_battle_status reply sends side_id as a
// NUMBER while the struct field was a string — an UnmarshalTypeError that
// failed the whole reply and blanked the caller's battle picture. So the
// property under test is not "it parses numbers"; it is "no input shape can
// fail the enclosing decode".
func TestFlexID_AcceptsEveryShapeWithoutFailingTheDecode(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  FlexID
	}{
		{"number", `1`, "1"},
		{"negative number", `-3`, "-3"},
		{"float", `1.5`, "1.5"},
		{"string", `"1"`, "1"},
		{"non-numeric string", `"side-a"`, "side-a"},
		{"empty string", `""`, ""},
		// Shapes nobody has observed. They must not error: an error here would
		// fail the entire reply, which is the failure this type prevents.
		{"null", `null`, ""},
		{"bool", `true`, ""},
		{"object", `{"id":1}`, ""},
		{"array", `[1]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got FlexID
			if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
				t.Fatalf("Unmarshal(%s) errored: %v — an error fails the whole enclosing reply", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("Unmarshal(%s) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// The enclosing-reply property, stated directly: a participant carrying an
// unreadable side_id still decodes, and the fields around it survive.
func TestFlexID_UnreadableSideIDDoesNotBlankTheReply(t *testing.T) {
	const reply = `{"battle_id":"b1","is_participant":true,"participants":[{"player_id":"me","side_id":{"nested":true},"hull_pct":80,"zone_distance":4}]}`
	var resp GetBattleStatusResponse
	if err := json.Unmarshal([]byte(reply), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(resp.Participants) != 1 {
		t.Fatalf("participants = %d, want 1", len(resp.Participants))
	}
	if resp.Participants[0].SideID != "" {
		t.Errorf("SideID = %q, want empty for an unreadable value", resp.Participants[0].SideID)
	}
	if resp.BattleID != "b1" || resp.Participants[0].HullPct != 80 || resp.Participants[0].ZoneDistance != 4 {
		t.Errorf("the rest of the reply must survive: %+v", resp)
	}
}

// MarshalJSON re-emits the canonical string form.
func TestFlexID_MarshalsAsString(t *testing.T) {
	b, err := json.Marshal(FlexID("2"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(b) != `"2"` {
		t.Errorf("Marshal = %s, want \"2\"", b)
	}
}
