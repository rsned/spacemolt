package game

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStoreRawJSON_FacilityBrowseForSaleAlsoKeyedFacility guards the regression
// where `facility browse_for_sale` (a type=ok payload carrying a "listings"
// field) was stored ONLY under "listings", so play_as's facility-command lookup
// (GetRawJSON("facility")) returned a stale `facility list` response instead.
// The response must also be retrievable under "facility".
func TestStoreRawJSON_FacilityBrowseForSaleAlsoKeyedFacility(t *testing.T) {
	c := &Client{
		latestRawJSON: make(map[string][]byte),
		debugLogger:   log.New(io.Discard, "", 0),
	}

	// A prior `facility list` lands under "facility".
	c.storeRawJSON(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"base_id": "grand_exchange_station", "player_facilities": []any{}, "faction_facilities": []any{}},
	})

	// Now browse_for_sale arrives — its own response, carrying "listings".
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action": "browse_for_sale", "base_id": "grand_exchange_station",
			"base_name": "Grand Exchange Station", "count": float64(0), "listings": []any{},
		},
	})

	// Retrievable under "facility" and reflecting the browse_for_sale response.
	got := string(c.GetRawJSON("facility"))
	if !strings.Contains(got, "browse_for_sale") {
		t.Errorf("GetRawJSON(\"facility\") did not return the browse_for_sale response: %q", got)
	}
	if strings.Contains(got, "player_facilities") {
		t.Errorf("GetRawJSON(\"facility\") still holds the stale facility list: %q", got)
	}
	// Still also available under the content-shape "listings" key.
	if l := string(c.GetRawJSON("listings")); !strings.Contains(l, "browse_for_sale") {
		t.Errorf("GetRawJSON(\"listings\") should also hold browse_for_sale: %q", l)
	}
}
