package respfmt

import (
	"strings"
	"testing"
)

func TestCreateBuyOrder_AllSucceeded(t *testing.T) {
	raw := []byte(`{
	  "command": "create_buy_order",
	  "result": {
	    "action": "create_buy_order",
	    "mode": "bulk",
	    "results": [
	      {"index":0,"item":"Iron Ore","item_id":"iron_ore","success":true,"message":"ok"}
	    ],
	    "summary": {"total":1,"succeeded":1,"failed":0}
	  },
	  "tick": 1
	}`)
	got := CreateBuyOrder(raw)
	if !strings.Contains(got, "1/1 succeeded") {
		t.Errorf("expected summary line, got %q", got)
	}
	if strings.Contains(got, "Failures") {
		t.Errorf("did not expect Failures section: %q", got)
	}
}

func TestCreateBuyOrder_WithFailures(t *testing.T) {
	raw := []byte(`{
	  "command": "create_buy_order",
	  "result": {
	    "action": "create_buy_order",
	    "mode": "bulk",
	    "results": [
	      {"index":0,"item":"Iron Ore","item_id":"iron_ore","success":true,"message":"ok"},
	      {"index":1,"item":"Storm Lance","item_id":"storm_lance","success":false,"message":"item is station-only"},
	      {"index":2,"item":"Xenon Gas","item_id":"xenon_gas","success":false,"message":"insufficient credits"}
	    ],
	    "summary": {"total":3,"succeeded":1,"failed":2}
	  }
	}`)
	got := CreateBuyOrder(raw)
	if !strings.Contains(got, "1/3 succeeded, 2 failed") {
		t.Errorf("summary missing in %q", got)
	}
	if !strings.Contains(got, "storm_lance") || !strings.Contains(got, "station-only") {
		t.Errorf("missing storm_lance failure: %q", got)
	}
	if !strings.Contains(got, "xenon_gas") || !strings.Contains(got, "insufficient credits") {
		t.Errorf("missing xenon_gas failure: %q", got)
	}
	// Failures should be sorted by item_id (storm_lance < xenon_gas).
	if strings.Index(got, "storm_lance") > strings.Index(got, "xenon_gas") {
		t.Errorf("failures not sorted by item_id: %q", got)
	}
}

func TestCreateBuyOrder_BareResultBody(t *testing.T) {
	raw := []byte(`{
	  "action": "create_buy_order",
	  "results": [
	    {"index":0,"item":"X","item_id":"x","success":false,"message":"nope"}
	  ],
	  "summary": {"total":1,"succeeded":0,"failed":1}
	}`)
	got := CreateBuyOrder(raw)
	if !strings.Contains(got, "0/1 succeeded, 1 failed") {
		t.Errorf("summary missing: %q", got)
	}
}

func TestCreateBuyOrder_ErrorCodeShape(t *testing.T) {
	// Some failure rows carry error/error_code (no message, no item/item_id),
	// e.g. contraband_restricted on smuggling-gated markets.
	raw := []byte(`{
	  "command": "create_sell_order",
	  "result": {
	    "action": "create_sell_order",
	    "mode": "bulk",
	    "results": [
	      {"index":0,"item":"Iron Ore","item_id":"iron_ore","success":true,"message":"ok"},
	      {"index":1,"error":"Void Dust is contraband...","error_code":"contraband_restricted","success":false}
	    ],
	    "summary": {"total":2,"succeeded":1,"failed":1}
	  }
	}`)
	got := CreateSellOrder(raw)
	if !strings.Contains(got, "1/2 succeeded, 1 failed") {
		t.Errorf("summary missing: %q", got)
	}
	if !strings.Contains(got, "contraband_restricted") {
		t.Errorf("expected error_code in output: %q", got)
	}
	if !strings.Contains(got, "Void Dust") {
		t.Errorf("expected error text in output: %q", got)
	}
	if !strings.Contains(got, "(index 1)") {
		t.Errorf("expected (index N) fallback id when item_id missing: %q", got)
	}
}

func TestCreateBuyOrder_Garbage(t *testing.T) {
	if got := CreateBuyOrder([]byte(`not json`)); got != "" {
		t.Errorf("expected empty for garbage, got %q", got)
	}
	if got := CreateBuyOrder([]byte(`{}`)); got != "" {
		t.Errorf("expected empty for empty object, got %q", got)
	}
}
