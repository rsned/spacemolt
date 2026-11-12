package game

import (
	"encoding/json"
	"strings"
	"testing"
)

// The live payload from 2026-11-12 renders every field an operator needs.
func TestFormatTradeOfferLivePayload(t *testing.T) {
	var payload map[string]any
	raw := `{"expires_at":"2026-11-12T21:20:40.950969379Z","hold":false,"note":"Stranded with 0 fuel at Ironbelt Range. Need 1 fuel cell to reach the next station. 5,000cr.","offer_credits":5000,"offer_items":[],"offer_safe_passage":false,"offerer_id":"fa4201d42cf90f8c0939ee4db7d05048","offerer_name":"OreCommander","request_credits":0,"request_items":[{"item_id":"fuel_cell","quantity":1}],"trade_id":"2ef929ac46674a6ce908a7c2a89ffc3e"}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	got := formatTradeOffer(payload)
	for _, want := range []string{"OreCommander", "Stranded with 0 fuel", "offers 5000 cr", "wants 1 x fuel_cell", "expires 2026-11-12T21:20", "2ef929ac46674a6ce908a7c2a89ffc3e"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestFormatTradeOfferSparsePayload(t *testing.T) {
	got := formatTradeOffer(map[string]any{})
	if !strings.Contains(got, "an unknown player") || !strings.Contains(got, "offers nothing; wants nothing") {
		t.Fatalf("got %q", got)
	}
}
