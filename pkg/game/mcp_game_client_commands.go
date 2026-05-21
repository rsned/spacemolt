package game

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// isTimeoutError checks if an error is a timeout (context deadline, HTTP timeout, etc.).
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "Client.Timeout exceeded")
}

// --- Navigation ---

func (m *MCPGameClient) Undock(ctx context.Context) error {
	result, err := m.callTool(ctx, "undock", nil)
	if err != nil {
		return err
	}
	if err := m.updateStateFromResult(result); err != nil {
		return err
	}
	m.mu.Lock()
	m.state.Doc = false
	m.mu.Unlock()
	return nil
}

func (m *MCPGameClient) Dock(ctx context.Context) error {
	result, err := m.callTool(ctx, "dock", nil)
	if err != nil {
		return err
	}
	if err := m.updateStateFromResult(result); err != nil {
		return err
	}
	// Explicitly set docked state — the dock response has "action": "dock"
	// but may not have a top-level "docked" bool field.
	m.mu.Lock()
	m.state.Doc = true
	m.state.Traveling = false
	m.mu.Unlock()

	// Capture the dock story from the response.
	if text, parseErr := parseToolResultText(result); parseErr == nil {
		var dockResp struct {
			Story string `json:"story"`
		}
		if json.Unmarshal([]byte(text), &dockResp) == nil && dockResp.Story != "" {
			m.mu.Lock()
			m.state.LastDockStory = dockResp.Story
			m.mu.Unlock()
		}
	}
	return nil
}

func (m *MCPGameClient) Travel(ctx context.Context, targetPOI string) (*TravelResult, error) {
	// The MCP server blocks until travel completes and returns an "arrived" response.
	// However, combat can interrupt travel, causing the server to hang or return an
	// error/combat response instead. We use a shorter context timeout and poll on failure.
	travelCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	result, err := m.callTool(travelCtx, "travel", map[string]any{
		"target_poi": targetPOI,
	})
	if err != nil {
		// On timeout or context error, check if we're still in transit or if
		// combat interrupted. Poll get_status to find out.
		if travelCtx.Err() != nil || isTimeoutError(err) {
			return m.recoverFromTravelTimeout(ctx, targetPOI)
		}
		return nil, err
	}
	if err := m.updateStateFromResult(result); err != nil {
		return nil, err
	}

	// Explicitly update POI — the server confirmed arrival at the target.
	m.mu.Lock()
	m.state.CurrentPOI = targetPOI
	m.state.Traveling = false
	m.state.Doc = false // Travel auto-undocks
	m.mu.Unlock()

	return &TravelResult{
		POI:      targetPOI,
		Canceled: false,
	}, nil
}

// recoverFromTravelTimeout polls get_status after a travel timeout to determine
// whether the agent arrived, is still traveling, or was interrupted by combat.
func (m *MCPGameClient) recoverFromTravelTimeout(ctx context.Context, targetPOI string) (*TravelResult, error) {
	m.logger.Printf("[MCP] Travel timed out, polling status to check for combat interruption...")

	deadline := time.Now().Add(3 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return &TravelResult{Canceled: true}, fmt.Errorf("travel to %s: timed out waiting for arrival or combat resolution", targetPOI)
		}

		if err := m.GetStatus(ctx); err != nil {
			m.logger.Printf("[MCP] get_status failed during recovery: %v", err)
			time.Sleep(SleepTick)
			continue
		}

		state := m.GetState()

		// Arrived at destination
		if !state.Traveling && state.CurrentPOI == targetPOI {
			m.logger.Printf("[MCP] Recovery: arrived at %s", targetPOI)
			return &TravelResult{POI: targetPOI, Canceled: false}, nil
		}

		// No longer traveling but not at destination — combat interrupted
		if !state.Traveling {
			m.logger.Printf("[MCP] Recovery: travel interrupted (at %s, not %s)", state.CurrentPOI, targetPOI)
			return &TravelResult{POI: state.CurrentPOI, Canceled: true}, nil
		}

		// Still traveling — keep polling
		m.logger.Printf("[MCP] Recovery: still in transit, waiting...")
		time.Sleep(SleepTick)
	}
}

func (m *MCPGameClient) Jump(ctx context.Context, targetSystem string) (*JumpResult, error) {
	// Same timeout handling as Travel — combat can interrupt jumps too.
	jumpCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	result, err := m.callTool(jumpCtx, "jump", map[string]any{
		"target_system": targetSystem,
	})
	if err != nil {
		if jumpCtx.Err() != nil || isTimeoutError(err) {
			return m.recoverFromJumpTimeout(ctx, targetSystem)
		}
		return nil, err
	}
	if err := m.updateStateFromResult(result); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.state.Traveling = false
	m.state.Doc = false
	m.mu.Unlock()

	state := m.GetState()
	return &JumpResult{
		SystemID:   state.CurrentSystem,
		SystemName: state.System.Name,
		POI:        state.CurrentPOI,
		Canceled:   false,
	}, nil
}

