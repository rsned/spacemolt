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
	if err := c.Send(ctx, protocol.Message{
		Type:      "battle",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// GetBattleStatus queries the current battle state (free query, no tick cost).
// Blocks until the server responds.
func (c *Client) GetBattleStatus(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "get_battle_status",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Reload reloads a weapon's magazine from ammo in cargo.
func (c *Client) Reload(ctx context.Context, weaponInstanceID, ammoItemID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "reload",
		Payload: map[string]any{
			"weapon_instance_id": weaponInstanceID,
			"ammo_item_id":      ammoItemID,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// SelfDestruct destroys your own ship.
func (c *Client) SelfDestruct(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "self_destruct",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// Cloak toggles the cloaking device. Pass true to enable, false to disable.
func (c *Client) Cloak(ctx context.Context, enable bool) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "cloak",
		Payload:   map[string]any{"enable": enable},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ScanTarget scans a specific target player.
func (c *Client) ScanTarget(ctx context.Context, targetID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "scan",
		Payload:   map[string]any{"target_id": targetID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
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
	// browse_ships returns type=ok with a "listings" array; storeRawJSON
	// detects this via content-based "listings" key (no action-field path).
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("listings"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// BuyListedShip purchases a ship listed by another player.
func (c *Client) BuyListedShip(ctx context.Context, listingID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "buy_listed_ship",
		Payload:   map[string]any{"listing_id": listingID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// BuyShip buys a pre-built ship from the station showroom.
func (c *Client) BuyShip(ctx context.Context, shipClass string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "buy_ship",
		Payload:   map[string]any{"ship_class": shipClass},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// CancelCommission cancels a pending or in-progress ship commission.
func (c *Client) CancelCommission(ctx context.Context, commissionID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "cancel_commission",
		Payload:   map[string]any{"commission_id": commissionID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// CancelShipListing removes a ship listing from the exchange.
func (c *Client) CancelShipListing(ctx context.Context, listingID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "cancel_ship_listing",
		Payload:   map[string]any{"listing_id": listingID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ClaimCommission claims a completed ship from a commission.
func (c *Client) ClaimCommission(ctx context.Context, commissionID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "claim_commission",
		Payload:   map[string]any{"commission_id": commissionID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// CommissionQuote gets a cost estimate for commissioning a ship.
func (c *Client) CommissionQuote(ctx context.Context, shipClass string) error {
	return c.Send(ctx, protocol.Message{
		Type:      "commission_quote",
		Payload:   map[string]any{"ship_class": shipClass},
		Timestamp: time.Now().UnixMilli(),
	})
}

// CommissionShip commissions a ship to be built at the current shipyard.
func (c *Client) CommissionShip(ctx context.Context, shipClass string, provideMaterials bool) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "commission_ship",
		Payload: map[string]any{
			"ship_class":        shipClass,
			"provide_materials": provideMaterials,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// CommissionStatus checks the status of ship commissions.
func (c *Client) CommissionStatus(ctx context.Context, baseID string) error {
	payload := map[string]any{}
	if baseID != "" {
		payload["base_id"] = baseID
	}
	return c.Send(ctx, protocol.Message{
		Type:      "commission_status",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// ListShipForSale lists a stored ship for sale on the exchange.
func (c *Client) ListShipForSale(ctx context.Context, shipID string, price float64) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "list_ship_for_sale",
		Payload: map[string]any{
			"ship_id": shipID,
			"price":   price,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}


// SwitchShip switches to a different ship stored at the current station.
func (c *Client) SwitchShip(ctx context.Context, shipID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "switch_ship",
		Payload:   map[string]any{"ship_id": shipID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// SellShip sells a stored ship at the current station.
func (c *Client) SellShip(ctx context.Context, shipID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "sell_ship",
		Payload:   map[string]any{"ship_id": shipID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ListShips lists all ships owned by the player and their locations.
func (c *Client) ListShips(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "list_ships",
		Timestamp: time.Now().UnixMilli(),
	}
	// list_ships returns type=ok with no "action" field on the wire (despite
	// storeRawJSON having a case for it — that path is dead for the current
	// server). The distinctive payload key is "active_ship_id" which uniquely
	// identifies the response shape. "ships" alone would collide with
	// view_storage which also carries that key.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("active_ship_id"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// UseItem uses a consumable item from cargo.
func (c *Client) UseItem(ctx context.Context, itemID string, quantity int) error {
	payload := map[string]any{"item_id": itemID}
	if quantity > 0 {
		payload["quantity"] = quantity
	}
	if err := c.Send(ctx, protocol.Message{
		Type:      "use_item",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// InstallMod installs a module on the ship.
func (c *Client) InstallMod(ctx context.Context, moduleID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "install_mod",
		Payload:   map[string]any{"module_id": moduleID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// RefitShip refits the active ship to its latest class specifications.
func (c *Client) RefitShip(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "refit_ship",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// UninstallMod uninstalls a module from the ship.
func (c *Client) UninstallMod(ctx context.Context, moduleID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "uninstall_mod",
		Payload:   map[string]any{"module_id": moduleID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ============================================================================
// Trading & Market
// ============================================================================

// AnalyzeMarket gets actionable trading insights at the current station.
func (c *Client) AnalyzeMarket(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
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
	_, err := c.execMutation(ctx, msg, matchCommand("create_buy_order"), terminateOnAction, SleepTick*3)
	return err
}

// CreateSellOrder lists items for sale on the station exchange.
func (c *Client) CreateSellOrder(ctx context.Context, payload map[string]any) error {
	msg := protocol.Message{
		Type:      "create_sell_order",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	_, err := c.execMutation(ctx, msg, matchCommand("create_sell_order"), terminateOnAction, SleepTick*3)
	return err
}

// CancelOrder cancels an active exchange order and returns escrow.
func (c *Client) CancelOrder(ctx context.Context, payload map[string]any) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "cancel_order",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ModifyOrder changes the price on an existing exchange order.
func (c *Client) ModifyOrder(ctx context.Context, payload map[string]any) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "modify_order",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ViewMarket views the order book at the current station.
func (c *Client) ViewMarket(ctx context.Context, itemID string) error {
	payload := map[string]any{}
	if itemID != "" {
		payload["item_id"] = itemID
	}
	msg := protocol.Message{
		Type:      "view_market",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// view_market returns type=ok with action="view_market"; storeRawJSON
	// stores under "market". Use matchAction for precise correlation.
	match := matchAll(matchType(protocol.TypeOK), matchAction("view_market"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// ViewOrders views your own orders at the current station.
func (c *Client) ViewOrders(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_orders",
		Timestamp: time.Now().UnixMilli(),
	}
	// view_orders returns type=ok with required "orders" array and "action" field.
	// openapi.json ViewOrdersResponse confirms both are always present. The response
	// also has a "base" field, but "orders" is more distinctive (no collision).
	// storeRawJSON action-switch overrides "base" with "orders" for this command.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("orders"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// EstimatePurchase previews what buying would cost without executing.
func (c *Client) EstimatePurchase(ctx context.Context, itemID string, quantity int) error {
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
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
	if err := c.Send(ctx, protocol.Message{
		Type:      "trade_offer",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// TradeAccept accepts a trade offer.
func (c *Client) TradeAccept(ctx context.Context, tradeID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "trade_accept",
		Payload:   map[string]any{"trade_id": tradeID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// TradeCancel cancels your trade offer.
func (c *Client) TradeCancel(ctx context.Context, tradeID string) error {
	return c.Send(ctx, protocol.Message{
		Type:      "trade_cancel",
		Payload:   map[string]any{"trade_id": tradeID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// TradeDecline declines a trade offer.
func (c *Client) TradeDecline(ctx context.Context, tradeID string) error {
	return c.Send(ctx, protocol.Message{
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
	// get_wrecks returns type=ok with a "wrecks" key; no "action" field.
	// storeRawJSON shape-detection (line ~3289) and openapi GetWrecksResponse confirm.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("wrecks"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// LootWreck loots items from a wreck.
func (c *Client) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "loot_wreck",
		Payload: map[string]any{
			"wreck_id": wreckID,
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// SalvageWreck salvages a wreck for raw materials.
func (c *Client) SalvageWreck(ctx context.Context, wreckID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "salvage_wreck",
		Payload:   map[string]any{"wreck_id": wreckID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// TowWreck attaches a tow line to a wreck for hauling.
func (c *Client) TowWreck(ctx context.Context, wreckID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "tow_wreck",
		Payload:   map[string]any{"wreck_id": wreckID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ReleaseTow releases a towed wreck at the current location.
func (c *Client) ReleaseTow(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "release_tow",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ScrapWreck scraps a towed wreck for salvage materials.
func (c *Client) ScrapWreck(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "scrap_wreck",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// SellWreck sells a towed wreck to the salvage yard for credits.
func (c *Client) SellWreck(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "sell_wreck",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
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
	// get_cargo returns type=ok with a "cargo" array; no "action" field.
	// State.Ship.Cargo is populated by parseGetCargoData inside
	// handleResponse, which runs BEFORE router dispatch (see client.go
	// read loop). On nil error from execQuery the state write is visible
	// to subsequent GetState() calls — no wall-clock sleep is needed,
	// though GetState() still RLock()s as usual.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("cargo"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
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
	_, err := c.execMutation(ctx, msg, matchCommand("jettison"), terminateOnAction, SleepTick*3)
	return err
}

// ViewStorage views your storage at the current station.
func (c *Client) ViewStorage(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_storage",
		Timestamp: time.Now().UnixMilli(),
	}
	// view_storage returns type=ok with "base_id" as the reliable indicator;
	// storeRawJSON stores under "storage". Items/ships may be omitted when empty.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("base_id"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// ViewStorageAt views your storage at a specific station (without needing to be docked).
func (c *Client) ViewStorageAt(ctx context.Context, stationID string) error {
	msg := protocol.Message{
		Type:      "view_storage",
		Payload:   map[string]any{"station_id": stationID},
		Timestamp: time.Now().UnixMilli(),
	}
	// Same response shape as ViewStorage: "base_id" is the reliable indicator.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("base_id"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
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
	_, err := c.execMutation(ctx, msg, matchCommand("withdraw_items"), terminateOnAction, SleepTick*3)
	return err
}

// WithdrawCredits moves credits from station storage to wallet.
func (c *Client) WithdrawCredits(ctx context.Context, amount float64) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "withdraw_credits",
		Payload:   map[string]any{"amount": amount},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// DepositCredits moves credits from wallet to station storage.
func (c *Client) DepositCredits(ctx context.Context, amount float64) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "deposit_credits",
		Payload:   map[string]any{"amount": amount},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// SendGift sends items or credits to another player's storage at this station.
func (c *Client) SendGift(ctx context.Context, payload map[string]any) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "send_gift",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
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
	// get_recipes returns type=ok with a "recipes" key; no "action" field.
	// storeRawJSON shape-detection (line ~3275) confirms the "recipes" key.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("recipes"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// GetSkills gets your skill progress.
func (c *Client) GetSkills(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_skills",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_skills returns type=ok with "player_skills" key; storeRawJSON stores
	// under "skills". No action field in response.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("player_skills"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// GetNearby gets other players at the current POI.
func (c *Client) GetNearby(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_nearby",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_nearby returns type=ok with "nearby" key; storeRawJSON stores under "nearby".
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("nearby"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// GetBase gets docked base details.
func (c *Client) GetBase(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_base",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_base returns type=ok with "base" (object) and "services" keys per
	// openapi GetBaseResponse schema. Use "services" as classifier: view_orders
	// also has a "base" field but not "services", so "services" is distinctive.
	// storeRawJSON stores under the "base" key (action-switch line ~3092).
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("services"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// GetShip gets detailed ship information.
func (c *Client) GetShip(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_ship",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_ship returns type=ok with no "action" field on the wire (the
	// storeRawJSON action case is dead code for the current server).
	// The distinctive payload key is "ship" — the ship object itself.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("ship"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
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
	// get_missions returns type=ok with "missions" array and "base_id" field; no
	// "action" field on the wire per openapi GetMissionsResponse. Use "base_id"
	// (not "missions" alone) to distinguish from get_active_missions which also
	// has a "missions" key but lacks "base_id".
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("base_id"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// GetActiveMissions views your active missions and progress.
func (c *Client) GetActiveMissions(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_active_missions",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_active_missions returns type=ok with "missions", "total_count", and
	// "max_missions" fields; no "action" field. "max_missions" is unique to this
	// response — storeRawJSON shape-detection uses total_count+max_missions pair,
	// but "max_missions" alone is sufficient and distinctive.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("max_missions"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// AcceptMission accepts a mission from the mission board.
func (c *Client) AcceptMission(ctx context.Context, missionID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "accept_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// CompleteMission completes a mission and claims rewards.
func (c *Client) CompleteMission(ctx context.Context, missionID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "complete_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// AbandonMission abandons an active mission.
func (c *Client) AbandonMission(ctx context.Context, missionID string) error {
	return c.Send(ctx, protocol.Message{
		Type:      "abandon_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// DeclineMission declines a mission and hears the NPC's response.
func (c *Client) DeclineMission(ctx context.Context, templateID string) error {
	return c.Send(ctx, protocol.Message{
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
	if err := c.Send(ctx, protocol.Message{
		Type:      "buy_insurance",
		Payload:   map[string]any{"ticks": ticks},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ClaimInsurance views your active insurance policies.
func (c *Client) ClaimInsurance(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "claim_insurance",
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetInsuranceQuote gets a risk-based insurance quote for the current ship.
func (c *Client) GetInsuranceQuote(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_insurance_quote",
		Timestamp: time.Now().UnixMilli(),
	})
}

// SetHomeBase sets the home base for respawning.
func (c *Client) SetHomeBase(ctx context.Context, baseID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "set_home_base",
		Payload:   map[string]any{"base_id": baseID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ============================================================================
// Notes & Documents
// ============================================================================

// CreateNote creates a new note document.
func (c *Client) CreateNote(ctx context.Context, title, content string) error {
	return c.Send(ctx, protocol.Message{
		Type: "create_note",
		Payload: map[string]any{
			"title":   title,
			"content": content,
		},
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetNotes lists all your note documents.
func (c *Client) GetNotes(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_notes",
		Timestamp: time.Now().UnixMilli(),
	})
}

// ReadNote reads a note document's contents.
func (c *Client) ReadNote(ctx context.Context, noteID string) error {
	return c.Send(ctx, protocol.Message{
		Type:      "read_note",
		Payload:   map[string]any{"note_id": noteID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// WriteNote edits an existing note document.
func (c *Client) WriteNote(ctx context.Context, noteID, content string) error {
	return c.Send(ctx, protocol.Message{
		Type: "write_note",
		Payload: map[string]any{
			"note_id": noteID,
			"content": content,
		},
		Timestamp: time.Now().UnixMilli(),
	})
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
	// catalog responses have {items, page, page_size, total, total_pages, type}.
	// total_pages is distinctive — no other response carries it.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("total_pages"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}

// GetCommands gets a structured list of all commands.
func (c *Client) GetCommands(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
		Type:      "get_guide",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// SearchChangelog searches release notes and version history.
func (c *Client) SearchChangelog(ctx context.Context, payload map[string]any) error {
	return c.Send(ctx, protocol.Message{
		Type:      "search_changelog",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// SearchSystems searches for systems by name.
func (c *Client) SearchSystems(ctx context.Context, query string) error {
	return c.Send(ctx, protocol.Message{
		Type:      "search_systems",
		Payload:   map[string]any{"query": query},
		Timestamp: time.Now().UnixMilli(),
	})
}

// GetVersion gets game version and release notes.
func (c *Client) GetVersion(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_version",
		Timestamp: time.Now().UnixMilli(),
	})
}

// Help gets help for commands.
func (c *Client) Help(ctx context.Context, payload map[string]any) error {
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
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
	// Server response carries {channel, messages[], has_more, total_count}
	// with no "action" field; match on type+channel+shape so concurrent
	// queries on different channels don't collide.
	match := matchAll(
		matchType(protocol.TypeOK),
		matchChannel(channel),
		matchPayloadKey("messages"),
	)
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
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
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
		Type:      "forum_create_thread",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// ForumGetThread gets a forum thread and its replies.
func (c *Client) ForumGetThread(ctx context.Context, threadID string) error {
	return c.Send(ctx, protocol.Message{
		Type:      "forum_get_thread",
		Payload:   map[string]any{"thread_id": threadID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// ForumReply replies to a forum thread.
func (c *Client) ForumReply(ctx context.Context, threadID, content string) error {
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
		Type:      "forum_delete_reply",
		Payload:   map[string]any{"reply_id": replyID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// ForumDeleteThread deletes a forum thread.
func (c *Client) ForumDeleteThread(ctx context.Context, threadID string) error {
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
		Type:      "captains_log_add",
		Payload:   map[string]any{"entry": entry},
		Timestamp: time.Now().UnixMilli(),
	})
}

// CaptainsLogGet gets a specific entry from your captain's log.
func (c *Client) CaptainsLogGet(ctx context.Context, index int) error {
	return c.Send(ctx, protocol.Message{
		Type:      "captains_log_get",
		Payload:   map[string]any{"index": index},
		Timestamp: time.Now().UnixMilli(),
	})
}

// CaptainsLogList lists all entries in your captain's log.
func (c *Client) CaptainsLogList(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "captains_log_list",
		Timestamp: time.Now().UnixMilli(),
	})
}

// ============================================================================
// Player Settings
// ============================================================================

// SetAnonymous sets anonymous mode.
func (c *Client) SetAnonymous(ctx context.Context, anonymous bool) error {
	return c.Send(ctx, protocol.Message{
		Type:      "set_anonymous",
		Payload:   map[string]any{"anonymous": anonymous},
		Timestamp: time.Now().UnixMilli(),
	})
}

// SetColors sets your ship colors.
func (c *Client) SetColors(ctx context.Context, primaryColor, secondaryColor string) error {
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
		Type:      "set_status",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// ============================================================================
// Station Facilities
// ============================================================================

// Facility manages facilities at stations.
func (c *Client) Facility(ctx context.Context, payload map[string]any) error {
	return c.Send(ctx, protocol.Message{
		Type:      "facility",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
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
	// faction_info returns type=ok with "is_member" boolean; no "action" field.
	// openapi FactionInfoResponse and storeRawJSON (line ~3317) confirm "is_member"
	// is always present and distinctive — no other command returns this field.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("is_member"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
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
	return c.Send(ctx, protocol.Message{
		Type:      "faction_list",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// CreateFaction creates a new faction.
func (c *Client) CreateFaction(ctx context.Context, payload map[string]any) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "create_faction",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// JoinFaction joins a faction via invitation.
func (c *Client) JoinFaction(ctx context.Context, factionID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "join_faction",
		Payload:   map[string]any{"faction_id": factionID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// LeaveFaction leaves your current faction.
func (c *Client) LeaveFaction(ctx context.Context) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "leave_faction",
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionInvite invites a player to your faction.
func (c *Client) FactionInvite(ctx context.Context, playerID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_invite",
		Payload:   map[string]any{"player_id": playerID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionKick kicks a player from your faction.
func (c *Client) FactionKick(ctx context.Context, playerID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_kick",
		Payload:   map[string]any{"player_id": playerID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionPromote promotes or demotes a faction member.
func (c *Client) FactionPromote(ctx context.Context, playerID, roleID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "faction_promote",
		Payload: map[string]any{
			"player_id": playerID,
			"role_id":   roleID,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionEdit updates faction description, charter, and colors.
func (c *Client) FactionEdit(ctx context.Context, payload map[string]any) error {
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
		Type:      "faction_edit_role",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionDeleteRole deletes a custom faction role.
func (c *Client) FactionDeleteRole(ctx context.Context, roleID string) error {
	return c.Send(ctx, protocol.Message{
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
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_declare_war",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionProposePeace proposes peace to a faction you're at war with.
func (c *Client) FactionProposePeace(ctx context.Context, targetFactionID, terms string) error {
	payload := map[string]any{"target_faction_id": targetFactionID}
	if terms != "" {
		payload["terms"] = terms
	}
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_propose_peace",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionAcceptPeace accepts a peace proposal.
func (c *Client) FactionAcceptPeace(ctx context.Context, targetFactionID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_accept_peace",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionSetAlly marks another faction as ally.
func (c *Client) FactionSetAlly(ctx context.Context, targetFactionID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_set_ally",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionSetEnemy marks another faction as enemy.
func (c *Client) FactionSetEnemy(ctx context.Context, targetFactionID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_set_enemy",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ============================================================================
// Faction Intel
// ============================================================================

// FactionSubmitIntel submits system intel to your faction's shared map.
func (c *Client) FactionSubmitIntel(ctx context.Context, systems []map[string]any) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_submit_intel",
		Payload:   map[string]any{"systems": systems},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionQueryIntel queries your faction's intel database.
func (c *Client) FactionQueryIntel(ctx context.Context, payload map[string]any) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_query_intel",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionIntelStatus views faction intel coverage statistics.
func (c *Client) FactionIntelStatus(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_intel_status",
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionSubmitTradeIntel submits market price observations to your faction's trade ledger.
func (c *Client) FactionSubmitTradeIntel(ctx context.Context, stations []map[string]any) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_submit_trade_intel",
		Payload:   map[string]any{"stations": stations},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionQueryTradeIntel searches your faction's market price database.
func (c *Client) FactionQueryTradeIntel(ctx context.Context, payload map[string]any) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_query_trade_intel",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionTradeIntelStatus views faction trade intelligence coverage statistics.
func (c *Client) FactionTradeIntelStatus(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_trade_intel_status",
		Timestamp: time.Now().UnixMilli(),
	})
}

// ============================================================================
// Faction Storage & Economy
// ============================================================================

// FactionDepositItems moves items from your cargo to faction storage.
func (c *Client) FactionDepositItems(ctx context.Context, itemID string, quantity int) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "faction_deposit_items",
		Payload: map[string]any{
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionWithdrawItems moves items from faction storage to your cargo.
func (c *Client) FactionWithdrawItems(ctx context.Context, itemID string, quantity int) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "faction_withdraw_items",
		Payload: map[string]any{
			"item_id":  itemID,
			"quantity": quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionDepositCredits transfers credits from your wallet to faction storage.
func (c *Client) FactionDepositCredits(ctx context.Context, amount float64) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_deposit_credits",
		Payload:   map[string]any{"amount": amount},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionWithdrawCredits transfers credits from faction storage to your wallet.
func (c *Client) FactionWithdrawCredits(ctx context.Context, amount float64) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_withdraw_credits",
		Payload:   map[string]any{"amount": amount},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionGift gifts items or credits to a faction's storage.
func (c *Client) FactionGift(ctx context.Context, factionID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["faction_id"] = factionID
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_gift",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ViewFactionStorage views your faction's shared storage at the current station.
func (c *Client) ViewFactionStorage(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "view_faction_storage",
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionCreateBuyOrder creates a buy order on behalf of your faction.
func (c *Client) FactionCreateBuyOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "faction_create_buy_order",
		Payload: map[string]any{
			"item_id":    itemID,
			"price_each": priceEach,
			"quantity":   quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionCreateSellOrder creates a sell order on behalf of your faction.
func (c *Client) FactionCreateSellOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	if err := c.Send(ctx, protocol.Message{
		Type: "faction_create_sell_order",
		Payload: map[string]any{
			"item_id":    itemID,
			"price_each": priceEach,
			"quantity":   quantity,
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ============================================================================
// Faction Missions
// ============================================================================

// FactionListMissions lists your faction's posted missions at this station.
func (c *Client) FactionListMissions(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_list_missions",
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionPostMission posts a mission on your faction's mission board.
func (c *Client) FactionPostMission(ctx context.Context, payload map[string]any) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_post_mission",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// FactionCancelMission cancels a posted faction mission and refunds escrowed rewards.
func (c *Client) FactionCancelMission(ctx context.Context, templateID string) error {
	if err := c.Send(ctx, protocol.Message{
		Type:      "faction_cancel_mission",
		Payload:   map[string]any{"template_id": templateID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}
	return c.waitForActionResponse(ctx, SleepTick)
}

// ============================================================================
// Faction Invites
// ============================================================================

// FactionGetInvites views pending faction invitations.
func (c *Client) FactionGetInvites(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_get_invites",
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionDeclineInvite declines a faction invitation.
func (c *Client) FactionDeclineInvite(ctx context.Context, factionID string) error {
	return c.Send(ctx, protocol.Message{
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
	return c.Send(ctx, protocol.Message{
		Type:      "faction_rooms",
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionVisitRoom visits a room and reads its description.
func (c *Client) FactionVisitRoom(ctx context.Context, roomID string) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_visit_room",
		Payload:   map[string]any{"room_id": roomID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionWriteRoom creates or updates a room in your faction's common space.
func (c *Client) FactionWriteRoom(ctx context.Context, payload map[string]any) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_write_room",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

// FactionDeleteRoom deletes a room from your faction's common space.
func (c *Client) FactionDeleteRoom(ctx context.Context, roomID string) error {
	return c.Send(ctx, protocol.Message{
		Type:      "faction_delete_room",
		Payload:   map[string]any{"room_id": roomID},
		Timestamp: time.Now().UnixMilli(),
	})
}

// RawCommand sends an arbitrary command to the server.
func (c *Client) RawCommand(ctx context.Context, command string, args map[string]any) error {
	return c.Send(ctx, protocol.Message{
		Type:      command,
		Payload:   args,
		Timestamp: time.Now().UnixMilli(),
	})
}
