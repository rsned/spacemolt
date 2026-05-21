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
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// ClaimCommission claims a completed ship from a commission.
func (c *Client) ClaimCommission(ctx context.Context, commissionID string) error {
	msg := protocol.Message{
		Type:      "claim_commission",
		Payload:   map[string]any{"commission_id": commissionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// CommissionQuote gets a cost estimate for commissioning a ship.
func (c *Client) CommissionQuote(ctx context.Context, shipClass string) error {
	return c.send(ctx, protocol.Message{
		Type:      "commission_quote",
		Payload:   map[string]any{"ship_class": shipClass},
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
	}
	return err
}

// CommissionStatus checks the status of ship commissions.
func (c *Client) CommissionStatus(ctx context.Context, baseID string) error {
	payload := map[string]any{}
	if baseID != "" {
		payload["base_id"] = baseID
	}
	return c.send(ctx, protocol.Message{
		Type:      "commission_status",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// SellShip sells a stored ship at the current station.
func (c *Client) SellShip(ctx context.Context, shipID string) error {
	msg := protocol.Message{
		Type:      "sell_ship",
		Payload:   map[string]any{"ship_id": shipID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// ============================================================================
// Trading & Market
// ============================================================================

// AnalyzeMarket gets actionable trading insights at the current station.
func (c *Client) AnalyzeMarket(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "analyze_market",
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// EstimatePurchase previews what buying would cost without executing.
func (c *Client) EstimatePurchase(ctx context.Context, itemID string, quantity int) error {
	return c.send(ctx, protocol.Message{
		Type: "estimate_purchase",
		Payload: map[string]any{
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetTrades views pending trade offers.
func (c *Client) GetTrades(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "get_trades",
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// TradeCancel cancels your trade offer.
func (c *Client) TradeCancel(ctx context.Context, tradeID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "trade_cancel",
		Payload:   map[string]any{"trade_id": tradeID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// TradeDecline declines a trade offer.
func (c *Client) TradeDecline(ctx context.Context, tradeID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "trade_decline",
		Payload:   map[string]any{"trade_id": tradeID},
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
	}
	return err
}

// LootWreck loots items from a wreck.
func (c *Client) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error {
	msg := protocol.Message{
		Type: "loot_wreck",
		Payload: map[string]any{
			"wreck_id": wreckID,
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// GetActionLog fetches recent server-side action log entries. The payload
// accepts server-supported filters (category, faction_id, page, page_size);
// pass nil for defaults.
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// AbandonMission abandons an active mission.
func (c *Client) AbandonMission(ctx context.Context, missionID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "abandon_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// DeclineMission declines a mission and hears the NPC's response.
func (c *Client) DeclineMission(ctx context.Context, templateID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "decline_mission",
		Payload:   map[string]any{"template_id": templateID},
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
	}
	return err
}

// ClaimInsurance views your active insurance policies.
func (c *Client) ClaimInsurance(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "claim_insurance",
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetInsuranceQuote gets a risk-based insurance quote for the current ship.
func (c *Client) GetInsuranceQuote(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "get_insurance_quote",
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		resp, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// GetCommands gets a structured list of all commands.
func (c *Client) GetCommands(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "get_commands",
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetGuide gets a detailed playstyle progression guide.
func (c *Client) GetGuide(ctx context.Context, guide string) error {
	payload := map[string]any{}
	if guide != "" {
		payload["guide"] = guide
	}
	return c.send(ctx, protocol.Message{
		Type:      "get_guide",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// SearchChangelog searches release notes and version history.
func (c *Client) SearchChangelog(ctx context.Context, payload map[string]any) error {
	return c.send(ctx, protocol.Message{
		Type:      "search_changelog",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// SearchSystems searches for systems by name.
func (c *Client) SearchSystems(ctx context.Context, query string) error {
	return c.send(ctx, protocol.Message{
		Type:      "search_systems",
		Payload:   map[string]any{"query": query},
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetVersion gets game version and release notes.
func (c *Client) GetVersion(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "get_version",
		Timestamp: time.Now().UnixMilli(),
	})
}

// Help gets help for commands.
func (c *Client) Help(ctx context.Context, payload map[string]any) error {
	return c.send(ctx, protocol.Message{
		Type:      "help",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
	return c.send(ctx, protocol.Message{
		Type:      "forum_list",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
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
	return c.send(ctx, protocol.Message{
		Type:      "forum_get_thread",
		Payload:   map[string]any{"thread_id": threadID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// ForumReply replies to a forum thread.
func (c *Client) ForumReply(ctx context.Context, threadID, content string) error {
	return c.send(ctx, protocol.Message{
		Type: "forum_reply",
		Payload: map[string]any{
			"thread_id": threadID,
			"content":   content,
		},
		Timestamp: time.Now().UnixMilli(),
	})
}

// ForumDeleteReply deletes a forum reply.
func (c *Client) ForumDeleteReply(ctx context.Context, replyID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "forum_delete_reply",
		Payload:   map[string]any{"reply_id": replyID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// ForumDeleteThread deletes a forum thread.
func (c *Client) ForumDeleteThread(ctx context.Context, threadID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "forum_delete_thread",
		Payload:   map[string]any{"thread_id": threadID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// ForumUpvote upvotes a thread or reply.
func (c *Client) ForumUpvote(ctx context.Context, threadID string, replyID string) error {
	payload := map[string]any{"thread_id": threadID}
	if replyID != "" {
		payload["reply_id"] = replyID
	}
	return c.send(ctx, protocol.Message{
		Type:      "forum_upvote",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// ============================================================================
// Captain's Log
// ============================================================================

// CaptainsLogAdd adds an entry to your captain's log.
func (c *Client) CaptainsLogAdd(ctx context.Context, entry string) error {
	return c.send(ctx, protocol.Message{
		Type:      "captains_log_add",
		Payload:   map[string]any{"entry": entry},
		Timestamp: time.Now().UnixMilli(),
	})
}

// CaptainsLogGet gets a specific entry from your captain's log.
func (c *Client) CaptainsLogGet(ctx context.Context, index int) error {
	return c.send(ctx, protocol.Message{
		Type:      "captains_log_get",
		Payload:   map[string]any{"index": index},
		Timestamp: time.Now().UnixMilli(),
	})
}

// CaptainsLogList lists all entries in your captain's log.
func (c *Client) CaptainsLogList(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "captains_log_list",
		Timestamp: time.Now().UnixMilli(),
	})
}

// ============================================================================
// Player Settings
// ============================================================================

// SetAnonymous sets anonymous mode.
func (c *Client) SetAnonymous(ctx context.Context, anonymous bool) error {
	return c.send(ctx, protocol.Message{
		Type:      "set_anonymous",
		Payload:   map[string]any{"anonymous": anonymous},
		Timestamp: time.Now().UnixMilli(),
	})
}

// SetColors sets your ship colors.
func (c *Client) SetColors(ctx context.Context, primaryColor, secondaryColor string) error {
	return c.send(ctx, protocol.Message{
		Type: "set_colors",
		Payload: map[string]any{
			"primary_color":   primaryColor,
			"secondary_color": secondaryColor,
		},
		Timestamp: time.Now().UnixMilli(),
	})
}

// SetPlayerStatus sets your status message and clan tag.
func (c *Client) SetPlayerStatus(ctx context.Context, payload map[string]any) error {
	return c.send(ctx, protocol.Message{
		Type:      "set_status",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
	return c.send(ctx, protocol.Message{
		Type:      "faction_list",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// FactionEdit updates faction description, charter, and colors.
func (c *Client) FactionEdit(ctx context.Context, payload map[string]any) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_edit",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
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
	return c.send(ctx, protocol.Message{
		Type:      "faction_create_role",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionEditRole edits a custom faction role.
func (c *Client) FactionEditRole(ctx context.Context, roleID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["role_id"] = roleID
	return c.send(ctx, protocol.Message{
		Type:      "faction_edit_role",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionDeleteRole deletes a custom faction role.
func (c *Client) FactionDeleteRole(ctx context.Context, roleID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_delete_role",
		Payload:   map[string]any{"role_id": roleID},
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// FactionQueryIntel queries your faction's intel database.
func (c *Client) FactionQueryIntel(ctx context.Context, payload map[string]any) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_query_intel",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionIntelStatus views faction intel coverage statistics.
func (c *Client) FactionIntelStatus(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_intel_status",
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
	}
	return err
}

// FactionQueryTradeIntel searches your faction's market price database.
func (c *Client) FactionQueryTradeIntel(ctx context.Context, payload map[string]any) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_query_trade_intel",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionTradeIntelStatus views faction trade intelligence coverage statistics.
func (c *Client) FactionTradeIntelStatus(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_trade_intel_status",
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// ViewFactionStorage views your faction's shared storage at the current station.
func (c *Client) ViewFactionStorage(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_faction_storage",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick))
	if err == nil {
		_, err = h.Result(ctx)
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
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick))
	if err == nil {
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// ============================================================================
// Faction Missions
// ============================================================================

// FactionListMissions lists your faction's posted missions at this station.
func (c *Client) FactionListMissions(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_list_missions",
		Timestamp: time.Now().UnixMilli(),
	})
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
		_, err = h.Result(ctx)
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
		_, err = h.Result(ctx)
	}
	return err
}

// ============================================================================
// Faction Invites
// ============================================================================

// FactionGetInvites views pending faction invitations.
func (c *Client) FactionGetInvites(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_get_invites",
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionDeclineInvite declines a faction invitation.
func (c *Client) FactionDeclineInvite(ctx context.Context, factionID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_decline_invite",
		Payload:   map[string]any{"faction_id": factionID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// ============================================================================
// Faction Rooms (Common Space)
// ============================================================================

// FactionRooms lists rooms in your faction's common space at the current station.
func (c *Client) FactionRooms(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_rooms",
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionVisitRoom visits a room and reads its description.
func (c *Client) FactionVisitRoom(ctx context.Context, roomID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_visit_room",
		Payload:   map[string]any{"room_id": roomID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionWriteRoom creates or updates a room in your faction's common space.
func (c *Client) FactionWriteRoom(ctx context.Context, payload map[string]any) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_write_room",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionDeleteRoom deletes a room from your faction's common space.
func (c *Client) FactionDeleteRoom(ctx context.Context, roomID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "faction_delete_room",
		Payload:   map[string]any{"room_id": roomID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// RawCommand sends an arbitrary command to the server.
func (c *Client) RawCommand(ctx context.Context, command string, args map[string]any) error {
	return c.send(ctx, protocol.Message{
		Type:      command,
		Payload:   args,
		Timestamp: time.Now().UnixMilli(),
	})
}
