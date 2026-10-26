package main

import (
	"slices"
	"testing"
)

// The station command takes its sub-action as `action`; the generic
// passthrough would send a bare positional as arg1 and the server answers
// invalid_payload (seen twice live, 2026-10-25).
func TestStationParts_PositionalBecomesAction(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"station", "info"}, []string{"station", "--action", "info"}},
		{[]string{"station", "set_market_fee", "--fee", "5"}, []string{"station", "--action", "set_market_fee", "--fee", "5"}},
		{[]string{"station", "--action", "info"}, []string{"station", "--action", "info"}},
		{[]string{"station"}, []string{"station"}},
	}
	for _, c := range cases {
		if got := stationParts(c.in); !slices.Equal(got, c.want) {
			t.Errorf("stationParts(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestStationPayload_TypesBoolsAndNumbers(t *testing.T) {
	p, err := stationPayload(stationParts([]string{"station", "set_public", "--public", "false"}))
	if err != nil {
		t.Fatal(err)
	}
	if p["action"] != "set_public" || p["public"] != false {
		t.Errorf("payload = %#v, want action set_public, public bool false", p)
	}

	p, err = stationPayload(stationParts([]string{"station", "set_market_fee", "--fee_percent", "3"}))
	if err != nil {
		t.Fatal(err)
	}
	if p["fee_percent"] != 3 {
		t.Errorf("fee_percent = %#v, want int 3", p["fee_percent"])
	}

	p, err = stationPayload(stationParts([]string{"station", "set_auto_buy_fuel", "--auto_buy_fuel"}))
	if err != nil {
		t.Fatal(err)
	}
	if p["auto_buy_fuel"] != true {
		t.Errorf("bare --auto_buy_fuel = %#v, want true", p["auto_buy_fuel"])
	}
}

// Names and ids are text even when they are all digits.
func TestStationPayload_TextStaysString(t *testing.T) {
	p, err := stationPayload(stationParts([]string{"station", "allow_player", "--player", "12345"}))
	if err != nil {
		t.Fatal(err)
	}
	if p["player"] != "12345" {
		t.Errorf("player = %#v, want string \"12345\"", p["player"])
	}
}

func TestStationPayload_ContrabandArray(t *testing.T) {
	p, err := stationPayload(stationParts([]string{"station", "set_contraband", "--contraband_items", "pirate_moonshine,void_spice"}))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := p["contraband_items"].([]string); !ok || !slices.Equal(got, []string{"pirate_moonshine", "void_spice"}) {
		t.Errorf("contraband_items = %#v", p["contraband_items"])
	}
}

// An empty list ALLOWS ALL items, which is different from null (default
// rules), so "[]" must go out as an empty array, never null.
func TestStationPayload_EmptyContrabandIsEmptyArray(t *testing.T) {
	p, err := stationPayload(stationParts([]string{"station", "set_contraband", "--contraband_items", "[]"}))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := p["contraband_items"].([]string)
	if !ok || got == nil || len(got) != 0 {
		t.Errorf("contraband_items = %#v, want non-nil empty []string", p["contraband_items"])
	}
}

func TestStationPayload_BadBool(t *testing.T) {
	if _, err := stationPayload(stationParts([]string{"station", "set_public", "--public", "maybe"})); err == nil {
		t.Error("want error for --public maybe")
	}
}

func TestStationPayload_DefaultsToInfo(t *testing.T) {
	p, err := stationPayload([]string{"station"})
	if err != nil {
		t.Fatal(err)
	}
	if p["action"] != "info" {
		t.Errorf("bare station action = %#v, want info", p["action"])
	}
}
