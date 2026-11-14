package game

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// The payload as the server sent it on 2026-11-14 (event "started").
const holdStartedJSON = `{"event":"started","holder_id":"87d3cf06060578d621b475e0a52ac16d","holder_name":"[CUSTOMS] Federation Customs I - The Levy Customs Station","message":"[CUSTOMS] Federation Customs I - The Levy Customs Station holds you with a warp scrambler for a customs inspection. You cannot jump, travel or dock until the inspection ends.","source":"customs","start_tick":2401224,"target_id":"a50924913cef881c5e4d14257589d9ba","target_name":"Arthur 'Artificer' Artis"}`

func holdPayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHoldUpdateTypeConstant(t *testing.T) {
	if protocol.TypeHoldUpdate != "hold_update" {
		t.Fatalf("TypeHoldUpdate = %q, want hold_update", protocol.TypeHoldUpdate)
	}
}

func TestHoldUpdateDecode(t *testing.T) {
	var h serverapi.HoldUpdate
	if err := json.Unmarshal([]byte(holdStartedJSON), &h); err != nil {
		t.Fatal(err)
	}
	if h.Event != "started" || h.Source != "customs" || h.StartTick != 2401224 ||
		h.HolderID != "87d3cf06060578d621b475e0a52ac16d" || h.TargetID != "a50924913cef881c5e4d14257589d9ba" ||
		h.TargetName != "Arthur 'Artificer' Artis" || !strings.Contains(h.HolderName, "Federation Customs I") ||
		!strings.Contains(h.Message, "cannot jump") {
		t.Fatalf("decoded %+v", h)
	}
}

// A hold start must reach the operational log and be kept on State; an end
// event clears it.
func TestHoldUpdateStartedThenEnded(t *testing.T) {
	var buf bytes.Buffer
	c := facilityEventClient(&buf)
	c.handleResponse(protocol.Response{Type: protocol.TypeHoldUpdate, Payload: holdPayload(t, holdStartedJSON)})

	got := buf.String()
	for _, want := range []string{"[HOLD] ", "customs hold started by [CUSTOMS] Federation Customs I", "cannot jump, travel or dock"} {
		if !strings.Contains(got, want) {
			t.Errorf("event log %q missing %q", got, want)
		}
	}
	if strings.Count(got, "\n") != 1 {
		t.Errorf("want one log line, got %q", got)
	}
	st := c.GetState()
	if st.ActiveHold == nil {
		t.Fatal("ActiveHold not recorded")
	}
	h := *st.ActiveHold
	if h.Source != "customs" || h.StartTick != 2401224 || h.HolderID != "87d3cf06060578d621b475e0a52ac16d" ||
		!strings.Contains(h.HolderName, "The Levy") || h.At.IsZero() {
		t.Fatalf("ActiveHold = %+v", h)
	}
	if !st.Held() {
		t.Error("Held() = false during a hold")
	}
	// Clone must not share the hold.
	st.ActiveHold.Source = "mutated"
	if c.GetState().ActiveHold.Source != "customs" {
		t.Error("State.Clone shares ActiveHold")
	}

	for _, ev := range []string{"ended", "released"} {
		c.handleResponse(protocol.Response{Type: protocol.TypeHoldUpdate, Payload: holdPayload(t, holdStartedJSON)})
		buf.Reset()
		c.handleResponse(protocol.Response{Type: protocol.TypeHoldUpdate, Payload: map[string]any{
			"event": ev, "holder_name": "Federation Customs", "source": "customs",
		}})
		if st := c.GetState(); st.ActiveHold != nil || st.Held() {
			t.Errorf("event %q did not clear the hold: %+v", ev, st.ActiveHold)
		}
		if !strings.Contains(buf.String(), "[HOLD] customs hold "+ev) {
			t.Errorf("event %q log = %q", ev, buf.String())
		}
	}
}

// An event we do not know must log but neither panic nor clear an active hold.
func TestHoldUpdateUnknownEventHarmless(t *testing.T) {
	var buf bytes.Buffer
	c := facilityEventClient(&buf)
	c.handleResponse(protocol.Response{Type: protocol.TypeHoldUpdate, Payload: holdPayload(t, holdStartedJSON)})
	c.handleResponse(protocol.Response{Type: protocol.TypeHoldUpdate, Payload: map[string]any{"event": "progress"}})
	c.handleResponse(protocol.Response{Type: protocol.TypeHoldUpdate, Payload: map[string]any{}})
	if c.GetState().ActiveHold == nil {
		t.Error("unknown event cleared the hold")
	}
	if !strings.Contains(buf.String(), "[HOLD] hold progress") {
		t.Errorf("log = %q", buf.String())
	}
}

// hold_update is an untagged push: SetOnPushEvent hooks must see it, the same
// as hazard_warning.
func TestHoldUpdateReachesPushEvent(t *testing.T) {
	c := newRouterTestClient(nil)
	var got []string
	cancel := c.SetOnPushEvent(func(resp protocol.Response) { got = append(got, resp.Type) })
	defer cancel()
	c.router.dispatch(protocol.Response{Type: protocol.TypeHoldUpdate, Payload: holdPayload(t, holdStartedJSON)})
	if len(got) != 1 || got[0] != protocol.TypeHoldUpdate {
		t.Fatalf("push hook saw %v", got)
	}
	if _, ok := pushOnlyResponseTypes[protocol.TypeHoldUpdate]; !ok {
		t.Error("hold_update missing from pushOnlyResponseTypes")
	}
}
