package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The commands below have no dedicated client method but are reachable via
// play_as's raw passthrough, so their responses are typed purely from openapi.
// Nothing exercises those structs at runtime, which means a typo'd or missing
// json tag would never surface — the field would just decode to zero forever.
//
// This pins each struct's JSON tags against the schema openapi actually
// declares. It is the only check these types get.
var passthroughSchemas = map[string]string{
	"faction_garages":           "FactionGaragesResponse",
	"hunt":                      "HuntResponse",
	"build_outpost":             "BuildBaseResponse",
	"buy_ship_license":          "ShipLicenseResponse",
	"place_ship_buy_order":      "PlaceShipBuyOrderResponse",
	"cancel_ship_buy_order":     "CancelShipBuyOrderResponse",
	"view_ship_buy_orders":      "ViewShipBuyOrdersResponse",
	"sell_ship_to_order":        "SellShipToOrderResponse",
	"prepay_tax":                "PrepayTaxResponse",
	"faction_prepay_tax":        "FactionPrepayTaxResponse",
	"faction_scan_poi":          "FactionScanPOIResponse",
	"get_faction_achievements":  "GetFactionAchievementsResponse",
	"get_notification_settings": "NotificationSettingsResponse",
	"station":                   "StationConfigResponse",
	"get_faction_tax_estimate":  "FactionTaxEstimateResponse",
	"get_tax_estimate":          "TaxEstimateResponse",
	"dismantle_outpost":         "DismantleOutpostResponse",
	"login_link":                "LoginLinkResponse",
	// login_link_poll's openapi entry (LoginLinkPollCommandResponse) is a oneOf
	// with no properties of its own, so pin the pending arm — the shape our
	// struct actually models. The approved arm is a plain LoginResponse.
	"login_link_poll": "LoginLinkPollResponse",
}

func TestPassthroughStructsCoverOpenAPISchemas(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "server_docs", "openapi.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("openapi.json not found: %v", err)
	}

	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal openapi.json: %v", err)
	}

	for action, schemaName := range passthroughSchemas {
		schema, ok := doc.Components.Schemas[schemaName]
		if !ok {
			t.Errorf("openapi has no schema %q (for action %q)", schemaName, action)
			continue
		}

		fields, known := expectedFieldsForAction(action)
		if !known {
			t.Errorf("action %q has no entry in actionResponseTypes", action)
			continue
		}

		for prop := range schema.Properties {
			if !fields[prop] {
				t.Errorf("%s: openapi declares %q but the response struct has no matching json tag "+
					"(it would silently decode to the zero value)", action, prop)
			}
		}
	}
}
