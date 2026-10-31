package main

import "testing"

// commission_ship parsed --fund_from_faction but then sent only ship_class and
// provide_materials, so a faction-funded build at our own station could not be
// requested at all. Every documented flag must reach the payload, booleans as
// booleans.
func TestCommissionShipPayloadKeepsFactionFunding(t *testing.T) {
	p, err := commissionShipPayload([]string{"commission_ship", "congregation", "--fund_from_faction", "true", "--bare_hull=false"})
	if err != nil {
		t.Fatal(err)
	}
	if p["ship_class"] != "congregation" {
		t.Errorf("ship_class = %v", p["ship_class"])
	}
	if p["fund_from_faction"] != true {
		t.Errorf("fund_from_faction = %#v, want true", p["fund_from_faction"])
	}
	if p["bare_hull"] != false {
		t.Errorf("bare_hull = %#v, want false", p["bare_hull"])
	}
	if _, ok := p["provide_materials"]; ok {
		t.Errorf("provide_materials sent although not given: %#v", p)
	}
}

func TestCommissionShipPayloadAllBooleanFlags(t *testing.T) {
	p, err := commissionShipPayload([]string{"commission_ship", "capacity", "--provide_materials=true", "--source_missing_materials", "1"})
	if err != nil {
		t.Fatal(err)
	}
	if p["provide_materials"] != true || p["source_missing_materials"] != true {
		t.Errorf("payload = %#v, want both booleans true", p)
	}
}

func TestCommissionShipPayloadNeedsClass(t *testing.T) {
	if _, err := commissionShipPayload([]string{"commission_ship", "--fund_from_faction", "true"}); err == nil {
		t.Error("want an error when no ship class is given")
	}
}
