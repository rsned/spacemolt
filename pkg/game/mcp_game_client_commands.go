package game

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// --- Navigation ---

func (m *MCPGameClient) Undock(ctx context.Context) error {
	result, err := m.callTool(ctx, "undock", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) Dock(ctx context.Context) error {
	result, err := m.callTool(ctx, "dock", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) Travel(ctx context.Context, targetPOI string) (*TravelResult, error) {
	result, err := m.callTool(ctx, "travel", map[string]any{
		"target_poi": targetPOI,
	})
	if err != nil {
		return nil, err
	}
	if err := m.updateStateFromResult(result); err != nil {
		return nil, err
	}
	state := m.GetState()
	return &TravelResult{
		POI:     state.CurrentPOI,
		Canceled: false,
	}, nil
}

func (m *MCPGameClient) Jump(ctx context.Context, targetSystem string) (*JumpResult, error) {
	result, err := m.callTool(ctx, "jump", map[string]any{
		"target_system": targetSystem,
	})
	if err != nil {
		return nil, err
	}
	if err := m.updateStateFromResult(result); err != nil {
		return nil, err
	}
	state := m.GetState()
	return &JumpResult{
		SystemID:   state.CurrentSystem,
		SystemName: state.System.Name,
		POI:        state.CurrentPOI,
		Canceled:   false,
	}, nil
}

// --- Mining & Scanning ---

func (m *MCPGameClient) Mine(ctx context.Context) error {
	result, err := m.callTool(ctx, "mine", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) Scan(ctx context.Context) error {
	result, err := m.callTool(ctx, "scan", nil)
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
	// Parse listings from the result.
	text, parseErr := parseToolResultText(result)
	if parseErr == nil {
		var resp struct {
			Listings []MarketListing `json:"listings"`
		}
		if jsonErr := json.Unmarshal([]byte(text), &resp); jsonErr == nil && len(resp.Listings) > 0 {
			m.listingsMu.Lock()
			m.latestListings = resp.Listings
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

func (m *MCPGameClient) Install(ctx context.Context, itemID string) error {
	result, err := m.callTool(ctx, "install_mod", map[string]any{
		"module_id": itemID,
	})
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
	return m.updateStateFromResult(result)
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
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetStatus(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_status", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetShip(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_ship", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetSkills(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_skills", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetPOI(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_poi", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetBase(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_base", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetMap(ctx context.Context, force ...bool) error {
	result, err := m.callTool(ctx, "get_map", nil)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.state.LastMapUpdate = now()
	m.mu.Unlock()
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetNearby(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_nearby", nil)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

func (m *MCPGameClient) GetVersion(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_version", nil)
	if err != nil {
		return err
	}
	// Parse version from result.
	text, err := parseToolResultText(result)
	if err != nil {
		return err
	}
	var vResp struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(text), &vResp); err == nil && vResp.Version != "" {
		m.mu.Lock()
		m.state.ServerVersion = vResp.Version
		m.mu.Unlock()
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
	_, err := m.callTool(ctx, "get_notes", nil)
	return err
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

func (m *MCPGameClient) ViewMarket(ctx context.Context, itemID string) error {
	args := map[string]any{}
	if itemID != "" {
		args["item_id"] = itemID
	}
	_, err := m.callTool(ctx, "view_market", args)
	return err
}

func (m *MCPGameClient) ViewOrders(ctx context.Context) error {
	_, err := m.callTool(ctx, "view_orders", nil)
	return err
}

// Missions

func (m *MCPGameClient) GetMissions(ctx context.Context) error {
	_, err := m.callTool(ctx, "get_missions", nil)
	return err
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

// now returns the current time. Declared as a var for testing.
var now = func() time.Time { return time.Now() }
