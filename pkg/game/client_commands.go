package game

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// ============================================================================
// Combat & Battle System
// ============================================================================

// Battle manages tactical battle actions (advance, retreat, stance, target, engage).
func (c *Client) Battle(ctx context.Context, action string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["action"] = action
	msg := protocol.Message{
		Type:      "battle",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Battle subactions (retreat/stance/target) reply with a plain non-pending
	// OK rather than an action_result, so accept either as terminal — otherwise
	// the command hangs until timeout and blocks subsequent actions.
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetBattleStatus queries the current battle state (free query, no tick cost).
// Blocks until the server responds.
func (c *Client) GetBattleStatus(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_battle_status",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepTick))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// Reload reloads a weapon's magazine from ammo in cargo.
func (c *Client) Reload(ctx context.Context, weaponInstanceID, ammoItemID string) error {
	msg := protocol.Message{
		Type: "reload",
		Payload: map[string]any{
			"weapon_instance_id": weaponInstanceID,
			"ammo_item_id":       ammoItemID,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// SelfDestruct destroys your own ship.
func (c *Client) SelfDestruct(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "self_destruct",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// Hunt engages the wildlife creature with the given creature_id (from
// get_nearby's creatures list). Equivalent to Attack on a creature id.
// Hunting resolves on a later tick; the immediate reply only acknowledges
// the command.
func (c *Client) Hunt(ctx context.Context, creatureID string) error {
	msg := protocol.Message{
		Type:      "hunt",
		Payload:   map[string]any{"target_id": creatureID},
		Timestamp: time.Now().UnixMilli(),
	}
	// Same two-frame shape as attack, and the same trap: `ok {pending:true}`
	// then `ok {action:"hunt",...}`, never an action_result. Under the default
	// terminateOnAction neither frame is terminal, the real one is dropped
	// against a full ackCh, and the out-of-reach action_errors that follow
	// each re-arm the deadline to SleepJumpMaxWait.
	//
	// Found by the canary: pirate-6 engaging a Belt-Grazer sat for five
	// minutes emitting "your weapons can't reach" once a tick before the
	// command timed out. The hunt executor calls this before its fight loop
	// begins, so the block lands in front of every engagement.
	h, err := c.Submit(ctx, msg,
		WithTerminator(terminateOnActionOrOK), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// Cloak toggles the cloaking device. Pass true to enable, false to disable.
func (c *Client) Cloak(ctx context.Context, enable bool) error {
	msg := protocol.Message{
		Type:      "cloak",
		Payload:   map[string]any{"enable": enable},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ScanTarget scans a specific target player.
// Note: the server command type is "scan" (same as Scan), not "scan_target".
func (c *Client) ScanTarget(ctx context.Context, targetID string) error {
	msg := protocol.Message{
		Type:      "scan",
		Payload:   map[string]any{"target_id": targetID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Ship Management
// ============================================================================

// BrowseShips browses ships listed for sale at a base.
// Blocks until the server responds.
func (c *Client) BrowseShips(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "browse_ships",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// browse_ships returns type=ok with a "listings" array; request_id
	// correlation makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// BuyListedShip purchases a ship listed by another player.
func (c *Client) BuyListedShip(ctx context.Context, listingID string) error {
	msg := protocol.Message{
		Type:      "buy_listed_ship",
		Payload:   map[string]any{"listing_id": listingID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// BuyShip buys a pre-built ship from the station showroom.
func (c *Client) BuyShip(ctx context.Context, shipClass string) error {
	msg := protocol.Message{
		Type:      "buy_ship",
		Payload:   map[string]any{"ship_class": shipClass},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CancelCommission cancels a pending or in-progress ship commission.
func (c *Client) CancelCommission(ctx context.Context, commissionID string) error {
	msg := protocol.Message{
		Type:      "cancel_commission",
		Payload:   map[string]any{"commission_id": commissionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CancelShipListing removes a ship listing from the exchange.
func (c *Client) CancelShipListing(ctx context.Context, listingID string) error {
	msg := protocol.Message{
		Type:      "cancel_ship_listing",
		Payload:   map[string]any{"listing_id": listingID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CommissionQuote gets a cost estimate for commissioning a ship.
func (c *Client) CommissionQuote(ctx context.Context, shipClass string) error {
	// Submit + WithAckOnly so the caller blocks until the type=ok frame
	// arrives. The previous fire-and-forget `send` returned before
	// storeRawJSON ran, so any downstream lookup (e.g. play_as's
	// showLastResponse) read an empty slot and the styled formatter
	// silently produced nothing.
	msg := protocol.Message{
		Type:      "commission_quote",
		Payload:   map[string]any{"ship_class": shipClass},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CommissionShip commissions a ship to be built at the current shipyard.
func (c *Client) CommissionShip(ctx context.Context, shipClass string, provideMaterials bool) error {
	msg := protocol.Message{
		Type: "commission_ship",
		Payload: map[string]any{
			"ship_class":        shipClass,
			"provide_materials": provideMaterials,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CommissionStatus checks the status of ship commissions.
func (c *Client) CommissionStatus(ctx context.Context, baseID string) error {
	// Submit + WithAckOnly so the caller blocks until this command's own
	// type=ok frame arrives. The previous fire-and-forget `send` unblocked on
	// any ok frame (e.g. a background poller's), so storeRawJSON hadn't run yet
	// and play_as's showLastResponse read an empty slot — the styled formatter
	// silently produced nothing. Mirrors CommissionQuote.
	payload := map[string]any{}
	if baseID != "" {
		payload["base_id"] = baseID
	}
	msg := protocol.Message{
		Type:      "commission_status",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ListShipForSale lists a stored ship for sale on the exchange.
func (c *Client) ListShipForSale(ctx context.Context, shipID string, price float64) error {
	msg := protocol.Message{
		Type: "list_ship_for_sale",
		Payload: map[string]any{
			"ship_id": shipID,
			"price":   price,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// SwitchShip switches to a different ship stored at the current station.
func (c *Client) SwitchShip(ctx context.Context, shipID string) error {
	msg := protocol.Message{
		Type:      "switch_ship",
		Payload:   map[string]any{"ship_id": shipID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// SellShip sells a stored ship at the current station.
//
// RETIRED SERVER-SIDE: sell_ship disappeared from both openapi and
// get_commands in the 2026-07-26 spec sync, so this call now errors. Selling
// goes through the exchange (ListShipForSale / BuyListedShip), straight into a
// standing bid (SellShipToOrder), or ScrapShip for no-payout disposal. Kept
// rather than deleted because nothing automated calls it (only play_as and the
// MCP bridge) and the spec has been wrong before — see BuildBaseResponse. It is
// already gone from pkg/actionspace, so no agent can choose it. Delete this
// method, its interface entry and both mocks once the retirement is confirmed
// against a live server.
func (c *Client) SellShip(ctx context.Context, shipID string) error {
	msg := protocol.Message{
		Type:      "sell_ship",
		Payload:   map[string]any{"ship_id": shipID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ListShips lists all ships owned by the player and their locations.
func (c *Client) ListShips(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "list_ships",
		Timestamp: time.Now().UnixMilli(),
	}
	// list_ships returns type=ok; request_id correlation eliminates the need
	// for a payload-shape classifier.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// UseItem uses a consumable item from cargo.
func (c *Client) UseItem(ctx context.Context, itemID string, quantity int) error {
	payload := map[string]any{"item_id": itemID}
	if quantity > 0 {
		payload["quantity"] = quantity
	}
	msg := protocol.Message{
		Type:      "use_item",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// InstallMod installs a module on the ship.
func (c *Client) InstallMod(ctx context.Context, moduleID string) error {
	msg := protocol.Message{
		Type:      "install_mod",
		Payload:   map[string]any{"module_id": moduleID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// RefitShip refits the active ship to its latest class specifications.
func (c *Client) RefitShip(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "refit_ship",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// UninstallMod uninstalls a module from the ship.
func (c *Client) UninstallMod(ctx context.Context, moduleID string) error {
	msg := protocol.Message{
		Type:      "uninstall_mod",
		Payload:   map[string]any{"module_id": moduleID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Trading & Market
// ============================================================================

// AnalyzeMarket gets actionable trading insights at the current station.
func (c *Client) AnalyzeMarket(ctx context.Context) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "analyze_market",
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// CreateBuyOrder places a buy offer on the station exchange.
func (c *Client) CreateBuyOrder(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "create_buy_order",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CreateSellOrder lists items for sale on the station exchange.
func (c *Client) CreateSellOrder(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "create_sell_order",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CancelOrder cancels an active exchange order and returns escrow.
func (c *Client) CancelOrder(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "cancel_order",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ModifyOrder changes the price on an existing exchange order.
func (c *Client) ModifyOrder(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "modify_order",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ViewMarket views the order book at the current station. The payload
// accepts server-supported filters (item_id, category, etc.); pass nil
// to fetch the full market.
func (c *Client) ViewMarket(ctx context.Context, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	msg := protocol.Message{
		Type:      "view_market",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// view_market returns type=ok; request_id correlation makes the
	// action-field classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ViewOrders views your own orders at the current station.
func (c *Client) ViewOrders(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_orders",
		Timestamp: time.Now().UnixMilli(),
	}
	// view_orders returns type=ok with "orders" array; request_id correlation
	// makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// EstimatePurchase previews what buying would cost without executing.
func (c *Client) EstimatePurchase(ctx context.Context, itemID string, quantity int) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type: "estimate_purchase",
		Payload: map[string]any{
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// GetTrades views pending trade offers.
func (c *Client) GetTrades(ctx context.Context) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "get_trades",
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// TradeOffer offers a trade to another player.
func (c *Client) TradeOffer(ctx context.Context, targetID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["target_id"] = targetID
	msg := protocol.Message{
		Type:      "trade_offer",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// TradeAccept accepts a trade offer.
func (c *Client) TradeAccept(ctx context.Context, tradeID string) error {
	msg := protocol.Message{
		Type:      "trade_accept",
		Payload:   map[string]any{"trade_id": tradeID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// TradeCancel cancels your trade offer.
func (c *Client) TradeCancel(ctx context.Context, tradeID string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "trade_cancel",
		Payload:   map[string]any{"trade_id": tradeID},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// TradeDecline declines a trade offer.
func (c *Client) TradeDecline(ctx context.Context, tradeID string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "trade_decline",
		Payload:   map[string]any{"trade_id": tradeID},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ============================================================================
// Salvage & Towing
// ============================================================================

// GetWrecks lists all wrecks at the current POI.
func (c *Client) GetWrecks(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_wrecks",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_wrecks returns type=ok with a "wrecks" key; request_id correlation
	// makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// LootWreck loots items from a wreck.
func (c *Client) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error {
	// Every key here is optional, and an EMPTY value is not the same as an
	// absent one. The server loots the whole wreck -- all cargo and all modules
	// -- only when item_id and module_id are both absent; item_id:"" names an
	// empty item instead. Likewise an absent wreck_id defaults to the wreck we
	// are towing, while wreck_id:"" names one that does not exist.
	payload := map[string]any{}
	if wreckID != "" {
		payload["wreck_id"] = wreckID
	}
	if itemID != "" {
		payload["item_id"] = itemID
		// quantity is only meaningful alongside an item_id.
		if quantity > 0 {
			payload["quantity"] = quantity
		}
	}
	msg := protocol.Message{
		Type:      "loot_wreck",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// SalvageWreck salvages a wreck for raw materials.
func (c *Client) SalvageWreck(ctx context.Context, wreckID string) error {
	msg := protocol.Message{
		Type:      "salvage_wreck",
		Payload:   map[string]any{"wreck_id": wreckID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// TowWreck attaches a tow line to a wreck for hauling.
func (c *Client) TowWreck(ctx context.Context, wreckID string) error {
	msg := protocol.Message{
		Type:      "tow_wreck",
		Payload:   map[string]any{"wreck_id": wreckID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ReleaseTow releases a towed wreck at the current location.
func (c *Client) ReleaseTow(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "release_tow",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ScrapWreck scraps a towed wreck for salvage materials.
func (c *Client) ScrapWreck(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "scrap_wreck",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// SellWreck sells a towed wreck to the salvage yard for credits.
func (c *Client) SellWreck(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "sell_wreck",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Cargo & Storage
// ============================================================================

// GetCargo gets the ship's cargo contents.
func (c *Client) GetCargo(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_cargo",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_cargo returns type=ok with a "cargo" array; request_id correlation
	// makes the payload-shape classifier redundant. State.Ship.Cargo is
	// populated by parseGetCargoData inside handleResponse, which runs
	// BEFORE router dispatch (see client.go read loop). On nil error from
	// Submit the state write is visible to subsequent GetState() calls — no
	// wall-clock sleep is needed, though GetState() still RLock()s as usual.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// Jettison jettisons items from cargo into space.
func (c *Client) Jettison(ctx context.Context, itemID string, quantity float64) error {
	msg := protocol.Message{
		Type: "jettison",
		Payload: map[string]any{
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ViewStorage views your storage at the current station.
func (c *Client) ViewStorage(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_storage",
		Timestamp: time.Now().UnixMilli(),
	}
	// view_storage returns type=ok; request_id correlation makes the
	// payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ViewStorageAt views your storage at a specific station (without needing to be docked).
func (c *Client) ViewStorageAt(ctx context.Context, stationID string) error {
	msg := protocol.Message{
		Type:      "view_storage",
		Payload:   map[string]any{"station_id": stationID},
		Timestamp: time.Now().UnixMilli(),
	}
	// Same response shape as ViewStorage; request_id correlation suffices.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// WithdrawItems moves items from station storage to cargo.
func (c *Client) WithdrawItems(ctx context.Context, itemID string, quantity float64) error {
	msg := protocol.Message{
		Type: "withdraw_items",
		Payload: map[string]any{
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// WithdrawItemsPayload sends a withdraw_items command with the caller-supplied
// payload. Use this when you need the v2 source/target selectors:
//
//	source: "cargo" (default), "storage" (personal→faction), "faction" (faction→personal)
//	target: "self" (default), "faction", "faction:TAG", or a player name (gift)
//
// Skips the cargo-availability precheck that WithdrawItems does, since
// non-default source/target moves don't read from cargo.
func (c *Client) WithdrawItemsPayload(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "withdraw_items",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// DepositItemsPayload sends a deposit_items command with the caller-supplied
// payload. See WithdrawItemsPayload for the source/target semantics. Skips
// the cargo-availability precheck that DepositItems does.
func (c *Client) DepositItemsPayload(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "deposit_items",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// SendGift sends items or credits to another player's storage at this station.
func (c *Client) SendGift(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "send_gift",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Crafting & Queries
// Note: Craft/CraftWithQuantity are in crafting.go with validation logic.
// ============================================================================

// GetRecipes gets available crafting recipes.
func (c *Client) GetRecipes(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_recipes",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_recipes returns type=ok with a "recipes" key; request_id
	// correlation makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetSkills gets your skill progress.
func (c *Client) GetSkills(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_skills",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_skills returns type=ok; request_id correlation eliminates the
	// historical payload-shape disambiguation that was needed when the
	// server's response shape was in flux.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetNearby gets other players at the current POI.
func (c *Client) GetNearby(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_nearby",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_nearby returns type=ok with "nearby" key; request_id correlation
	// makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetSystemAgents gets all uncloaked online players in the current system
// (system-wide version of GetNearby).
func (c *Client) GetSystemAgents(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_system_agents",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetBase gets docked base details.
func (c *Client) GetBase(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_base",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_base returns type=ok with "base" (object) and "services" keys;
	// request_id correlation makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetShip gets detailed ship information.
func (c *Client) GetShip(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_ship",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_ship returns type=ok with a "ship" payload key; request_id
	// correlation makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Missions
// ============================================================================

// GetMissions gets available missions at the current base.
func (c *Client) GetMissions(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_missions",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_missions returns type=ok; request_id correlation makes the
	// payload-shape classifier (previously needed to disambiguate from
	// get_active_missions) redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetActionLog fetches recent server-side action log entries. The payload
// accepts server-supported filters (category, event_type, faction_id, page,
// page_size); pass nil for defaults.
func (c *Client) GetActionLog(ctx context.Context, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	msg := protocol.Message{
		Type:      "get_action_log",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// get_action_log returns type=ok with "entries" array; request_id
	// correlation makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetActiveMissions views your active missions and progress.
func (c *Client) GetActiveMissions(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_active_missions",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_active_missions returns type=ok; request_id correlation makes the
	// payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// AcceptMission accepts a mission from the mission board.
func (c *Client) AcceptMission(ctx context.Context, missionID string) error {
	msg := protocol.Message{
		Type:      "accept_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CompleteMission completes a mission and claims rewards.
func (c *Client) CompleteMission(ctx context.Context, missionID string) error {
	msg := protocol.Message{
		Type:      "complete_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// AbandonMission abandons an active mission.
func (c *Client) AbandonMission(ctx context.Context, missionID string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "abandon_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// DeclineMission declines a mission and hears the NPC's response.
func (c *Client) DeclineMission(ctx context.Context, templateID string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "decline_mission",
		Payload:   map[string]any{"template_id": templateID},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ============================================================================
// Insurance & Respawn
// ============================================================================

// BuyInsurance purchases ship insurance for a number of ticks.
func (c *Client) BuyInsurance(ctx context.Context, ticks int) error {
	msg := protocol.Message{
		Type:      "buy_insurance",
		Payload:   map[string]any{"ticks": ticks},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ClaimInsurance views your active insurance policies.
func (c *Client) ClaimInsurance(ctx context.Context) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "claim_insurance",
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// GetInsuranceQuote gets a risk-based insurance quote for the current ship.
// It blocks until the quote response is received so callers (e.g. the play_as
// REPL) can read the stored payload immediately — get_insurance_quote returns
// type=ok with a "quote" key, like get_nearby.
func (c *Client) GetInsuranceQuote(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_insurance_quote",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// SetHomeBase sets the home base for respawning.
func (c *Client) SetHomeBase(ctx context.Context, baseID string) error {
	msg := protocol.Message{
		Type:      "set_home_base",
		Payload:   map[string]any{"base_id": baseID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Notes & Documents
// ============================================================================

// CreateNote creates a new note document.
func (c *Client) CreateNote(ctx context.Context, title, content string) error {
	msg := protocol.Message{
		Type: "create_note",
		Payload: map[string]any{
			"title":   title,
			"content": content,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetNotes lists all your note documents.
func (c *Client) GetNotes(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_notes",
		Timestamp: time.Now().UnixMilli(),
	}
	// Server returns {notes: [...], total_count: N} synchronously; request_id
	// correlation makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ReadNote reads a note document's contents.
func (c *Client) ReadNote(ctx context.Context, noteID string) error {
	msg := protocol.Message{
		Type:      "read_note",
		Payload:   map[string]any{"note_id": noteID},
		Timestamp: time.Now().UnixMilli(),
	}
	// read_note response carries note_id + content synchronously when the
	// server can find the document. If it can't (note_not_found), the
	// server replies with type=error — request_id correlation routes both
	// outcomes to this caller; surface the error cleanly.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	var resp protocol.Response
	if err == nil {
		resp, err = c.await(ctx, h)
	}
	if err != nil {
		return err
	}
	if resp.Type == protocol.TypeError {
		return serverErrorFromPayload(resp.Payload)
	}
	return nil
}

// WriteNote edits an existing note document.
func (c *Client) WriteNote(ctx context.Context, noteID, content string) error {
	msg := protocol.Message{
		Type: "write_note",
		Payload: map[string]any{
			"note_id": noteID,
			"content": content,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Catalog & Help
// ============================================================================

// Catalog browses game reference data (ships, skills, recipes, items).
func (c *Client) Catalog(ctx context.Context, catalogType string, page, pageSize int) error {
	msg := protocol.Message{
		Type: "catalog",
		Payload: map[string]any{
			"type":      catalogType,
			"page":      page,
			"page_size": pageSize,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	// catalog responses have {items, page, page_size, total, total_pages, type};
	// request_id correlation makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetCommands gets a structured list of all commands.
func (c *Client) GetCommands(ctx context.Context) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "get_commands",
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// GetGuide gets a detailed playstyle progression guide.
func (c *Client) GetGuide(ctx context.Context, guide string) error {
	payload := map[string]any{}
	if guide != "" {
		payload["guide"] = guide
	}
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "get_guide",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// SearchChangelog searches release notes and version history.
func (c *Client) SearchChangelog(ctx context.Context, payload map[string]any) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "search_changelog",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// SearchSystems searches for systems by name.
func (c *Client) SearchSystems(ctx context.Context, query string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "search_systems",
		Payload:   map[string]any{"query": query},
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// GetVersion gets game version and release notes.
func (c *Client) GetVersion(ctx context.Context) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "get_version",
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// Help gets help for commands.
func (c *Client) Help(ctx context.Context, payload map[string]any) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "help",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ============================================================================
// Social & Chat
// ============================================================================

// Chat sends a chat message.
func (c *Client) Chat(ctx context.Context, channel, content string, targetID string) error {
	payload := map[string]any{
		"channel": channel,
		"content": content,
	}
	if targetID != "" {
		payload["target_id"] = targetID
	}
	return c.send(ctx, protocol.Message{
		Type:      "chat",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetChatHistory gets chat message history.
func (c *Client) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["channel"] = channel
	msg := protocol.Message{
		Type:      "get_chat_history",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Server response carries {channel, messages[], has_more, total_count};
	// request_id correlation routes concurrent queries on different
	// channels to the correct caller without payload-shape matching.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Forum
// ============================================================================

// ForumList lists forum threads.
func (c *Client) ForumList(ctx context.Context, page int) error {
	payload := map[string]any{}
	if page > 0 {
		payload["page"] = page
	}
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "forum_list",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ForumCreateThread creates a new forum thread.
func (c *Client) ForumCreateThread(ctx context.Context, title, content string, category string) error {
	payload := map[string]any{
		"title":   title,
		"content": content,
	}
	if category != "" {
		payload["category"] = category
	}
	return c.send(ctx, protocol.Message{
		Type:      "forum_create_thread",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// ForumGetThread gets a forum thread and its replies.
func (c *Client) ForumGetThread(ctx context.Context, threadID string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "forum_get_thread",
		Payload:   map[string]any{"thread_id": threadID},
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ForumReply replies to a forum thread.
func (c *Client) ForumReply(ctx context.Context, threadID, content string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type: "forum_reply",
		Payload: map[string]any{
			"thread_id": threadID,
			"content":   content,
		},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ForumDeleteReply deletes a forum reply.
func (c *Client) ForumDeleteReply(ctx context.Context, replyID string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "forum_delete_reply",
		Payload:   map[string]any{"reply_id": replyID},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ForumDeleteThread deletes a forum thread.
func (c *Client) ForumDeleteThread(ctx context.Context, threadID string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "forum_delete_thread",
		Payload:   map[string]any{"thread_id": threadID},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ForumUpvote upvotes a thread or reply.
func (c *Client) ForumUpvote(ctx context.Context, threadID string, replyID string) error {
	payload := map[string]any{"thread_id": threadID}
	if replyID != "" {
		payload["reply_id"] = replyID
	}
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "forum_upvote",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ============================================================================
// Captain's Log
// ============================================================================

// CaptainsLogAdd adds an entry to your captain's log.
func (c *Client) CaptainsLogAdd(ctx context.Context, entry string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "captains_log_add",
		Payload:   map[string]any{"entry": entry},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// CaptainsLogGet gets a specific entry from your captain's log.
func (c *Client) CaptainsLogGet(ctx context.Context, index int) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "captains_log_get",
		Payload:   map[string]any{"index": index},
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// CaptainsLogList lists all entries in your captain's log.
func (c *Client) CaptainsLogList(ctx context.Context) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "captains_log_list",
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ============================================================================
// Player Settings
// ============================================================================

// SetAnonymous sets anonymous mode.
func (c *Client) SetAnonymous(ctx context.Context, anonymous bool) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "set_anonymous",
		Payload:   map[string]any{"anonymous": anonymous},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// SetColors sets your ship colors.
func (c *Client) SetColors(ctx context.Context, primaryColor, secondaryColor string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type: "set_colors",
		Payload: map[string]any{
			"primary_color":   primaryColor,
			"secondary_color": secondaryColor,
		},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// SetPlayerStatus sets your status message and clan tag.
func (c *Client) SetPlayerStatus(ctx context.Context, payload map[string]any) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "set_status",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// ============================================================================
// Station Facilities
// ============================================================================

// facilityTerminate handles facility's dual-shape completion: the standard
// action terminator semantics for action_result/action_error, plus a sync
// query termination when type=ok lands without pending=true.
func facilityTerminate(resp protocol.Response) (bool, error) {
	switch resp.Type {
	case protocol.TypeActionResult:
		return true, nil
	case protocol.TypeActionError, protocol.TypeError:
		return true, serverErrorFromPayload(resp.Payload)
	case protocol.TypeOK:
		if pending, _ := resp.Payload["pending"].(bool); pending {
			return false, nil // intermediate; async mutation is still working
		}
		return true, nil // sync query reply is the terminal
	}
	return false, nil
}

// Facility manages facilities at stations. Both query-shaped sub-actions
// (list, types, upgrades, help, faction_list, etc.) and mutation-shaped
// sub-actions (build, toggle, personal_build, transfer, etc.) flow through
// here. Sync queries return type=ok inline; async mutations follow the
// pending ok → action_result/action_error pattern.
func (c *Client) Facility(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "facility",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg,
		WithTerminator(facilityTerminate),
		WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Exploration
// ============================================================================

// Note: SurveySystem and FindRoute are in client.go.

// ============================================================================
// Faction Management
// ============================================================================

// FactionInfo views faction details. If factionID is empty, views your own faction.
func (c *Client) FactionInfo(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "faction_info",
		Timestamp: time.Now().UnixMilli(),
	}
	// faction_info returns type=ok with "is_member" boolean; request_id
	// correlation makes the payload-shape classifier redundant.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionList lists all factions.
func (c *Client) FactionList(ctx context.Context, limit, offset int) error {
	payload := map[string]any{}
	if limit > 0 {
		payload["limit"] = limit
	}
	if offset > 0 {
		payload["offset"] = offset
	}
	msg := protocol.Message{
		Type:      "faction_list",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Query (x-is-mutation=false): returns type=ok immediately; WithAckOnly
	// treats that reply as terminal for this request_id.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CreateFaction creates a new faction.
func (c *Client) CreateFaction(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "create_faction",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// JoinFaction joins a faction via invitation.
func (c *Client) JoinFaction(ctx context.Context, factionID string) error {
	msg := protocol.Message{
		Type:      "join_faction",
		Payload:   map[string]any{"faction_id": factionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// LeaveFaction leaves your current faction.
func (c *Client) LeaveFaction(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "leave_faction",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionInvite invites a player to your faction.
func (c *Client) FactionInvite(ctx context.Context, playerID string) error {
	msg := protocol.Message{
		Type:      "faction_invite",
		Payload:   map[string]any{"player_id": playerID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionKick kicks a player from your faction.
func (c *Client) FactionKick(ctx context.Context, playerID string) error {
	msg := protocol.Message{
		Type:      "faction_kick",
		Payload:   map[string]any{"player_id": playerID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionPromote promotes or demotes a faction member.
func (c *Client) FactionPromote(ctx context.Context, playerID, roleID string) error {
	msg := protocol.Message{
		Type: "faction_promote",
		Payload: map[string]any{
			"player_id": playerID,
			"role_id":   roleID,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionEdit updates faction description, charter, and colors.
func (c *Client) FactionEdit(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_edit",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Not tick-gated (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionCreateRole creates a custom faction role.
func (c *Client) FactionCreateRole(ctx context.Context, name string, priority int, permissions map[string]any) error {
	payload := map[string]any{
		"name":     name,
		"priority": priority,
	}
	if permissions != nil {
		payload["permissions"] = permissions
	}
	msg := protocol.Message{
		Type:      "faction_create_role",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Not tick-gated (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionEditRole edits a custom faction role.
func (c *Client) FactionEditRole(ctx context.Context, roleID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["role_id"] = roleID
	msg := protocol.Message{
		Type:      "faction_edit_role",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Not tick-gated (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionDeleteRole deletes a custom faction role.
func (c *Client) FactionDeleteRole(ctx context.Context, roleID string) error {
	msg := protocol.Message{
		Type:      "faction_delete_role",
		Payload:   map[string]any{"role_id": roleID},
		Timestamp: time.Now().UnixMilli(),
	}
	// Not tick-gated (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Faction Diplomacy
// ============================================================================

// FactionDeclareWar declares war on another faction.
func (c *Client) FactionDeclareWar(ctx context.Context, targetFactionID, reason string) error {
	payload := map[string]any{"target_faction_id": targetFactionID}
	if reason != "" {
		payload["reason"] = reason
	}
	msg := protocol.Message{
		Type:      "faction_declare_war",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionProposePeace proposes peace to a faction you're at war with.
func (c *Client) FactionProposePeace(ctx context.Context, targetFactionID, terms string) error {
	payload := map[string]any{"target_faction_id": targetFactionID}
	if terms != "" {
		payload["terms"] = terms
	}
	msg := protocol.Message{
		Type:      "faction_propose_peace",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionAcceptPeace accepts a peace proposal.
func (c *Client) FactionAcceptPeace(ctx context.Context, targetFactionID string) error {
	msg := protocol.Message{
		Type:      "faction_accept_peace",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionProposeAlly proposes a mutual alliance with another faction. The
// target's diplomacy-capable members must call FactionAcceptAlly to ratify.
func (c *Client) FactionProposeAlly(ctx context.Context, targetFactionID string) error {
	msg := protocol.Message{
		Type:      "faction_propose_ally",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionAcceptAlly accepts a pending alliance proposal, ratifying it on both sides.
func (c *Client) FactionAcceptAlly(ctx context.Context, targetFactionID string) error {
	msg := protocol.Message{
		Type:      "faction_accept_ally",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionRemoveAlly dissolves an alliance with another faction. Idempotent:
// succeeds even if no alliance existed.
func (c *Client) FactionRemoveAlly(ctx context.Context, targetFactionID string) error {
	msg := protocol.Message{
		Type:      "faction_remove_ally",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionSetEnemy marks another faction as enemy.
func (c *Client) FactionSetEnemy(ctx context.Context, targetFactionID string) error {
	msg := protocol.Message{
		Type:      "faction_set_enemy",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Faction Intel
// ============================================================================

// FactionSubmitIntel submits system intel to your faction's shared map.
func (c *Client) FactionSubmitIntel(ctx context.Context, systems []map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_submit_intel",
		Payload:   map[string]any{"systems": systems},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// Espionage sends a spy to gather intelligence on the station the player is
// currently docked at, using the faction's Espionage HQ facility. Requires
// faction membership, an active Espionage HQ built anywhere by the faction,
// and being docked at the target station.
//
// The operation blocks the player for ~90s server-side and no other action can
// be taken until it resolves, so it awaits on SleepEspionageMaxWait rather than
// the ordinary mutation timeout.
func (c *Client) Espionage(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "espionage",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepEspionageMaxWait))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionQueryIntel queries your faction's intel database.
func (c *Client) FactionQueryIntel(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_query_intel",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Query (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionIntelStatus views faction intel coverage statistics.
func (c *Client) FactionIntelStatus(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "faction_intel_status",
		Timestamp: time.Now().UnixMilli(),
	}
	// Query (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionSubmitTradeIntel submits market price observations to your faction's trade ledger.
func (c *Client) FactionSubmitTradeIntel(ctx context.Context, stations []map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_submit_trade_intel",
		Payload:   map[string]any{"stations": stations},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionQueryTradeIntel searches your faction's market price database.
func (c *Client) FactionQueryTradeIntel(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_query_trade_intel",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Query (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionTradeIntelStatus views faction trade intelligence coverage statistics.
func (c *Client) FactionTradeIntelStatus(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "faction_trade_intel_status",
		Timestamp: time.Now().UnixMilli(),
	}
	// Query (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Faction Storage & Economy
// ============================================================================

// FactionDepositItems moves items from your cargo to faction storage.
func (c *Client) FactionDepositItems(ctx context.Context, itemID string, quantity int) error {
	msg := protocol.Message{
		Type: "faction_deposit_items",
		Payload: map[string]any{
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionWithdrawItems moves items from faction storage to your cargo.
func (c *Client) FactionWithdrawItems(ctx context.Context, itemID string, quantity int) error {
	msg := protocol.Message{
		Type: "faction_withdraw_items",
		Payload: map[string]any{
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionDepositItemsPayload sends a faction_deposit_items command with the
// caller-supplied payload. Allows v2 source/target selectors (see
// WithdrawItemsPayload for semantics).
func (c *Client) FactionDepositItemsPayload(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_deposit_items",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionWithdrawItemsPayload sends a faction_withdraw_items command with
// the caller-supplied payload. Allows v2 source/target selectors.
func (c *Client) FactionWithdrawItemsPayload(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_withdraw_items",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionDepositCredits transfers credits from your wallet to faction storage.
func (c *Client) FactionDepositCredits(ctx context.Context, amount float64) error {
	msg := protocol.Message{
		Type:      "faction_deposit_credits",
		Payload:   map[string]any{"amount": amount},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionWithdrawCredits transfers credits from faction storage to your wallet.
func (c *Client) FactionWithdrawCredits(ctx context.Context, amount float64) error {
	msg := protocol.Message{
		Type:      "faction_withdraw_credits",
		Payload:   map[string]any{"amount": amount},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionGift gifts items or credits to a faction's storage.
func (c *Client) FactionGift(ctx context.Context, factionID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["faction_id"] = factionID
	msg := protocol.Message{
		Type:      "faction_gift",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ViewFactionStorage views your faction's shared storage at the current station.
func (c *Client) ViewFactionStorage(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_faction_storage",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ViewFactionStorageAt views your faction's shared storage at a specific station.
// As of v0.299.0, you can query remotely with station_id as long as you're a faction member.
func (c *Client) ViewFactionStorageAt(ctx context.Context, stationID string) error {
	msg := protocol.Message{
		Type:      "view_faction_storage",
		Timestamp: time.Now().UnixMilli(),
		Payload:   map[string]any{"station_id": stationID},
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionCreateBuyOrder creates a buy order on behalf of your faction.
func (c *Client) FactionCreateBuyOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	msg := protocol.Message{
		Type: "faction_create_buy_order",
		Payload: map[string]any{
			"item_id":    itemID,
			"price_each": priceEach,
			"quantity":   quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionCreateSellOrder creates a sell order on behalf of your faction.
func (c *Client) FactionCreateSellOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	msg := protocol.Message{
		Type: "faction_create_sell_order",
		Payload: map[string]any{
			"item_id":    itemID,
			"price_each": priceEach,
			"quantity":   quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Faction Missions
// ============================================================================

// FactionListMissions lists your faction's posted missions at this station.
func (c *Client) FactionListMissions(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "faction_list_missions",
		Timestamp: time.Now().UnixMilli(),
	}
	// Query (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionPostMission posts a mission on your faction's mission board.
func (c *Client) FactionPostMission(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_post_mission",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionCancelMission cancels a posted faction mission and refunds escrowed rewards.
func (c *Client) FactionCancelMission(ctx context.Context, templateID string) error {
	msg := protocol.Message{
		Type:      "faction_cancel_mission",
		Payload:   map[string]any{"template_id": templateID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Faction Invites
// ============================================================================

// FactionGetInvites views pending faction invitations.
func (c *Client) FactionGetInvites(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "faction_get_invites",
		Timestamp: time.Now().UnixMilli(),
	}
	// Query: returns type=ok with an "invites" array. request_id correlation
	// lets callers reliably wait for and match this reply (WithAckOnly = no
	// action_result/tick to wait on).
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionDeclineInvite declines a faction invitation.
func (c *Client) FactionDeclineInvite(ctx context.Context, factionID string) error {
	msg := protocol.Message{
		Type:      "faction_decline_invite",
		Payload:   map[string]any{"faction_id": factionID},
		Timestamp: time.Now().UnixMilli(),
	}
	// Not tick-gated (x-is-mutation=false): server returns type=ok immediately,
	// so WithAckOnly treats that reply as terminal for this request_id.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Faction Rooms (Common Space)
// ============================================================================

// FactionRooms lists rooms in your faction's common space at the current station.
func (c *Client) FactionRooms(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "faction_rooms",
		Timestamp: time.Now().UnixMilli(),
	}
	// Query (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionVisitRoom visits a room and reads its description.
func (c *Client) FactionVisitRoom(ctx context.Context, roomID string) error {
	msg := protocol.Message{
		Type:      "faction_visit_room",
		Payload:   map[string]any{"room_id": roomID},
		Timestamp: time.Now().UnixMilli(),
	}
	// Query (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionWriteRoom creates or updates a room in your faction's common space.
func (c *Client) FactionWriteRoom(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "faction_write_room",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Not tick-gated (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionDeleteRoom deletes a room from your faction's common space.
func (c *Client) FactionDeleteRoom(ctx context.Context, roomID string) error {
	msg := protocol.Message{
		Type:      "faction_delete_room",
		Payload:   map[string]any{"room_id": roomID},
		Timestamp: time.Now().UnixMilli(),
	}
	// Not tick-gated (x-is-mutation=false): returns type=ok immediately.
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// RawCommand sends an arbitrary command to the server and blocks until its
// terminal response. terminateOnActionOrOK resolves immediately on a
// synchronous (non-pending) ok — the shape queries return — but waits through
// a pending:true ack for the real action_result terminal that deferred
// mutations deliver on the next tick. Blocking ensures the terminal payload
// has been received and cached by storeRawJSON before the caller reads it
// back: interactive callers like play_as look the response up by command name
// immediately after this returns, so a fire-and-forget send (or an ack-only
// wait, for a deferred mutation) would race and show nothing or the bare
// "pending" frame instead of the real result.
func (c *Client) RawCommand(ctx context.Context, command string, args map[string]any) error {
	msg := protocol.Message{
		Type:      command,
		Payload:   args,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}

// GetDrone fetches details for a single drone.
func (c *Client) GetDrone(ctx context.Context, droneID string) error {
	msg := protocol.Message{
		Type:      "get_drone",
		Payload:   map[string]any{"drone_id": droneID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetDrones fetches the drone bay summary and roster.
func (c *Client) GetDrones(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_drones",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// LoadDrone loads an item from cargo into the drone bay as a drone.
func (c *Client) LoadDrone(ctx context.Context, itemID string) error {
	msg := protocol.Message{
		Type:      "load_drone",
		Payload:   map[string]any{"item_id": itemID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// UnloadDrone unloads a drone from the bay back into cargo.
func (c *Client) UnloadDrone(ctx context.Context, droneID string) error {
	msg := protocol.Message{
		Type:      "unload_drone",
		Payload:   map[string]any{"drone_id": droneID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// RecallDrone recalls a deployed drone (or all drones when all is true).
func (c *Client) RecallDrone(ctx context.Context, droneID string, all bool) error {
	payload := map[string]any{"all": all}
	if droneID != "" {
		payload["drone_id"] = droneID
	}
	msg := protocol.Message{
		Type:      "recall_drone",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// UploadDroneScript uploads an automation script to a deployed drone.
func (c *Client) UploadDroneScript(ctx context.Context, droneID, script string) error {
	msg := protocol.Message{
		Type:      "upload_drone_script",
		Payload:   map[string]any{"drone_id": droneID, "script": script},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// DeployDrone launches a drone from the bay into the current location, or
// every in-bay drone when all is true (server-side bandwidth check still
// applies — drones that would exceed remaining bandwidth are silently
// skipped). Mirrors the RecallDrone signature; pass droneID="" with all=true
// for the bulk path.
func (c *Client) DeployDrone(ctx context.Context, droneID string, all bool) error {
	payload := map[string]any{"all": all}
	if droneID != "" {
		payload["drone_id"] = droneID
	}
	msg := protocol.Message{
		Type:      "deploy_drone",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// SetDroneName assigns a display name to a drone you own (max 32 chars;
// same character rules as ship names). Pass an empty name to clear.
// Not a mutation — no tick cost.
func (c *Client) SetDroneName(ctx context.Context, droneID, name string) error {
	msg := protocol.Message{
		Type:      "set_drone_name",
		Payload:   map[string]any{"drone_id": droneID, "name": name},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionAcceptInvite accepts a pending invitation to join a faction.
func (c *Client) FactionAcceptInvite(ctx context.Context, factionID string) error {
	msg := protocol.Message{
		Type:      "faction_accept_invite",
		Payload:   map[string]any{"faction_id": factionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionWithdrawInvite withdraws an invitation previously sent to a player.
func (c *Client) FactionWithdrawInvite(ctx context.Context, playerID string) error {
	msg := protocol.Message{
		Type:      "faction_withdraw_invite",
		Payload:   map[string]any{"player_id": playerID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// FactionRemoveEnemy removes a faction from this faction's enemy list.
func (c *Client) FactionRemoveEnemy(ctx context.Context, targetFactionID string) error {
	msg := protocol.Message{
		Type:      "faction_remove_enemy",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// Citizenship performs a citizenship sub-action: list, apply, renounce, or
// withdraw. empireID is required for all but list, which ignores it.
//
// The terminator differs by action. The command carries x-is-mutation because
// three of its four actions are mutations that execute on the next tick, but
// "list" is a plain query the server ack-terminates: waiting on it for an
// action frame that never arrives hangs the caller for the whole timeout and
// leaves the "citizenship" action lock held, blocking every later call.
func (c *Client) Citizenship(ctx context.Context, action, empireID string) error {
	payload := map[string]any{"action": action}
	if empireID != "" {
		payload["empire_id"] = empireID
	}
	msg := protocol.Message{
		Type:      "citizenship",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	opts := []SubmitOption{WithTimeout(SleepTick * 3)}
	if action == "list" || action == "" {
		opts = append(opts, WithAckOnly())
	}
	h, err := c.Submit(ctx, msg, opts...)
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// PayBounty clears every uncleared crime with one empire — including unpaid
// income and property tax — and restores the reputation those crimes cost, up
// to that empire's cap.
//
// It works anywhere: docked, in open space, or mid-jump. If the empire already
// has the pilot detained, paying releases them immediately. Payment is
// all-or-nothing per empire.
//
// empire is optional; omit it (pass "") when the agent owes exactly one empire
// and the server will infer the target. Naming an empire is required when
// several are owed — read standings[].outstanding_bounty from get_status.
//
// source is "self" (the wallet, and the server's default) or "faction" (the
// treasury, which needs ManageTreasury). Pass "" to take the default.
//
// This is the escape from the 0-credit spiral: a bountied agent cannot buy
// fuel, and before v0.564.0 gifted credits were seized on entering the empire's
// territory. Gifts now land even while detained, so gift -> PayBounty -> refuel
// recovers an agent wherever it is stranded.
func (c *Client) PayBounty(ctx context.Context, empire, source string) error {
	payload := map[string]any{}
	if empire != "" {
		payload["empire"] = empire
	}
	if source != "" {
		payload["source"] = source
	}
	msg := protocol.Message{
		Type:      "pay_bounty",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetEmpireInfo fetches empire information. empireID is optional; when empty
// the server returns all empires.
func (c *Client) GetEmpireInfo(ctx context.Context, empireID string) error {
	payload := map[string]any{}
	if empireID != "" {
		payload["empire_id"] = empireID
	}
	msg := protocol.Message{
		Type:      "get_empire_info",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// Petition submits a citizenship petition message to an empire. The server
// ack-terminates this action (not flagged x-is-mutation), so it uses the
// query terminator.
func (c *Client) Petition(ctx context.Context, empireID, message string) error {
	msg := protocol.Message{
		Type:      "petition",
		Payload:   map[string]any{"empire_id": empireID, "message": message},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// GetTaxEstimate fetches the player's current tax assessment estimate.
func (c *Client) GetTaxEstimate(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_tax_estimate",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ViewInsurance lists the player's active insurance policies.
func (c *Client) ViewInsurance(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_insurance",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ScrapShip scraps a ship, moving its cargo and modules to storage.
func (c *Client) ScrapShip(ctx context.Context, shipID string) error {
	msg := protocol.Message{
		Type:      "scrap_ship",
		Payload:   map[string]any{"ship_id": shipID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CompletedMissions lists the player's completed missions.
func (c *Client) CompletedMissions(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "completed_missions",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// DeleteNote deletes a saved note by ID. The server ack-terminates this
// action (not flagged x-is-mutation), so it uses the query terminator.
func (c *Client) DeleteNote(ctx context.Context, noteID string) error {
	msg := protocol.Message{
		Type:      "delete_note",
		Payload:   map[string]any{"note_id": noteID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// CaptainsLogDelete deletes a captain's-log entry by index. The server
// ack-terminates this action (not flagged x-is-mutation), so it uses the
// query terminator.
func (c *Client) CaptainsLogDelete(ctx context.Context, index int) error {
	msg := protocol.Message{
		Type:      "captains_log_delete",
		Payload:   map[string]any{"index": index},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// AgentLogs submits an agent telemetry log entry. data is optional structured
// context. The action is write-only (no response body) and ack-terminated.
func (c *Client) AgentLogs(ctx context.Context, category, severity, message string, data map[string]any) error {
	payload := map[string]any{
		"category": category,
		"severity": severity,
		"message":  message,
	}
	if data != nil {
		payload["data"] = data
	}
	msg := protocol.Message{
		Type:      "agentlogs",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ============================================================================
// Shipping (freight contracts)
// ============================================================================

// shippingMutations are the tick-deferred /shipping actions. Their real reply
// arrives later in an action_result frame, so they must await that frame rather
// than the immediate pending ack (see storeRawJSON's TypeActionResult case).
// The reads (list, get, profile, track) reply synchronously and keep the ack path.
var shippingMutations = map[string]bool{
	"accept": true, "deliver": true, "return": true,
	"cancel": true, "post": true, "pay_debt": true,
}

// Shipping sends a /shipping action with the given payload (the action is
// injected). The reply is cached under "shipping_<action>" (storeRawJSON);
// read it with GetRawJSON and unmarshal into the matching serverapi struct.
func (c *Client) Shipping(ctx context.Context, action string, payload map[string]any) error {
	// Build a fresh outbound map so we never mutate the caller's payload
	// (injecting "action" into a map the caller reuses would be a footgun).
	out := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		out[k] = v
	}
	out["action"] = action
	// Drop any body this session already cached for this action BEFORE issuing,
	// so a call whose own reply never lands cannot be read as success against
	// the PREVIOUS call's body. latestRawJSON is session-lifetime and has no
	// per-command invalidation, and terminateOnActionOrOK returns on a
	// non-pending ok — i.e. possibly before an action_result. A stale body is
	// worse than none: it decodes into a real contract with a real ID, so the
	// callers' `ID == ""` guards cannot fire and they act on the wrong
	// contract. Clearing makes that failure mode empty-not-stale, which those
	// guards already handle by returning rather than transiting blind.
	c.ClearRawJSON("shipping_" + action)
	msg := protocol.Message{
		Type:      "shipping",
		Payload:   out,
		Timestamp: time.Now().UnixMilli(),
	}
	opts := []SubmitOption{WithTimeout(SleepMedium)}
	if shippingMutations[action] {
		opts = append(opts, WithTerminator(terminateOnActionOrOK))
	} else {
		opts = append(opts, WithAckOnly())
	}
	h, err := c.Submit(ctx, msg, opts...)
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ShippingList fetches the current station's freight board (docked-only). sort ∈
// {reward,distance,age} (empty = server default reward). Reply: shipping_list.
func (c *Client) ShippingList(ctx context.Context, sort string) error {
	p := map[string]any{}
	if sort != "" {
		p["sort"] = sort
	}
	return c.Shipping(ctx, "list", p)
}

// ShippingGet fetches one contract by id. Reply: shipping_get.
func (c *Client) ShippingGet(ctx context.Context, shipmentID string) error {
	return c.Shipping(ctx, "get", map[string]any{"shipment_id": shipmentID})
}

// ShippingAccept accepts a contract as the given carrier (player|faction). The
// package lands in the carrier's storage at origin. Reply: shipping_accept.
func (c *Client) ShippingAccept(ctx context.Context, shipmentID, carrier string) error {
	return c.Shipping(ctx, "accept", map[string]any{"shipment_id": shipmentID, "carrier": carrier})
}

// ShippingDeliver settles a delivered contract. Reply: shipping_deliver.
func (c *Client) ShippingDeliver(ctx context.Context, shipmentID string) error {
	return c.Shipping(ctx, "deliver", map[string]any{"shipment_id": shipmentID})
}

// ShippingReturn returns a contract to its origin (breach-avoidance escape
// hatch). Reply: shipping_return.
func (c *Client) ShippingReturn(ctx context.Context, shipmentID string) error {
	return c.Shipping(ctx, "return", map[string]any{"shipment_id": shipmentID})
}

// ShippingCancel cancels a contract (breach-avoidance escape hatch). Reply:
// shipping_cancel.
func (c *Client) ShippingCancel(ctx context.Context, shipmentID string) error {
	return c.Shipping(ctx, "cancel", map[string]any{"shipment_id": shipmentID})
}

// ShippingTrack fetches a contract plus its beacon custody events (limit ≤ 200;
// 0 = server default). Reply: shipping_track.
func (c *Client) ShippingTrack(ctx context.Context, shipmentID string, limit int) error {
	p := map[string]any{"shipment_id": shipmentID}
	if limit > 0 {
		p["limit"] = limit
	}
	return c.Shipping(ctx, "track", p)
}

// ShippingProfile fetches this actor's carrier standing, capacity, progression
// and debts. Reply: shipping_profile.
func (c *Client) ShippingProfile(ctx context.Context) error {
	return c.Shipping(ctx, "profile", nil)
}

// ShippingPayDebt pays freight debt (amount ≤ 0 pays the full balance). Reply:
// shipping_pay_debt.
func (c *Client) ShippingPayDebt(ctx context.Context, amount int64) error {
	p := map[string]any{}
	if amount > 0 {
		p["amount"] = amount
	}
	return c.Shipping(ctx, "pay_debt", p)
}
