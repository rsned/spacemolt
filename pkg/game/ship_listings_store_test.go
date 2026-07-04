package game

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStoreRawJSONBrowseShips guards against the raw-key drift that killed
// ship-listing capture 2026-02-18..2026-07-04: browse_ships responses
// ({base_id, base_name, count, listings}, NO action field) must land under a
// dedicated "ship_listings" key, while facility sub-actions with the same
// content shape (always carrying "action") keep their existing keys.
func TestStoreRawJSONBrowseShips(t *testing.T) {
	c := &Client{
		latestRawJSON: make(map[string][]byte),
		debugLogger:   log.New(io.Discard, "", 0),
	}

	// browse_ships response (live shape 2026-07-04): no "action" field.
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"base_id": "nyx_nexus_station", "base_name": "Nyx Nexus Station",
			"count": float64(1),
			"listings": []any{map[string]any{
				"listing_id": "70b2ce92", "ship_id": "720608fd",
				"class_id": "eviction_notice", "price": float64(133174),
			}},
		},
	})
	got := string(c.GetRawJSON("ship_listings"))
	if !strings.Contains(got, "eviction_notice") {
		t.Errorf("GetRawJSON(\"ship_listings\") missing browse_ships payload: %q", got)
	}
	if l := c.GetRawJSON("listings"); l != nil {
		t.Errorf("browse_ships must not land under \"listings\", got: %q", string(l))
	}

	// facility browse_for_sale: same content shape but has "action" — must
	// NOT be classified as ship_listings.
	c2 := &Client{
		latestRawJSON: make(map[string][]byte),
		debugLogger:   log.New(io.Discard, "", 0),
	}
	c2.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action": "browse_for_sale", "base_id": "grand_exchange_station",
			"base_name": "Grand Exchange Station", "count": float64(0), "listings": []any{},
		},
	})
	if s := c2.GetRawJSON("ship_listings"); s != nil {
		t.Errorf("facility browse_for_sale must not be keyed ship_listings: %q", string(s))
	}
	if l := string(c2.GetRawJSON("listings")); !strings.Contains(l, "browse_for_sale") {
		t.Errorf("facility browse_for_sale must stay under \"listings\": %q", l)
	}
}
