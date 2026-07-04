package knowledge

import (
	"encoding/json"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ShipListingFromDetail maps a browse_ships wire listing to the KB row shape.
// The wire Hull field is int/omitempty, so absent and 0 are indistinguishable;
// a for-sale hull of literal 0 does not occur, so both map to -1 (not
// reported).
func ShipListingFromDetail(d serverapi.ShipListingDetail) ShipListing {
	hull := d.Hull
	if hull == 0 {
		hull = -1
	}
	return ShipListing{
		ListingID:    d.ListingID,
		ShipID:       d.ShipID,
		ClassID:      d.ClassID,
		ShipName:     d.ShipName,
		Category:     d.Category,
		Tier:         d.Tier,
		Scale:        d.Scale,
		Hull:         hull,
		MaxHull:      d.MaxHull,
		Shield:       d.Shield,
		ModulesCount: d.ModulesCount,
		Price:        d.Price,
		Seller:       d.Seller,
		ListedAt:     d.ListedAt,
	}
}

// ShipListingsFromBrowseJSON decodes a raw browse_ships response payload
// (raw-store key "ship_listings") into KB listing rows. Returns the base id
// the server reported alongside the rows.
func ShipListingsFromBrowseJSON(raw []byte) (string, []ShipListing, error) {
	var resp serverapi.BrowseShipsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", nil, fmt.Errorf("parse browse_ships response: %w", err)
	}
	ships := make([]ShipListing, 0, len(resp.Listings))
	for _, d := range resp.Listings {
		ships = append(ships, ShipListingFromDetail(d))
	}
	return resp.BaseID, ships, nil
}
