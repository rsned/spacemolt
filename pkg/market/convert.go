package market

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// OrdersFromListings maps game market listings to market.Order rows.
// source tags provenance (e.g. "play_as", "agent", "worker").
func OrdersFromListings(stationID string, gameListings []game.MarketListing, source string, capturedAt time.Time) []Order {
	orders := make([]Order, 0, len(gameListings))
	for _, l := range gameListings {
		orders = append(orders, Order{
			StationID:  stationID,
			ItemID:     l.ItemID,
			ItemName:   l.ItemID, // No separate name in game.MarketListing; use ItemID.
			Side:       l.Type,   // "buy" or "sell"
			PriceEach:  l.PricePerUnit,
			Quantity:   l.Quantity,
			Source:     source,
			CapturedAt: capturedAt,
		})
	}
	return orders
}
