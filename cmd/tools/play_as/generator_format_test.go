package main

import (
	"strings"
	"testing"
)

func TestFormatGenerator_Connected(t *testing.T) {
	raw := []byte(`{"command":"generator","result":{"connected":true,"base_id":"c976fa7df53c87f9616165d7d411b41a","output":12,"end_tick":2352500,"fuel_per_tick":1,"fuel_reserve":11,"fuel_burned":0,"power_delivered":0,"message":"Field generator linked to the station grid."}}`)
	out := formatGenerator(raw)
	for _, want := range []string{"CONNECTED", "output 12", "1 fuel/tick", "reserve 11", "until tick 2352500", "Field generator linked"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot: %q", want, out)
		}
	}
}

func TestFormatGenerator_Disconnected(t *testing.T) {
	raw := []byte(`{"command":"generator","result":{"connected":false,"base_id":"c976fa7df53c87f9616165d7d411b41a","output":0,"fuel_per_tick":0,"fuel_reserve":11,"fuel_burned":42,"power_delivered":504,"message":"Generator disconnected."}}`)
	out := formatGenerator(raw)
	for _, want := range []string{"DISCONNECTED", "504 power delivered", "42 fuel burned"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot: %q", want, out)
		}
	}
	if strings.Contains(out, "until tick") {
		t.Errorf("disconnected should not show a timeout tick, got %q", out)
	}
}

func TestFormatGenerator_BadPayload(t *testing.T) {
	if out := formatGenerator([]byte(`not json`)); out != "" {
		t.Errorf("bad payload should fall back to JSON, got %q", out)
	}
}
