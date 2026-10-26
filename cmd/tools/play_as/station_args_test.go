package main

import (
	"slices"
	"strings"
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

// `station set_public true` went out with no `public` key (live 2026-10-25,
// tick 2234173) and the server read the missing value as false -- the opposite
// of what was typed. Bare values must land on the action's parameter.
func TestStationPayload_PositionalValues(t *testing.T) {
	cases := []struct {
		in   []string
		key  string
		want any
	}{
		{[]string{"station", "set_public", "true"}, "public", true},
		{[]string{"station", "set_build_policy", "false"}, "allow_outsiders", false},
		{[]string{"station", "set_market_fee", "3"}, "fee_percent", 3},
		{[]string{"station", "set_refuel_price", "12"}, "price", 12},
		{[]string{"station", "allow_player", "marketbot_home"}, "player", "marketbot_home"},
		{[]string{"station", "set_name", "Conjunction", "Junction"}, "name", "Conjunction Junction"},
		{[]string{"station", "set_contraband", "pirate_moonshine"}, "contraband_items", []string{"pirate_moonshine"}},
	}
	for _, c := range cases {
		p, err := stationPayload(stationParts(c.in))
		if err != nil {
			t.Errorf("%v: %v", c.in, err)
			continue
		}
		got := p[c.key]
		if gs, ok := got.([]string); ok {
			if !slices.Equal(gs, c.want.([]string)) {
				t.Errorf("%v: %s = %#v, want %#v", c.in, c.key, got, c.want)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%v: %s = %#v, want %#v", c.in, c.key, got, c.want)
		}
	}
}

func TestStationPayload_ServiceAccessTwoPositionals(t *testing.T) {
	p, err := stationPayload(stationParts([]string{"station", "set_service_access", "refuel", "public"}))
	if err != nil {
		t.Fatal(err)
	}
	if p["service"] != "refuel" || p["access"] != "public" {
		t.Errorf("payload = %#v", p)
	}
}

// A setter missing its value must not be sent: the server fills the gap with
// a default (false / 0) instead of refusing.
func TestStationPayload_MissingValueRefused(t *testing.T) {
	for _, in := range [][]string{
		{"station", "set_public"},
		{"station", "set_market_fee"},
		{"station", "set_service_access", "refuel"},
		{"station", "ban"},
	} {
		if _, err := stationPayload(stationParts(in)); err == nil || !strings.Contains(err.Error(), "missing") {
			t.Errorf("%v: want missing-value error, got %v", in, err)
		}
	}
	for _, in := range [][]string{{"station", "info"}, {"station", "reset_contraband"}} {
		if _, err := stationPayload(stationParts(in)); err != nil {
			t.Errorf("%v: %v", in, err)
		}
	}
}

func TestStationPayload_NonNumericFee(t *testing.T) {
	if _, err := stationPayload(stationParts([]string{"station", "set_market_fee", "lots"})); err == nil {
		t.Error("want error for non-numeric fee")
	}
}
