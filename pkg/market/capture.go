package market

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// parseViewMarket parses a raw view_market JSON response into Orders. The
// stationID is stamped on every order; system context is attached by the caller
// on the MarketSnapshot (systemID is not part of an individual order).
func parseViewMarket(raw []byte, stationID string, capturedAt time.Time) ([]Order, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var resp serverapi.ViewMarketResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}

	var orders []Order
	now := capturedAt.UTC()

	for _, item := range resp.Items {
		// Buy orders
		for _, o := range item.BuyOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			orders = append(orders, Order{
				StationID:  stationID,
				ItemID:     item.ItemID,
				ItemName:   item.ItemName,
				Category:   item.Category,
				Side:       "buy",
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}

		// Sell orders
		for _, o := range item.SellOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			orders = append(orders, Order{
				StationID:  stationID,
				ItemID:     item.ItemID,
				ItemName:   item.ItemName,
				Category:   item.Category,
				Side:       "sell",
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}
	}

	return orders, nil
}

// CaptureFromClient captures market data from a game client's last view_market
// response and persists it via the collector. It is a no-op (returns nil) when
// the client has no state, is not at a station, or has no cached market payload.
func CaptureFromClient(ctx context.Context, client game.GameClient, collector *Collector) error {
	state := client.GetState()
	if state == nil {
		return nil
	}

	// Must be at a station
	if state.CurrentPOI == "" {
		return nil
	}

	raw := client.GetRawJSON("market")
	if len(raw) == 0 {
		return nil
	}

	now := time.Now()
	orders, err := parseViewMarket(raw, state.CurrentPOI, now)
	if err != nil {
		return err
	}

	return collector.WriteSnapshot(ctx, snapshotFromState(state, orders, now))
}

// parseGetBaseFuel extracts a station's fuel price from a raw get_base JSON
// response. ok is false when the payload is empty or carries no usable fuel
// price (all-in <= 0), so a station without a fuel pump is skipped.
func parseGetBaseFuel(raw []byte, stationID, capturedBy string, capturedAt time.Time) (StationFuel, bool, error) {
	if len(raw) == 0 {
		return StationFuel{}, false, nil
	}
	var resp serverapi.GetBaseResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return StationFuel{}, false, err
	}
	if resp.FuelPriceAllIn <= 0 {
		return StationFuel{}, false, nil
	}
	sf := StationFuel{
		StationID:      stationID,
		FuelPrice:      int(resp.FuelPrice),
		FuelTaxPerUnit: int(resp.FuelTaxPerUnit),
		FuelPriceAllIn: int(resp.FuelPriceAllIn),
		CapturedAt:     capturedAt.UTC().Format(time.RFC3339),
		CapturedBy:     capturedBy,
		// -1 = this payload didn't report it; UpsertStationFuel keeps the
		// stored value in that case rather than clobbering a real reading.
		FuelReserve:         -1,
		FuelCapacity:        -1,
		FactionFuelReserve:  -1,
		FactionFuelCapacity: -1,
	}
	if resp.Base != nil {
		sf.FactionID = resp.Base.FactionID
		if resp.Base.Fuel != nil {
			sf.FuelReserve = *resp.Base.Fuel
			sf.ReserveObservedAt = sf.CapturedAt
		}
		if resp.Base.MaxFuel != nil {
			sf.FuelCapacity = *resp.Base.MaxFuel
		}
	}
	// A bunker only exists when capacity is reported; reserve 0 with no
	// capacity would otherwise read as a measured-empty bunker.
	if resp.FactionFuelCapacity > 0 {
		sf.FactionFuelReserve = resp.FactionFuelReserve
		sf.FactionFuelCapacity = resp.FactionFuelCapacity
		sf.ReserveObservedAt = sf.CapturedAt
	}
	return sf, true, nil
}

// CaptureFuelFromClient reads the get_base response the client last cached and
// upserts the docked station's fuel price. No-op (nil) when the collector is
// nil, the ship is not at a station, or the payload has no fuel price.
func CaptureFuelFromClient(ctx context.Context, client game.GameClient, collector *Collector, capturedBy string) error {
	if collector == nil {
		return nil
	}
	state := client.GetState()
	if state == nil || state.CurrentPOI == "" {
		return nil
	}
	sf, ok, err := parseGetBaseFuel(client.GetRawJSON("base"), state.CurrentPOI, capturedBy, time.Now())
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return collector.UpsertStationFuel(ctx, sf)
}

// snapshotFromState builds a MarketSnapshot's identity fields from the
// reliable parts of game state. state.CurrentSystem cannot be trusted as a
// system id: get_system/logged_in merges overwrite it with the display name
// ("Krynn", "Alpha Centauri"), which is how display names ended up in
// market.db stations.system_id. Likewise state.CurrentPOI is a slug — or an
// opaque hash for player-owned stations — never a display name, so it must
// not be stored as station_name. Station names resolve through the current
// system's POI list (BaseName, the station's proper name, over the POI's own
// Name), matching what the KBUpdateStation capture path writes so the two
// writers converge on the same row values.
func snapshotFromState(state *game.State, orders []Order, now time.Time) MarketSnapshot {
	systemID := state.System.ID
	if systemID == "" {
		systemID = state.CurrentSystem
	}
	systemName := state.System.Name
	if systemName == "" {
		systemName = systemID
	}

	stationName := state.CurrentPOI
	for _, poi := range state.System.POIs {
		if poi.ID != state.CurrentPOI {
			continue
		}
		switch {
		case poi.BaseName != "":
			stationName = poi.BaseName
		case poi.Name != "":
			stationName = poi.Name
		}
		break
	}

	return MarketSnapshot{
		StationID:   state.CurrentPOI,
		StationName: stationName,
		SystemID:    systemID,
		SystemName:  systemName,
		Orders:      orders,
		CapturedAt:  now,
	}
}
