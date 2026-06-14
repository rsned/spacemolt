package main

import (
	"strings"
	"testing"
)

func TestFormatCloak_Engaged(t *testing.T) {
	// action_result-wrapped frame, as delivered over the wire.
	raw := []byte(`{"command":"cloak","result":{"cloak_strength":40,"enabled":true,"message":"Cloaking device engaged. Cloak strength: 40. Consuming 1 fuel/tick."},"tick":1085943}`)
	out := formatCloak(raw)
	for _, want := range []string{"ENGAGED", "strength 40", "Cloaking device engaged"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot: %q", want, out)
		}
	}
}

func TestFormatCloak_Disengaged(t *testing.T) {
	raw := []byte(`{"command":"cloak","result":{"enabled":false,"message":"Cloaking device disengaged."}}`)
	out := formatCloak(raw)
	if !strings.Contains(out, "DISENGAGED") {
		t.Errorf("expected DISENGAGED, got %q", out)
	}
	if strings.Contains(out, "strength") {
		t.Errorf("should not show strength when disengaged, got %q", out)
	}
}

func TestFormatCloak_BadPayload(t *testing.T) {
	if got := formatCloak([]byte(`not json`)); got != "" {
		t.Errorf("expected empty for invalid json, got %q", got)
	}
}
