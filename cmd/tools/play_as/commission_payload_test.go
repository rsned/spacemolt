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

// commission_quote sent only ship_class, so `commission_quote axiomata
// --bare_hull=true` quoted the fitted ship (2026-11-16). It shares the
// commission_ship flag parsing now.
func TestCommissionQuotePayloadSendsBareHull(t *testing.T) {
	p, err := commissionQuotePayload([]string{"commission_quote", "Axiomata", "--bare_hull=true", "--source_missing_materials"})
	if err != nil {
		t.Fatal(err)
	}
	if p["ship_class"] != "axiomata" || p["bare_hull"] != true || p["source_missing_materials"] != true {
		t.Errorf("payload = %#v, want axiomata with bare_hull and source_missing_materials true", p)
	}
}

func TestCommissionQuotePayloadNeedsClass(t *testing.T) {
	if _, err := commissionQuotePayload([]string{"commission_quote", "--bare_hull"}); err == nil {
		t.Error("want an error when no ship class is given")
	}
}

// A bare boolean flag means true. Before, `--bare_hull` as the last token was
// dropped, and `--bare_hull --provide_materials true` took "--provide_materials"
// as bare_hull's value and lost provide_materials.
func TestCommissionShipPayloadBareFlagIsTrue(t *testing.T) {
	p, err := commissionShipPayload([]string{"commission_ship", "axiomata", "--bare_hull", "--provide_materials", "true"})
	if err != nil {
		t.Fatal(err)
	}
	if p["bare_hull"] != true || p["provide_materials"] != true {
		t.Errorf("payload = %#v, want bare_hull and provide_materials true", p)
	}
	p, err = commissionShipPayload([]string{"commission_ship", "axiomata", "--bare_hull"})
	if err != nil {
		t.Fatal(err)
	}
	if p["bare_hull"] != true {
		t.Errorf("trailing --bare_hull = %#v, want true", p["bare_hull"])
	}
}
