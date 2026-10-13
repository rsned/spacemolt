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

func TestGiftReceivedTypeConstant(t *testing.T) {
	if protocol.TypeGiftReceived != "gift_received" {
		t.Fatalf("TypeGiftReceived = %q, want %q", protocol.TypeGiftReceived, "gift_received")
	}
}

// Credits arriving by gift are how broke agents and faction treasuries get
// funded, and the worker heartbeat reads a stale 0 balance — so a landed gift
// looks like a failed one. This push is the authoritative confirmation, and it
// must survive with debug OFF.
func TestGiftReceivedCreditsReachEventLog(t *testing.T) {
	var buf bytes.Buffer
	c := facilityEventClient(&buf)
	c.handleResponse(protocol.Response{
		Type: protocol.TypeGiftReceived,
		Payload: map[string]any{
			"credits":   float64(400000),
			"sender":    "Johnny_Cab",
			"sender_id": "3b90160c94a34f159384eae69f3e140c",
			"timestamp": "2026-10-13T01:51:58Z",
		},
	})
	got := buf.String()
	if !strings.Contains(got, "Johnny_Cab") {
		t.Errorf("event log = %q, want the sender named", got)
	}
	if !strings.Contains(got, "400000") && !strings.Contains(got, "400,000") {
		t.Errorf("event log = %q, want the credit amount", got)
	}
}

// An item gift lands in storage at base_id, not in cargo, and a ship gift can
// land at a station other than the one you are docked at. Both have to be
// reported or the assets are invisible until someone happens to look.
func TestGiftReceivedItemsAndShipsNameTheStation(t *testing.T) {
	var buf bytes.Buffer
	c := facilityEventClient(&buf)
	c.handleResponse(protocol.Response{
		Type: protocol.TypeGiftReceived,
		Payload: map[string]any{
			"sender":    "craftsman-1",
			"sender_id": "a50924913cef881c5e4d14257589d9ba",
			"timestamp": "2026-10-13T01:52:00Z",
			"base_id":   "confederacy_central_command",
			"items": []any{
				map[string]any{"item_id": "steel_plate", "name": "Steel Plate", "quantity": float64(200)},
			},
			"ships": []any{
				map[string]any{"ship_id": "s1", "class_id": "congregation", "class_name": "Congregation"},
			},
		},
	})
	got := buf.String()
	for _, want := range []string{"craftsman-1", "steel_plate", "confederacy_central_command", "Congregation"} {
		if !strings.Contains(got, want) {
			t.Errorf("event log = %q, want it to contain %q", got, want)
		}
	}
}

func TestGiftReceivedIsPushOnly(t *testing.T) {
	if _, ok := pushOnlyResponseTypes[protocol.TypeGiftReceived]; !ok {
		t.Error("pushOnlyResponseTypes missing gift_received")
	}
}