// recoverFromJumpTimeout polls get_status after a jump timeout.
func (m *MCPGameClient) recoverFromJumpTimeout(ctx context.Context, targetSystem string) (*JumpResult, error) {
	m.logger.Printf("[MCP] Jump timed out, polling status to check for combat interruption...")

	deadline := time.Now().Add(3 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return &JumpResult{Canceled: true}, fmt.Errorf("jump to %s: timed out waiting for arrival or combat resolution", targetSystem)
		}

		if err := m.GetStatus(ctx); err != nil {
			m.logger.Printf("[MCP] get_status failed during recovery: %v", err)
			time.Sleep(SleepTick)
			continue
		}

		state := m.GetState()

		// Arrived in target system
		if !state.Traveling && strings.EqualFold(state.System.ID, targetSystem) {
			m.logger.Printf("[MCP] Recovery: arrived in %s", state.System.Name)
			return &JumpResult{
				SystemID:   state.CurrentSystem,
				SystemName: state.System.Name,
				POI:        state.CurrentPOI,
				Canceled:   false,
			}, nil
		}

		// No longer traveling but not in target system — combat interrupted
		if !state.Traveling {
			m.logger.Printf("[MCP] Recovery: jump interrupted (in %s, not %s)", state.System.Name, targetSystem)
			return &JumpResult{
				SystemID:   state.CurrentSystem,
				SystemName: state.System.Name,
				POI:        state.CurrentPOI,
				Canceled:   true,
			}, nil
		}

		m.logger.Printf("[MCP] Recovery: still in transit, waiting...")
		time.Sleep(SleepTick)
	}
}

// --- Mining & Scanning ---

func (m *MCPGameClient) Mine(ctx context.Context) error {
	result, err := m.callTool(ctx, "mine", nil)
	if err != nil {
		return err
	}
	if err := m.updateStateFromResult(result); err != nil {
		return err
	}

	// Update XP quantity from mining yield so observations reflect units mined.
	m.rawJSONMu.RLock()
	raw := m.latestRawJSON["_last"]
	m.rawJSONMu.RUnlock()
	if len(raw) > 0 {
		var yield struct {
			Quantity float64 `json:"quantity"`
		}
		if json.Unmarshal(raw, &yield) == nil && yield.Quantity > 0 {
			m.xpMu.Lock()
			m.xpLastQuantity = int(yield.Quantity)
			m.xpMu.Unlock()
		}
	}
	return nil
}

