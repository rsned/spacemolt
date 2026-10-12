package game

import (
	"bytes"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// facilityEventClient returns a client whose eventLogger writes to buf, with
// debug logging pointed at io.Discard — the configuration every worker actually
// runs in.
func facilityEventClient(buf *bytes.Buffer) *Client {
	return &Client{
		debugLogger:   log.New(io.Discard, "", 0),
		eventLogger:   log.New(buf, "[GAME] ", 0),
		latestRawJSON: make(map[string][]byte),
		state:         &State{},
	}
}

func TestFacilityReclaimedTypeConstant(t *testing.T) {
	if protocol.TypeFacilityReclaimed != "facility_reclaimed" {
		t.Fatalf("TypeFacilityReclaimed = %q, want %q", protocol.TypeFacilityReclaimed, "facility_reclaimed")
	}
}

// A repossession is permanent and irreversible: the facility is gone and must be
// rebuilt and paid for again. It must reach the operational log, not the debug
// log that is switched off in production.
func TestFacilityReclaimedReachesEventLog(t *testing.T) {
	var buf bytes.Buffer
	c := facilityEventClient(&buf)
	c.handleResponse(protocol.Response{
		Type: protocol.TypeFacilityReclaimed,
		Payload: map[string]any{
			"base_id":    "voss_redoubt_station",
			"base_name":  "Voss Redoubt Station",
			"faction_id": "e727c0e918d994c72db2978fe5b18edc",
			"facilities": []any{"Backwater Still"},
			"message":    "The station has repossessed 1 of your faction's facilities at Voss Redoubt Station for unpaid rent: Backwater Still.",
		},
	})
	got := buf.String()
	if !strings.Contains(got, "repossessed") {
		t.Errorf("event log = %q, want the repossession message", got)
	}
	if !strings.Contains(got, "Voss Redoubt Station") {
		t.Errorf("event log = %q, want the base named", got)
	}
}

// The server warns before it repossesses. That warning was logged to the debug
// logger only, so it was discarded in every production run -- eleven facilities
// were lost on 2026-10-12 with the warnings going nowhere. It belongs in the
// operational log.
func TestFacilityRentWarningReachesEventLog(t *testing.T) {
	var buf bytes.Buffer
	c := facilityEventClient(&buf)
	c.handleResponse(protocol.Response{
		Type: protocol.TypeFacilityRentWarning,
		Payload: map[string]any{
			"base_id":           "grand_exchange_station",
			"base_name":         "Grand Exchange Station",
			"credits_owed":      float64(43886),
			"facilities_behind": float64(10),
			"missed_cycles":     float64(32),
			"grace_cycles":      float64(260),
			"message":           "Rent is overdue at Grand Exchange Station.",
		},
	})
	got := buf.String()
	if !strings.Contains(got, "Rent is overdue") {
		t.Errorf("event log = %q, want the rent warning message", got)
	}
}

// Both are unsolicited pushes: they must not clobber the "_last" raw JSON slot
// that interactive tools read after issuing a command.
func TestFacilityEventsArePushOnly(t *testing.T) {
	for _, typ := range []string{protocol.TypeFacilityReclaimed, protocol.TypeFacilityRentWarning} {
		if _, ok := pushOnlyResponseTypes[typ]; !ok {
			t.Errorf("pushOnlyResponseTypes missing %q", typ)
		}
	}
}
