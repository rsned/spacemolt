package game

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestV0687PushTypesReachEventLog(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		payload map[string]any
		want    []string
	}{
		{"faction_kick", protocol.TypeFactionKick,
			map[string]any{"faction_name": "Iron Pact", "kicked_by": "boss"},
			[]string{"FACTION KICK", "Iron Pact", "boss"}},
		{"faction_withdraw_invite", protocol.TypeFactionWithdrawInvite,
			map[string]any{"faction_id": "f1", "faction_name": "Iron Pact", "withdrawn_by": "boss"},
			[]string{"INVITE WITHDRAWN", "Iron Pact", "f1", "boss"}},
		{"faction_mobile_base_update", protocol.TypeFactionMobileBaseUpdate,
			map[string]any{"event": "jump_arrived", "message": "Civitas has arrived in sol.",
				"mobile_base": map[string]any{"system_id": "sol"}},
			[]string{"MOBILE BASE jump_arrived", "Civitas has arrived", "[system sol]"}},
		{"pathfinder_storm", protocol.TypePathfinderStorm,
			map[string]any{"deflected": true, "message": "The storm pushed you off course."},
			[]string{"PATHFINDER STORM", "deflected=true", "off course"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := eventExpectedFields[tt.typ]; !ok {
				t.Errorf("%s missing from eventExpectedFields", tt.typ)
			}
			if _, ok := pushOnlyResponseTypes[tt.typ]; !ok {
				t.Errorf("%s missing from pushOnlyResponseTypes", tt.typ)
			}
			var buf bytes.Buffer
			c := facilityEventClient(&buf)
			c.handleResponse(protocol.Response{Type: tt.typ, Payload: tt.payload})
			got := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("event log %q missing %q", got, w)
				}
			}
		})
	}
}