func (m *MCPGameClient) Scan(ctx context.Context) error {
	result, err := m.callTool(ctx, "scan", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) ScanTarget(ctx context.Context, targetID string) error {
	result, err := m.callTool(ctx, "scan", map[string]any{"target_id": targetID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// --- Combat ---

func (m *MCPGameClient) Attack(ctx context.Context, targetID string) error {
	result, err := m.callTool(ctx, "attack", map[string]any{
		"target_id": targetID,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) Cloak(ctx context.Context, enable bool) error {
	result, err := m.callTool(ctx, "cloak", map[string]any{
		"enable": enable,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// --- Commerce ---

func (m *MCPGameClient) Sell(ctx context.Context, itemID string, quantity float64) error {
	result, err := m.callTool(ctx, "sell", map[string]any{
		"item_id":  itemID,
		"quantity": int(quantity),
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) SellAllBulk(ctx context.Context, reservedItems []string) error {
	// Get current cargo and sell everything not in the reserved list.
	state := m.GetState()
	for _, item := range state.Ship.Cargo {
		if isReserved(item.ItemID, reservedItems) {
			continue
		}
		if item.Quantity <= 0 {
			continue
		}
		if err := m.Sell(ctx, item.ItemID, item.Quantity); err != nil {
			// Log but continue selling other items.
			m.logger.Printf("[MCP] Warning: failed to sell %s: %v", item.ItemID, err)
		}
	}
	return nil
}

func (m *MCPGameClient) Buy(ctx context.Context, itemID string, quantity float64) error {
	result, err := m.callTool(ctx, "buy", map[string]any{
		"item_id":  itemID,
		"quantity": int(quantity),
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetListings(ctx context.Context) error {
	result, err := m.callTool(ctx, "view_market", nil)
	if err != nil {
		return err
	}
	// Parse listings from the result. view_market returns an "items" array
	// of aggregated order book entries, not individual listings.
	text, parseErr := parseToolResultText(result)
	if parseErr == nil {
		m.rawJSONMu.Lock()
		m.latestRawJSON["market"] = []byte(text)
		m.rawJSONMu.Unlock()

		var resp struct {
			Items []serverapi.ViewMarketItem `json:"items"`
		}
		if jsonErr := json.Unmarshal([]byte(text), &resp); jsonErr == nil && len(resp.Items) > 0 {
			// Convert aggregated order book items into synthetic MarketListings.
			var listings []MarketListing
			for _, item := range resp.Items {
				if item.BestSell > 0 {
					listing := MarketListing{
						ItemID:       item.ItemID,
						ItemType:     inferItemType(item.ItemID),
						PricePerUnit: item.BestSell,
						Type:         "sell",
					}
					for _, order := range item.SellOrders {
						listing.Quantity += order.Quantity
					}
					listings = append(listings, listing)
				}
				if item.BestBuy > 0 {
					listing := MarketListing{
						ItemID:       item.ItemID,
						ItemType:     inferItemType(item.ItemID),
						PricePerUnit: item.BestBuy,
						Type:         "buy",
					}
					for _, order := range item.BuyOrders {
						listing.Quantity += order.Quantity
					}
					listings = append(listings, listing)
				}
			}
			m.listingsMu.Lock()
			m.latestListings = listings
			m.listingsMu.Unlock()
		}
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetTrades(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_trades", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// --- Crafting ---

func (m *MCPGameClient) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error {
	result, err := m.callTool(ctx, "craft", map[string]any{
		"recipe_id": recipeID,
		"quantity":  quantity,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetRecipes(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_skills", nil)
	if err != nil {
		return err
	}
	// get_skills returns recipes too; but there's a dedicated get_recipes... hmm.
	// Actually the interface says GetRecipes, let's use the right tool.
	_ = result
	result2, err := m.callTool(ctx, "catalog", map[string]any{
		"type": "recipe",
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result2)
}

// --- Ship Maintenance ---

func (m *MCPGameClient) Refuel(ctx context.Context) error {
	result, err := m.callTool(ctx, "refuel", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) Repair(ctx context.Context) error {
	result, err := m.callTool(ctx, "repair", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) RepairWith(ctx context.Context, payload map[string]any) error {
	result, err := m.callTool(ctx, "repair", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) Fleet(ctx context.Context, action string, playerID string) error {
	payload := map[string]any{"action": action}
	if playerID != "" {
		payload["player_id"] = playerID
	}
	result, err := m.callTool(ctx, "fleet", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) DistressSignal(ctx context.Context, distressType string) error {
	payload := map[string]any{}
	if distressType != "" {
		payload["distress_type"] = distressType
	}
	result, err := m.callTool(ctx, "distress_signal", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) InstallMod(ctx context.Context, moduleID string) error {
	result, err := m.callTool(ctx, "install_mod", map[string]any{
		"module_id": moduleID,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) RefitShip(ctx context.Context) error {
	result, err := m.callTool(ctx, "refit_ship", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) UninstallMod(ctx context.Context, moduleID string) error {
	result, err := m.callTool(ctx, "uninstall_mod", map[string]any{
		"module_id": moduleID,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) BuyShip(ctx context.Context, shipClass string) error {
	result, err := m.callTool(ctx, "buy_ship", map[string]any{
		"ship_class": shipClass,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) BrowseShips(ctx context.Context, payload map[string]any) error {
	result, err := m.callTool(ctx, "browse_ships", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) BuyInsurance(ctx context.Context, ticks int) error {
	result, err := m.callTool(ctx, "buy_insurance", map[string]any{
		"ticks": ticks,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) ClaimInsurance(ctx context.Context) error {
	result, err := m.callTool(ctx, "claim_insurance", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// --- Cargo & Storage ---

func (m *MCPGameClient) DepositItems(ctx context.Context, itemID string, quantity float64) error {
	result, err := m.callTool(ctx, "deposit_items", map[string]any{
		"item_id":  itemID,
		"quantity": int(quantity),
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) DepositAllItems(ctx context.Context) error {
	// Refresh cargo before depositing to avoid using stale state.
	if err := m.GetCargo(ctx); err != nil {
		m.logger.Printf("[MCP] Warning: failed to refresh cargo before deposit_all: %v", err)
	}

	state := m.GetState()
	for _, item := range state.Ship.Cargo {
		if item.Quantity <= 0 {
			continue
		}
		if err := m.DepositItems(ctx, item.ItemID, item.Quantity); err != nil {
			m.logger.Printf("[MCP] Warning: failed to deposit %s: %v", item.ItemID, err)
		}
	}
	return nil
}

func (m *MCPGameClient) WithdrawItems(ctx context.Context, itemID string, quantity float64) error {
	result, err := m.callTool(ctx, "withdraw_items", map[string]any{
		"item_id":  itemID,
		"quantity": int(quantity),
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) DepositItemsPayload(ctx context.Context, payload map[string]any) error {
	result, err := m.callTool(ctx, "deposit_items", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) WithdrawItemsPayload(ctx context.Context, payload map[string]any) error {
	result, err := m.callTool(ctx, "withdraw_items", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) ViewStorage(ctx context.Context) error {
	result, err := m.callTool(ctx, "view_storage", nil)
	if err != nil {
		return err
	}
	// Cache the raw storage response for GetRawJSON("storage")
	if text, parseErr := parseToolResultText(result); parseErr == nil {
		m.rawJSONMu.Lock()
		m.latestRawJSON["storage"] = []byte(text)
		m.rawJSONMu.Unlock()
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) ViewStorageAt(ctx context.Context, stationID string) error {
	result, err := m.callTool(ctx, "view_storage", map[string]any{"station_id": stationID})
	if err != nil {
		return err
	}
	// Cache the raw storage response for GetRawJSON("storage")
	if text, parseErr := parseToolResultText(result); parseErr == nil {
		m.rawJSONMu.Lock()
		m.latestRawJSON["storage"] = []byte(text)
		m.rawJSONMu.Unlock()
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) Jettison(ctx context.Context, itemID string, quantity float64) error {
	result, err := m.callTool(ctx, "jettison", map[string]any{
		"item_id":  itemID,
		"quantity": int(quantity),
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// --- Wrecks ---

func (m *MCPGameClient) GetWrecks(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_wrecks", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "wrecks")
}

func (m *MCPGameClient) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error {
	result, err := m.callTool(ctx, "loot_wreck", map[string]any{
		"wreck_id": wreckID,
		"item_id":  itemID,
		"quantity": int(quantity),
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) SalvageWreck(ctx context.Context, wreckID string) error {
	result, err := m.callTool(ctx, "salvage_wreck", map[string]any{
		"wreck_id": wreckID,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// --- Queries ---

func (m *MCPGameClient) GetSystem(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_system", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "system")
}

func (m *MCPGameClient) GetStatus(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_status", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "status")
}

func (m *MCPGameClient) GetNotifications(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_notifications", nil)
	if err != nil {
		return err
	}
	m.dispatchChatNotifications(result)
	return m.cacheResultAs(result, "notifications")
}

func (m *MCPGameClient) GetShip(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_ship", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "ship")
}

func (m *MCPGameClient) GetCargo(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_cargo", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "cargo")
}

func (m *MCPGameClient) GetSkills(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_skills", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "skills")
}

func (m *MCPGameClient) GetPOI(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_poi", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "poi")
}

func (m *MCPGameClient) GetBase(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_base", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "base")
}

func (m *MCPGameClient) GetMap(ctx context.Context, force ...bool) error {
	result, err := m.callTool(ctx, "get_map", nil)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.state.LastMapUpdate = now()
	m.mu.Unlock()
	return m.cacheResultAs(result, "systems")
}

func (m *MCPGameClient) GetNearby(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_nearby", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "nearby")
}

func (m *MCPGameClient) GetSystemAgents(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_system_agents", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "get_system_agents")
}

func (m *MCPGameClient) GetVersion(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_version", nil)
	if err != nil {
		return err
	}
	// Parse version from result.
	text, parseErr := parseToolResultText(result)
	if parseErr == nil {
		m.rawJSONMu.Lock()
		m.latestRawJSON["version"] = []byte(text)
		m.latestRawJSON["_last"] = []byte(text)
		m.rawJSONMu.Unlock()

		var vResp struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(text), &vResp); err == nil && vResp.Version != "" {
			m.mu.Lock()
			m.state.ServerVersion = vResp.Version
			m.mu.Unlock()
		}
	}
	return nil
}

func (m *MCPGameClient) Help(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "help", payload)
	return err
}

// --- Route Planning ---

func (m *MCPGameClient) FindRoute(ctx context.Context, targetSystem string) ([]RouteStep, error) {
	result, err := m.callTool(ctx, "find_route", map[string]any{
		"target_system": targetSystem,
	})
	if err != nil {
		return nil, err
	}

	text, err := parseToolResultText(result)
	if err != nil {
		return nil, err
	}

	// Cache as _last for interactive tools.
	m.rawJSONMu.Lock()
	m.latestRawJSON["_last"] = []byte(text)
	m.rawJSONMu.Unlock()

	var routeResp struct {
		Route []RouteStep `json:"route"`
	}
	if err := json.Unmarshal([]byte(text), &routeResp); err != nil {
		return nil, fmt.Errorf("parsing route response: %w", err)
	}

	return routeResp.Route, nil
}

// --- Faction ---

func (m *MCPGameClient) CreateFaction(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "create_faction", payload)
	return err
}

func (m *MCPGameClient) JoinFaction(ctx context.Context, factionID string) error {
	_, err := m.callTool(ctx, "join_faction", map[string]any{
		"faction_id": factionID,
	})
	return err
}

func (m *MCPGameClient) LeaveFaction(ctx context.Context) error {
	_, err := m.callTool(ctx, "leave_faction", nil)
	return err
}

func (m *MCPGameClient) FactionInvite(ctx context.Context, playerID string) error {
	_, err := m.callTool(ctx, "faction_invite", map[string]any{
		"player_id": playerID,
	})
	return err
}

func (m *MCPGameClient) FactionKick(ctx context.Context, playerID string) error {
	_, err := m.callTool(ctx, "faction_kick", map[string]any{
		"player_id": playerID,
	})
	return err
}

func (m *MCPGameClient) FactionPromote(ctx context.Context, playerID, roleID string) error {
	_, err := m.callTool(ctx, "faction_promote", map[string]any{
		"player_id": playerID,
		"role_id":   roleID,
	})
	return err
}

// --- Communication ---

func (m *MCPGameClient) Chat(ctx context.Context, channel, content string, targetID string) error {
	args := map[string]any{
		"channel": channel,
		"content": content,
	}
	if targetID != "" {
		args["target_id"] = targetID
	}
	_, err := m.callTool(ctx, "chat", args)
	return err
}

func (m *MCPGameClient) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error {
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["channel"] = channel
	result, err := m.callTool(ctx, "get_chat_history", payload)
	if err != nil {
		return err
	}

	// Parse chat history into state.
	text, err := parseToolResultText(result)
	if err != nil {
		return err
	}
	// Cache as _last for interactive tools.
	m.rawJSONMu.Lock()
	m.latestRawJSON["_last"] = []byte(text)
	m.rawJSONMu.Unlock()

	var chatResp struct {
		Messages []ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(text), &chatResp); err == nil {
		m.mu.Lock()
		m.state.LastChatHistory = chatResp.Messages
		m.mu.Unlock()
	}
	return nil
}

func (m *MCPGameClient) SetPlayerStatus(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "set_status", payload)
	return err
}

func (m *MCPGameClient) SetHomeBase(ctx context.Context, baseID string) error {
	_, err := m.callTool(ctx, "set_home_base", map[string]any{
		"base_id": baseID,
	})
	return err
}

// --- Forum ---

func (m *MCPGameClient) ForumList(ctx context.Context, page int) error {
	_, err := m.callTool(ctx, "forum_list", map[string]any{
		"page": page,
	})
	return err
}

func (m *MCPGameClient) ForumCreateThread(ctx context.Context, title, content string, category string) error {
	args := map[string]any{
		"title":   title,
		"content": content,
	}
	if category != "" {
		args["category"] = category
	}
	_, err := m.callTool(ctx, "forum_create_thread", args)
	return err
}

func (m *MCPGameClient) ForumGetThread(ctx context.Context, threadID string) error {
	_, err := m.callTool(ctx, "forum_get_thread", map[string]any{
		"thread_id": threadID,
	})
	return err
}

func (m *MCPGameClient) ForumReply(ctx context.Context, threadID, content string) error {
	_, err := m.callTool(ctx, "forum_reply", map[string]any{
		"thread_id": threadID,
		"content":   content,
	})
	return err
}

func (m *MCPGameClient) ForumUpvote(ctx context.Context, threadID string, replyID string) error {
	args := map[string]any{}
	if threadID != "" {
		args["thread_id"] = threadID
	}
	if replyID != "" {
		args["reply_id"] = replyID
	}
	_, err := m.callTool(ctx, "forum_upvote", args)
	return err
}

func (m *MCPGameClient) ForumDeleteThread(ctx context.Context, threadID string) error {
	_, err := m.callTool(ctx, "forum_delete_thread", map[string]any{
		"thread_id": threadID,
	})
	return err
}

func (m *MCPGameClient) ForumDeleteReply(ctx context.Context, replyID string) error {
	_, err := m.callTool(ctx, "forum_delete_reply", map[string]any{
		"reply_id": replyID,
	})
	return err
}

// --- Notes ---

func (m *MCPGameClient) CreateNote(ctx context.Context, title, content string) error {
	_, err := m.callTool(ctx, "create_note", map[string]any{
		"title":   title,
		"content": content,
	})
	return err
}

func (m *MCPGameClient) WriteNote(ctx context.Context, noteID, content string) error {
	_, err := m.callTool(ctx, "write_note", map[string]any{
		"note_id": noteID,
		"content": content,
	})
	return err
}

func (m *MCPGameClient) GetNotes(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_notes", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "notes")
}

// Ship Management

func (m *MCPGameClient) ListShips(ctx context.Context) error {
	_, err := m.callTool(ctx, "list_ships", nil)
	return err
}

func (m *MCPGameClient) SwitchShip(ctx context.Context, shipID string) error {
	result, err := m.callTool(ctx, "switch_ship", map[string]any{"ship_id": shipID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) SellShip(ctx context.Context, shipID string) error {
	result, err := m.callTool(ctx, "sell_ship", map[string]any{"ship_id": shipID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// Exchange

func (m *MCPGameClient) CreateSellOrder(ctx context.Context, payload map[string]any) error {
	result, err := m.callTool(ctx, "create_sell_order", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) CreateBuyOrder(ctx context.Context, payload map[string]any) error {
	result, err := m.callTool(ctx, "create_buy_order", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) ViewMarket(ctx context.Context, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	result, err := m.callTool(ctx, "view_market", payload)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "view_market")
}

func (m *MCPGameClient) ViewOrders(ctx context.Context) error {
	result, err := m.callTool(ctx, "view_orders", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "orders")
}

// Action Log

func (m *MCPGameClient) GetActionLog(ctx context.Context, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	result, err := m.callTool(ctx, "get_action_log", payload)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "action_log")
}

// Missions

func (m *MCPGameClient) GetMissions(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_missions", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "missions")
}

func (m *MCPGameClient) AcceptMission(ctx context.Context, missionID string) error {
	result, err := m.callTool(ctx, "accept_mission", map[string]any{"mission_id": missionID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// Survey

func (m *MCPGameClient) SurveySystem(ctx context.Context) error {
	_, err := m.callTool(ctx, "survey_system", nil)
	return err
}

// Captain's Log

func (m *MCPGameClient) CaptainsLogAdd(ctx context.Context, entry string) error {
	_, err := m.callTool(ctx, "captains_log_add", map[string]any{"entry": entry})
	return err
}

func (m *MCPGameClient) CaptainsLogList(ctx context.Context) error {
	result, err := m.callTool(ctx, "captains_log_list", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "captains_log_list")
}

// --- Additional query methods ---

func (m *MCPGameClient) FactionInfo(ctx context.Context) error {
	result, err := m.callTool(ctx, "faction_info", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_info")
}

func (m *MCPGameClient) GetActiveMissions(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_active_missions", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "active_missions")
}

func (m *MCPGameClient) GetCommands(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_commands", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "commands")
}

func (m *MCPGameClient) GetInsuranceQuote(ctx context.Context) error {
	// Use claim_insurance to get quote info — there's no dedicated quote tool
	// but the get_status response includes insurance info
	result, err := m.callTool(ctx, "get_status", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "insurance_quote")
}

func (m *MCPGameClient) Catalog(ctx context.Context, catalogType string, page, pageSize int) error {
	args := map[string]any{
		"type":      catalogType,
		"page":      page,
		"page_size": pageSize,
	}
	result, err := m.callTool(ctx, "catalog", args)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "catalog")
}

// GetMarketListings returns cached market listings from the last GetListings call.
func (m *MCPGameClient) GetMarketListings() []MarketListing {
	m.listingsMu.RLock()
	defer m.listingsMu.RUnlock()
	result := make([]MarketListing, len(m.latestListings))
	copy(result, m.latestListings)
	return result
}

// GetRawJSON returns cached raw JSON for the given key, if available.
func (m *MCPGameClient) GetRawJSON(key string) []byte {
	m.rawJSONMu.RLock()
	defer m.rawJSONMu.RUnlock()
	return m.latestRawJSON[key]
}

// --- Helpers ---

func isReserved(itemID string, reserved []string) bool {
	for _, r := range reserved {
		if r == itemID {
			return true
		}
	}
	return false
}

func (m *MCPGameClient) RawCommand(ctx context.Context, command string, args map[string]any) error {
	result, err := m.callTool(ctx, command, args)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// --- Batch 4.5 additions: methods added to GameClient interface for REPL migration ---

func (m *MCPGameClient) Battle(ctx context.Context, action string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["action"] = action
	_, err := m.callTool(ctx, "battle", payload)
	return err
}

func (m *MCPGameClient) Reload(ctx context.Context, weaponInstanceID, ammoItemID string) error {
	_, err := m.callTool(ctx, "reload", map[string]any{
		"weapon_instance_id": weaponInstanceID,
		"ammo_item_id":       ammoItemID,
	})
	return err
}

func (m *MCPGameClient) EstimatePurchase(ctx context.Context, itemID string, quantity int) error {
	_, err := m.callTool(ctx, "estimate_purchase", map[string]any{
		"item_id":  itemID,
		"quantity": quantity,
	})
	return err
}

func (m *MCPGameClient) CancelOrder(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "cancel_order", payload)
	return err
}

func (m *MCPGameClient) ModifyOrder(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "modify_order", payload)
	return err
}

func (m *MCPGameClient) TradeOffer(ctx context.Context, targetID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["target_id"] = targetID
	_, err := m.callTool(ctx, "trade_offer", payload)
	return err
}

func (m *MCPGameClient) TradeAccept(ctx context.Context, tradeID string) error {
	_, err := m.callTool(ctx, "trade_accept", map[string]any{"trade_id": tradeID})
	return err
}

func (m *MCPGameClient) TradeCancel(ctx context.Context, tradeID string) error {
	_, err := m.callTool(ctx, "trade_cancel", map[string]any{"trade_id": tradeID})
	return err
}

func (m *MCPGameClient) TradeDecline(ctx context.Context, tradeID string) error {
	_, err := m.callTool(ctx, "trade_decline", map[string]any{"trade_id": tradeID})
	return err
}

func (m *MCPGameClient) BuyListedShip(ctx context.Context, listingID string) error {
	_, err := m.callTool(ctx, "buy_listed_ship", map[string]any{"listing_id": listingID})
	return err
}

func (m *MCPGameClient) ListShipForSale(ctx context.Context, shipID string, price float64) error {
	_, err := m.callTool(ctx, "list_ship_for_sale", map[string]any{"ship_id": shipID, "price": price})
	return err
}

func (m *MCPGameClient) CommissionQuote(ctx context.Context, shipClass string) error {
	_, err := m.callTool(ctx, "commission_quote", map[string]any{"ship_class": shipClass})
	return err
}

func (m *MCPGameClient) CommissionStatus(ctx context.Context, baseID string) error {
	payload := map[string]any{}
	if baseID != "" {
		payload["base_id"] = baseID
	}
	_, err := m.callTool(ctx, "commission_status", payload)
	return err
}

func (m *MCPGameClient) CancelCommission(ctx context.Context, commissionID string) error {
	_, err := m.callTool(ctx, "cancel_commission", map[string]any{"commission_id": commissionID})
	return err
}

func (m *MCPGameClient) ClaimCommission(ctx context.Context, commissionID string) error {
	_, err := m.callTool(ctx, "claim_commission", map[string]any{"commission_id": commissionID})
	return err
}

func (m *MCPGameClient) CommissionShip(ctx context.Context, shipClass string, provideMaterials bool) error {
	_, err := m.callTool(ctx, "commission_ship", map[string]any{
		"ship_class":        shipClass,
		"provide_materials": provideMaterials,
	})
	return err
}

func (m *MCPGameClient) TowWreck(ctx context.Context, wreckID string) error {
	_, err := m.callTool(ctx, "tow_wreck", map[string]any{"wreck_id": wreckID})
	return err
}

func (m *MCPGameClient) UseItem(ctx context.Context, itemID string, quantity int) error {
	payload := map[string]any{"item_id": itemID}
	if quantity > 0 {
		payload["quantity"] = quantity
	}
	_, err := m.callTool(ctx, "use_item", payload)
	return err
}

func (m *MCPGameClient) CaptainsLogGet(ctx context.Context, index int) error {
	result, err := m.callTool(ctx, "captains_log_get", map[string]any{"index": index})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "captains_log_get")
}

func (m *MCPGameClient) SearchSystems(ctx context.Context, query string) error {
	result, err := m.callTool(ctx, "search_systems", map[string]any{"query": query})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "search_systems")
}

func (m *MCPGameClient) GetGuide(ctx context.Context, guide string) error {
	payload := map[string]any{}
	if guide != "" {
		payload["guide"] = guide
	}
	result, err := m.callTool(ctx, "get_guide", payload)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "guide")
}

func (m *MCPGameClient) FactionList(ctx context.Context, limit, offset int) error {
	args := map[string]any{}
	if limit > 0 {
		args["limit"] = limit
	}
	if offset > 0 {
		args["offset"] = offset
	}
	result, err := m.callTool(ctx, "faction_list", args)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_list")
}

func (m *MCPGameClient) FactionEdit(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "faction_edit", payload)
	return err
}

func (m *MCPGameClient) FactionGetInvites(ctx context.Context) error {
	result, err := m.callTool(ctx, "faction_get_invites", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_invites")
}

func (m *MCPGameClient) FactionDeclineInvite(ctx context.Context, factionID string) error {
	_, err := m.callTool(ctx, "faction_decline_invite", map[string]any{"faction_id": factionID})
	return err
}

func (m *MCPGameClient) FactionDeclareWar(ctx context.Context, targetFactionID, reason string) error {
	payload := map[string]any{"target_faction_id": targetFactionID}
	if reason != "" {
		payload["reason"] = reason
	}
	_, err := m.callTool(ctx, "faction_declare_war", payload)
	return err
}

func (m *MCPGameClient) FactionProposePeace(ctx context.Context, targetFactionID, terms string) error {
	payload := map[string]any{"target_faction_id": targetFactionID}
	if terms != "" {
		payload["terms"] = terms
	}
	_, err := m.callTool(ctx, "faction_propose_peace", payload)
	return err
}

func (m *MCPGameClient) FactionAcceptPeace(ctx context.Context, targetFactionID string) error {
	_, err := m.callTool(ctx, "faction_accept_peace", map[string]any{"target_faction_id": targetFactionID})
	return err
}

func (m *MCPGameClient) FactionProposeAlly(ctx context.Context, targetFactionID string) error {
	_, err := m.callTool(ctx, "faction_propose_ally", map[string]any{"target_faction_id": targetFactionID})
	return err
}

func (m *MCPGameClient) FactionAcceptAlly(ctx context.Context, targetFactionID string) error {
	_, err := m.callTool(ctx, "faction_accept_ally", map[string]any{"target_faction_id": targetFactionID})
	return err
}

func (m *MCPGameClient) FactionRemoveAlly(ctx context.Context, targetFactionID string) error {
	_, err := m.callTool(ctx, "faction_remove_ally", map[string]any{"target_faction_id": targetFactionID})
	return err
}

func (m *MCPGameClient) FactionSetEnemy(ctx context.Context, targetFactionID string) error {
	_, err := m.callTool(ctx, "faction_set_enemy", map[string]any{"target_faction_id": targetFactionID})
	return err
}

func (m *MCPGameClient) FactionDepositCredits(ctx context.Context, amount float64) error {
	_, err := m.callTool(ctx, "faction_deposit_credits", map[string]any{"amount": amount})
	return err
}

func (m *MCPGameClient) FactionWithdrawCredits(ctx context.Context, amount float64) error {
	_, err := m.callTool(ctx, "faction_withdraw_credits", map[string]any{"amount": amount})
	return err
}

func (m *MCPGameClient) FactionDepositItems(ctx context.Context, itemID string, quantity int) error {
	_, err := m.callTool(ctx, "faction_deposit_items", map[string]any{
		"item_id":  itemID,
		"quantity": quantity,
	})
	return err
}

func (m *MCPGameClient) FactionWithdrawItems(ctx context.Context, itemID string, quantity int) error {
	_, err := m.callTool(ctx, "faction_withdraw_items", map[string]any{
		"item_id":  itemID,
		"quantity": quantity,
	})
	return err
}

func (m *MCPGameClient) FactionDepositItemsPayload(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "faction_deposit_items", payload)
	return err
}

func (m *MCPGameClient) FactionWithdrawItemsPayload(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "faction_withdraw_items", payload)
	return err
}

func (m *MCPGameClient) ViewFactionStorage(ctx context.Context) error {
	result, err := m.callTool(ctx, "view_faction_storage", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_storage")
}

// ViewFactionStorageAt views your faction's shared storage at a specific station.
// As of v0.299.0, you can query remotely with station_id as long as you're a faction member.
func (m *MCPGameClient) ViewFactionStorageAt(ctx context.Context, stationID string) error {
	result, err := m.callTool(ctx, "view_faction_storage", map[string]any{
		"station_id": stationID,
	})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_storage")
}

func (m *MCPGameClient) FactionCreateBuyOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	_, err := m.callTool(ctx, "faction_create_buy_order", map[string]any{
		"item_id":    itemID,
		"price_each": priceEach,
		"quantity":   quantity,
	})
	return err
}

func (m *MCPGameClient) FactionCreateSellOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	_, err := m.callTool(ctx, "faction_create_sell_order", map[string]any{
		"item_id":    itemID,
		"price_each": priceEach,
		"quantity":   quantity,
	})
	return err
}

func (m *MCPGameClient) FactionCreateRole(ctx context.Context, name string, priority int, permissions map[string]any) error {
	payload := map[string]any{"name": name, "priority": priority}
	if permissions != nil {
		payload["permissions"] = permissions
	}
	_, err := m.callTool(ctx, "faction_create_role", payload)
	return err
}

func (m *MCPGameClient) FactionEditRole(ctx context.Context, roleID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["role_id"] = roleID
	_, err := m.callTool(ctx, "faction_edit_role", payload)
	return err
}

func (m *MCPGameClient) FactionDeleteRole(ctx context.Context, roleID string) error {
	_, err := m.callTool(ctx, "faction_delete_role", map[string]any{"role_id": roleID})
	return err
}

func (m *MCPGameClient) FactionQueryIntel(ctx context.Context, payload map[string]any) error {
	result, err := m.callTool(ctx, "faction_query_intel", payload)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_intel")
}

func (m *MCPGameClient) FactionQueryTradeIntel(ctx context.Context, payload map[string]any) error {
	result, err := m.callTool(ctx, "faction_query_trade_intel", payload)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_trade_intel")
}

func (m *MCPGameClient) FactionIntelStatus(ctx context.Context) error {
	result, err := m.callTool(ctx, "faction_intel_status", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_intel_status")
}

func (m *MCPGameClient) FactionTradeIntelStatus(ctx context.Context) error {
	result, err := m.callTool(ctx, "faction_trade_intel_status", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_trade_intel_status")
}

func (m *MCPGameClient) FactionRooms(ctx context.Context) error {
	result, err := m.callTool(ctx, "faction_rooms", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_rooms")
}

func (m *MCPGameClient) FactionVisitRoom(ctx context.Context, roomID string) error {
	result, err := m.callTool(ctx, "faction_visit_room", map[string]any{"room_id": roomID})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_room")
}

func (m *MCPGameClient) FactionWriteRoom(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "faction_write_room", payload)
	return err
}

func (m *MCPGameClient) FactionDeleteRoom(ctx context.Context, roomID string) error {
	_, err := m.callTool(ctx, "faction_delete_room", map[string]any{"room_id": roomID})
	return err
}

func (m *MCPGameClient) FactionListMissions(ctx context.Context) error {
	result, err := m.callTool(ctx, "faction_list_missions", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "faction_missions")
}

func (m *MCPGameClient) FactionCancelMission(ctx context.Context, templateID string) error {
	_, err := m.callTool(ctx, "faction_cancel_mission", map[string]any{"template_id": templateID})
	return err
}

func (m *MCPGameClient) SendGift(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "send_gift", payload)
	return err
}

func (m *MCPGameClient) ReadNote(ctx context.Context, noteID string) error {
	result, err := m.callTool(ctx, "read_note", map[string]any{"note_id": noteID})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "note")
}

func (m *MCPGameClient) CompleteMission(ctx context.Context, missionID string) error {
	_, err := m.callTool(ctx, "complete_mission", map[string]any{"mission_id": missionID})
	return err
}

func (m *MCPGameClient) AbandonMission(ctx context.Context, missionID string) error {
	_, err := m.callTool(ctx, "abandon_mission", map[string]any{"mission_id": missionID})
	return err
}

func (m *MCPGameClient) DeclineMission(ctx context.Context, templateID string) error {
	_, err := m.callTool(ctx, "decline_mission", map[string]any{"template_id": templateID})
	return err
}

func (m *MCPGameClient) SetAnonymous(ctx context.Context, anonymous bool) error {
	_, err := m.callTool(ctx, "set_anonymous", map[string]any{"anonymous": anonymous})
	return err
}

func (m *MCPGameClient) SetColors(ctx context.Context, primaryColor, secondaryColor string) error {
	_, err := m.callTool(ctx, "set_colors", map[string]any{
		"primary_color":   primaryColor,
		"secondary_color": secondaryColor,
	})
	return err
}

func (m *MCPGameClient) Facility(ctx context.Context, payload map[string]any) error {
	_, err := m.callTool(ctx, "facility", payload)
	return err
}

// now returns the current time. Declared as a var for testing.
var now = func() time.Time { return time.Now() }
