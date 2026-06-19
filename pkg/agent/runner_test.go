package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// filterActions returns actions excluding the specified ones (e.g., internal refresh calls).
func filterActions(actions []string, exclude ...string) []string {
	excludeSet := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		excludeSet[e] = true
	}
	var filtered []string
	for _, a := range actions {
		if !excludeSet[a] {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

// Mock agent for testing
type mockAgent struct {
	id          string
	decisionFn  func(ctx context.Context, es EnrichedState) (Decision, error)
	learnFn     func(result ActionResult) error
	startCalled bool
	stopCalled  bool
}

func (m *mockAgent) ID() string               { return m.id }
func (m *mockAgent) Name() string             { return m.id }
func (m *mockAgent) Personality() Personality { return Personality{} }
func (m *mockAgent) Memory() Memory           { return nil }
func (m *mockAgent) Status() Status           { return Status{} }

func (m *mockAgent) Decide(ctx context.Context, es EnrichedState) (Decision, error) {
	if m.decisionFn != nil {
		return m.decisionFn(ctx, es)
	}
	return Decision{Action: "wait"}, nil
}

func (m *mockAgent) Learn(result ActionResult) error {
	if m.learnFn != nil {
		return m.learnFn(result)
	}
	return nil
}

func (m *mockAgent) Start(ctx context.Context) error {
	m.startCalled = true
	return nil
}

func (m *mockAgent) Stop() error {
	m.stopCalled = true
	return nil
}

// Tactical Action Queue methods (stubs for testing)
func (m *mockAgent) EnqueueActions(actions []PlannedAction)    {}
func (m *mockAgent) DequeueAction() (*PlannedAction, bool)     { return nil, false }
func (m *mockAgent) GetActionQueue() []PlannedAction           { return nil }
func (m *mockAgent) ClearActionQueue(reason string)            {}
func (m *mockAgent) SetUsingQueuedAction(using bool)           {}
func (m *mockAgent) IsUsingQueuedAction() bool                 { return false }
func (m *mockAgent) SetRouteHome(_ []game.RouteStep, _ string) {}
func (m *mockAgent) GetRouteHome() ([]game.RouteStep, string)  { return nil, "" }

// Mock game client for testing
type mockGameClient struct {
	state           *game.State
	actionsRecorded []string
	getSkillsErr    error
}

func newMockGameClient() *mockGameClient {
	return &mockGameClient{
		state: &game.State{
			CurrentTick: 100,
		},
		actionsRecorded: []string{},
	}
}

func (m *mockGameClient) GetState() *game.State {
	return m.state
}

// Connection methods
func (m *mockGameClient) Connect(ctx context.Context) error { return nil }
func (m *mockGameClient) Close() error                      { return nil }
func (m *mockGameClient) IsConnected() bool                 { return true }
func (m *mockGameClient) Ready() <-chan struct{}            { ch := make(chan struct{}); close(ch); return ch }
func (m *mockGameClient) Login(ctx context.Context) error   { return nil }
func (m *mockGameClient) Register(ctx context.Context, empire, registrationCode string) error {
	return nil
}

// Navigation
func (m *mockGameClient) Undock(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "undock")
	return nil
}
func (m *mockGameClient) Dock(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "dock")
	return nil
}
func (m *mockGameClient) Travel(ctx context.Context, targetPOI string) (*game.TravelResult, error) {
	m.actionsRecorded = append(m.actionsRecorded, "travel:"+targetPOI)
	return nil, nil
}
func (m *mockGameClient) Jump(ctx context.Context, targetSystem string) (*game.JumpResult, error) {
	m.actionsRecorded = append(m.actionsRecorded, "jump:"+targetSystem)
	return nil, nil
}

// Mining & Scanning
func (m *mockGameClient) Mine(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "mine")
	return nil
}
func (m *mockGameClient) Scan(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "scan")
	return nil
}

func (m *mockGameClient) ScanTarget(ctx context.Context, targetID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "scan_target:"+targetID)
	return nil
}

// Combat
func (m *mockGameClient) Attack(ctx context.Context, targetID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "attack:"+targetID)
	return nil
}
func (m *mockGameClient) Cloak(ctx context.Context, enable bool) error {
	m.actionsRecorded = append(m.actionsRecorded, "cloak")
	return nil
}

// Commerce
func (m *mockGameClient) Sell(ctx context.Context, itemID string, quantity float64) error {
	m.actionsRecorded = append(m.actionsRecorded, "sell:"+itemID)
	return nil
}
func (m *mockGameClient) SellAllBulk(ctx context.Context, reservedItems []string) error {
	m.actionsRecorded = append(m.actionsRecorded, "sell_all_bulk")
	return nil
}
func (m *mockGameClient) Buy(ctx context.Context, itemID string, quantity float64) error {
	m.actionsRecorded = append(m.actionsRecorded, "buy:"+itemID)
	return nil
}
func (m *mockGameClient) GetListings(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_listings")
	return nil
}
func (m *mockGameClient) GetTrades(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_trades")
	return nil
}

// Crafting
func (m *mockGameClient) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error {
	m.actionsRecorded = append(m.actionsRecorded, "craft:"+recipeID)
	return nil
}
func (m *mockGameClient) CraftWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	m.actionsRecorded = append(m.actionsRecorded, "craft:"+recipeID)
	return nil
}
func (m *mockGameClient) GetRecipes(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_recipes")
	return nil
}
func (m *mockGameClient) Recycle(ctx context.Context, recipeID string, quantity int) error {
	m.actionsRecorded = append(m.actionsRecorded, "recycle:"+recipeID)
	return nil
}
func (m *mockGameClient) RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	m.actionsRecorded = append(m.actionsRecorded, "recycle:"+recipeID)
	return nil
}

// Ship Maintenance
func (m *mockGameClient) Refuel(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "refuel")
	return nil
}
func (m *mockGameClient) Repair(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "repair")
	return nil
}
func (m *mockGameClient) RepairWith(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) Fleet(ctx context.Context, action string, playerID string) error {
	return nil
}
func (m *mockGameClient) DistressSignal(ctx context.Context, distressType string) error {
	return nil
}
func (m *mockGameClient) Install(ctx context.Context, itemID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "install:"+itemID)
	return nil
}
func (m *mockGameClient) RefitShip(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "refit_ship")
	return nil
}
func (m *mockGameClient) InstallMod(ctx context.Context, moduleID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "install_mod:"+moduleID)
	return nil
}
func (m *mockGameClient) UninstallMod(ctx context.Context, moduleID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "uninstall_mod:"+moduleID)
	return nil
}
func (m *mockGameClient) BuyShip(ctx context.Context, shipClass string) error {
	m.actionsRecorded = append(m.actionsRecorded, "buy_ship:"+shipClass)
	return nil
}
func (m *mockGameClient) BrowseShips(_ context.Context, _ map[string]any) error {
	return nil
}
func (m *mockGameClient) BuyInsurance(ctx context.Context, ticks int) error {
	m.actionsRecorded = append(m.actionsRecorded, "buy_insurance")
	return nil
}
func (m *mockGameClient) ClaimInsurance(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "claim_insurance")
	return nil
}

// Cargo & Storage
func (m *mockGameClient) DepositItems(ctx context.Context, itemID string, quantity float64) error {
	m.actionsRecorded = append(m.actionsRecorded, "deposit_items:"+itemID)
	return nil
}
func (m *mockGameClient) DepositAllItems(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "deposit_all_items")
	return nil
}
func (m *mockGameClient) WithdrawItems(ctx context.Context, itemID string, quantity float64) error {
	m.actionsRecorded = append(m.actionsRecorded, "withdraw_items:"+itemID)
	return nil
}
func (m *mockGameClient) DepositItemsPayload(ctx context.Context, payload map[string]any) error {
	m.actionsRecorded = append(m.actionsRecorded, "deposit_items_payload")
	return nil
}
func (m *mockGameClient) WithdrawItemsPayload(ctx context.Context, payload map[string]any) error {
	m.actionsRecorded = append(m.actionsRecorded, "withdraw_items_payload")
	return nil
}
func (m *mockGameClient) ViewStorage(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "view_storage")
	return nil
}
func (m *mockGameClient) ViewStorageAt(ctx context.Context, stationID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "view_storage_at:"+stationID)
	return nil
}

// Wrecks
func (m *mockGameClient) GetWrecks(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_wrecks")
	return nil
}
func (m *mockGameClient) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error {
	m.actionsRecorded = append(m.actionsRecorded, "loot_wreck:"+wreckID)
	return nil
}
func (m *mockGameClient) SalvageWreck(ctx context.Context, wreckID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "salvage_wreck:"+wreckID)
	return nil
}

// Queries
func (m *mockGameClient) GetStatus(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_status")
	return nil
}
func (m *mockGameClient) GetNotifications(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_notifications")
	return nil
}
func (m *mockGameClient) GetSystem(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_system")
	return nil
}
func (m *mockGameClient) GetShip(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_ship")
	return nil
}
func (m *mockGameClient) GetCargo(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_cargo")
	return nil
}
func (m *mockGameClient) GetSkills(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_skills")
	return m.getSkillsErr
}
func (m *mockGameClient) GetPOI(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_poi")
	return nil
}
func (m *mockGameClient) GetBase(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_base")
	return nil
}
func (m *mockGameClient) GetMap(ctx context.Context, force ...bool) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_map")
	return nil
}
func (m *mockGameClient) GetNearby(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_nearby")
	return nil
}
func (m *mockGameClient) GetSystemAgents(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_system_agents")
	return nil
}
func (m *mockGameClient) GetVersion(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_version")
	return nil
}
func (m *mockGameClient) GetCommands(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_commands")
	return nil
}
func (m *mockGameClient) GetActiveMissions(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_active_missions")
	return nil
}
func (m *mockGameClient) GetInsuranceQuote(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_insurance_quote")
	return nil
}
func (m *mockGameClient) Help(ctx context.Context, payload map[string]any) error {
	m.actionsRecorded = append(m.actionsRecorded, "help")
	return nil
}
func (m *mockGameClient) FactionInfo(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "faction_info")
	return nil
}
func (m *mockGameClient) CaptainsLogList(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "captains_log_list")
	return nil
}
func (m *mockGameClient) Catalog(ctx context.Context, catalogType string, page, pageSize int) error {
	m.actionsRecorded = append(m.actionsRecorded, "catalog")
	return nil
}

// Route Planning
func (m *mockGameClient) FindRoute(_ context.Context, targetSystem string) ([]game.RouteStep, error) {
	m.actionsRecorded = append(m.actionsRecorded, "find_route:"+targetSystem)
	return nil, nil
}

// Faction
func (m *mockGameClient) CreateFaction(ctx context.Context, payload map[string]any) error {
	m.actionsRecorded = append(m.actionsRecorded, "create_faction")
	return nil
}
func (m *mockGameClient) JoinFaction(ctx context.Context, factionID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "join_faction:"+factionID)
	return nil
}
func (m *mockGameClient) LeaveFaction(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "leave_faction")
	return nil
}
func (m *mockGameClient) FactionInvite(ctx context.Context, playerID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "faction_invite:"+playerID)
	return nil
}
func (m *mockGameClient) FactionKick(ctx context.Context, playerID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "faction_kick:"+playerID)
	return nil
}
func (m *mockGameClient) FactionPromote(ctx context.Context, playerID, roleID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "faction_promote:"+playerID)
	return nil
}

// Communication
func (m *mockGameClient) Chat(ctx context.Context, channel, content string, targetID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "chat:"+channel)
	return nil
}
func (m *mockGameClient) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) Jettison(ctx context.Context, itemID string, quantity float64) error {
	m.actionsRecorded = append(m.actionsRecorded, "jettison:"+itemID)
	return nil
}
func (m *mockGameClient) SetPlayerStatus(ctx context.Context, payload map[string]any) error {
	m.actionsRecorded = append(m.actionsRecorded, "set_status")
	return nil
}
func (m *mockGameClient) SetHomeBase(ctx context.Context, baseID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "set_home_base:"+baseID)
	return nil
}

// Forum
func (m *mockGameClient) ForumList(ctx context.Context, page int) error {
	m.actionsRecorded = append(m.actionsRecorded, "forum_list")
	return nil
}
func (m *mockGameClient) ForumCreateThread(ctx context.Context, title, content string, category string) error {
	m.actionsRecorded = append(m.actionsRecorded, "forum_create_thread")
	return nil
}
func (m *mockGameClient) ForumGetThread(ctx context.Context, threadID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "forum_get_thread:"+threadID)
	return nil
}
func (m *mockGameClient) ForumReply(ctx context.Context, threadID, content string) error {
	m.actionsRecorded = append(m.actionsRecorded, "forum_reply:"+threadID)
	return nil
}
func (m *mockGameClient) ForumUpvote(ctx context.Context, threadID string, replyID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "forum_upvote:"+threadID)
	return nil
}
func (m *mockGameClient) ForumDeleteThread(ctx context.Context, threadID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "forum_delete_thread:"+threadID)
	return nil
}
func (m *mockGameClient) ForumDeleteReply(ctx context.Context, replyID string) error {
	m.actionsRecorded = append(m.actionsRecorded, "forum_delete_reply:"+replyID)
	return nil
}

// Notes
func (m *mockGameClient) CreateNote(ctx context.Context, title, content string) error {
	m.actionsRecorded = append(m.actionsRecorded, "create_note")
	return nil
}
func (m *mockGameClient) WriteNote(ctx context.Context, noteID, content string) error {
	m.actionsRecorded = append(m.actionsRecorded, "write_note:"+noteID)
	return nil
}
func (m *mockGameClient) GetNotes(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_notes")
	return nil
}

// Market & Ships
func (m *mockGameClient) GetMarketListings() []game.MarketListing             { return nil }
func (m *mockGameClient) GetRawJSON(key string) []byte                        { return nil }
func (m *mockGameClient) ListShips(ctx context.Context) error                 { return nil }
func (m *mockGameClient) SwitchShip(ctx context.Context, shipID string) error { return nil }
func (m *mockGameClient) SellShip(ctx context.Context, shipID string) error   { return nil }
func (m *mockGameClient) CreateSellOrder(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) CreateBuyOrder(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) ViewMarket(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) ViewOrders(ctx context.Context) error                         { return nil }

// Action Log
func (m *mockGameClient) GetActionLog(ctx context.Context, payload map[string]any) error { return nil }

// Missions
func (m *mockGameClient) GetMissions(ctx context.Context) error                       { return nil }
func (m *mockGameClient) AcceptMission(ctx context.Context, missionID string) error   { return nil }
func (m *mockGameClient) CompleteMission(ctx context.Context, missionID string) error { return nil }
func (m *mockGameClient) AbandonMission(ctx context.Context, missionID string) error  { return nil }
func (m *mockGameClient) DeclineMission(ctx context.Context, templateID string) error { return nil }

// Survey & Log
func (m *mockGameClient) SurveySystem(ctx context.Context) error                 { return nil }
func (m *mockGameClient) CaptainsLogAdd(ctx context.Context, entry string) error { return nil }
func (m *mockGameClient) CaptainsLogGet(ctx context.Context, index int) error    { return nil }
func (m *mockGameClient) RawCommand(_ context.Context, _ string, _ map[string]any) error {
	return nil
}

// Combat
func (m *mockGameClient) Battle(ctx context.Context, action string, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) GetBattleStatus(_ context.Context) error { return nil }
func (m *mockGameClient) Reload(ctx context.Context, weaponInstanceID, ammoItemID string) error {
	return nil
}

// Commerce extras
func (m *mockGameClient) EstimatePurchase(ctx context.Context, itemID string, quantity int) error {
	return nil
}
func (m *mockGameClient) CancelOrder(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) ModifyOrder(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) TradeOffer(ctx context.Context, targetID string, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) TradeAccept(ctx context.Context, tradeID string) error  { return nil }
func (m *mockGameClient) TradeCancel(ctx context.Context, tradeID string) error  { return nil }
func (m *mockGameClient) TradeDecline(ctx context.Context, tradeID string) error { return nil }

// Ship Management extras
func (m *mockGameClient) BuyListedShip(ctx context.Context, listingID string) error { return nil }
func (m *mockGameClient) ListShipForSale(ctx context.Context, shipID string, price float64) error {
	return nil
}
func (m *mockGameClient) CommissionQuote(ctx context.Context, shipClass string) error     { return nil }
func (m *mockGameClient) CommissionStatus(ctx context.Context, baseID string) error       { return nil }
func (m *mockGameClient) CancelCommission(ctx context.Context, commissionID string) error { return nil }
func (m *mockGameClient) ClaimCommission(ctx context.Context, commissionID string) error  { return nil }
func (m *mockGameClient) CommissionShip(ctx context.Context, shipClass string, provideMaterials bool) error {
	return nil
}

// Wrecks extras
func (m *mockGameClient) TowWreck(ctx context.Context, wreckID string) error             { return nil }
func (m *mockGameClient) UseItem(ctx context.Context, itemID string, quantity int) error { return nil }

// Data collection extras
func (m *mockGameClient) SearchSystems(ctx context.Context, query string) error { return nil }
func (m *mockGameClient) GetGuide(ctx context.Context, guide string) error      { return nil }

// Faction extras
func (m *mockGameClient) FactionList(ctx context.Context, limit, offset int) error      { return nil }
func (m *mockGameClient) FactionEdit(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) FactionGetInvites(ctx context.Context) error                   { return nil }
func (m *mockGameClient) FactionDeclineInvite(ctx context.Context, factionID string) error {
	return nil
}
func (m *mockGameClient) FactionDeclareWar(ctx context.Context, targetFactionID, reason string) error {
	return nil
}
func (m *mockGameClient) FactionProposePeace(ctx context.Context, targetFactionID, terms string) error {
	return nil
}
func (m *mockGameClient) FactionAcceptPeace(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionProposeAlly(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionAcceptAlly(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionRemoveAlly(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionSetEnemy(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionDepositCredits(ctx context.Context, amount float64) error { return nil }
func (m *mockGameClient) FactionWithdrawCredits(ctx context.Context, amount float64) error {
	return nil
}
func (m *mockGameClient) FactionDepositItems(ctx context.Context, itemID string, quantity int) error {
	return nil
}
func (m *mockGameClient) FactionDepositItemsPayload(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionWithdrawItems(ctx context.Context, itemID string, quantity int) error {
	return nil
}
func (m *mockGameClient) FactionWithdrawItemsPayload(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) ViewFactionStorage(ctx context.Context) error { return nil }
func (m *mockGameClient) ViewFactionStorageAt(ctx context.Context, stationID string) error {
	return nil
}
func (m *mockGameClient) FactionCreateBuyOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	return nil
}
func (m *mockGameClient) FactionCreateSellOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	return nil
}
func (m *mockGameClient) FactionCreateRole(ctx context.Context, name string, priority int, permissions map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionEditRole(ctx context.Context, roleID string, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionDeleteRole(ctx context.Context, roleID string) error { return nil }
func (m *mockGameClient) FactionSubmitIntel(ctx context.Context, systems []map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionQueryIntel(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionQueryTradeIntel(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionIntelStatus(ctx context.Context) error              { return nil }
func (m *mockGameClient) FactionTradeIntelStatus(ctx context.Context) error         { return nil }
func (m *mockGameClient) FactionRooms(ctx context.Context) error                    { return nil }
func (m *mockGameClient) FactionVisitRoom(ctx context.Context, roomID string) error { return nil }
func (m *mockGameClient) FactionWriteRoom(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionDeleteRoom(ctx context.Context, roomID string) error { return nil }
func (m *mockGameClient) FactionListMissions(ctx context.Context) error              { return nil }
func (m *mockGameClient) FactionCancelMission(ctx context.Context, templateID string) error {
	return nil
}

// Communication extras
func (m *mockGameClient) SendGift(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) SetAnonymous(ctx context.Context, anonymous bool) error     { return nil }
func (m *mockGameClient) SetColors(ctx context.Context, primaryColor, secondaryColor string) error {
	return nil
}

// Notes extras
func (m *mockGameClient) ReadNote(ctx context.Context, noteID string) error { return nil }

// Station Facilities
func (m *mockGameClient) Facility(ctx context.Context, payload map[string]any) error { return nil }

func (m *mockGameClient) SetOnChatMessage(fn func(msg serverapi.ChatMessage)) {}

// Phase 1 API-currentness actions (drones, empire/citizenship, economy, logs)
func (m *mockGameClient) GetDrone(ctx context.Context, droneID string) error    { return nil }
func (m *mockGameClient) GetDrones(ctx context.Context) error                   { return nil }
func (m *mockGameClient) LoadDrone(ctx context.Context, itemID string) error    { return nil }
func (m *mockGameClient) UnloadDrone(ctx context.Context, droneID string) error { return nil }
func (m *mockGameClient) RecallDrone(ctx context.Context, droneID string, all bool) error {
	return nil
}
func (m *mockGameClient) UploadDroneScript(ctx context.Context, droneID, script string) error {
	return nil
}
func (m *mockGameClient) DeployDrone(ctx context.Context, droneID string, all bool) error {
	return nil
}
func (m *mockGameClient) SetDroneName(ctx context.Context, droneID, name string) error {
	return nil
}
func (m *mockGameClient) FactionAcceptInvite(ctx context.Context, factionID string) error {
	return nil
}
func (m *mockGameClient) FactionWithdrawInvite(ctx context.Context, playerID string) error {
	return nil
}
func (m *mockGameClient) FactionRemoveEnemy(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) Citizenship(ctx context.Context, action, empireID string) error { return nil }
func (m *mockGameClient) GetEmpireInfo(ctx context.Context, empireID string) error       { return nil }
func (m *mockGameClient) Petition(ctx context.Context, empireID, message string) error   { return nil }
func (m *mockGameClient) GetTaxEstimate(ctx context.Context) error                       { return nil }
func (m *mockGameClient) ViewInsurance(ctx context.Context) error                        { return nil }
func (m *mockGameClient) ScrapShip(ctx context.Context, shipID string) error             { return nil }
func (m *mockGameClient) CompletedMissions(ctx context.Context) error                    { return nil }
func (m *mockGameClient) DeleteNote(ctx context.Context, noteID string) error            { return nil }
func (m *mockGameClient) CaptainsLogDelete(ctx context.Context, index int) error         { return nil }
func (m *mockGameClient) AgentLogs(ctx context.Context, category, severity, message string, data map[string]any) error {
	return nil
}

func TestRunner_StartAndStop(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()

	config := DefaultRunnerConfig()
	config.DecisionInterval = 100 * time.Millisecond

	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Start runner
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}

	if !agent.startCalled {
		t.Error("Agent.Start() was not called")
	}

	if !runner.IsRunning() {
		t.Error("Runner should be running")
	}

	// Stop runner
	if err := runner.Stop(); err != nil {
		t.Fatalf("Failed to stop runner: %v", err)
	}

	if !agent.stopCalled {
		t.Error("Agent.Stop() was not called")
	}

	// Wait a moment for goroutine to exit
	time.Sleep(200 * time.Millisecond)

	if runner.IsRunning() {
		t.Error("Runner should not be running after stop")
	}
}

func TestRunner_ExecuteCycle_ActionCommand(t *testing.T) {
	decisionCount := 0
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			decisionCount++
			return Decision{
				Action:     "mine",
				Reasoning:  "Mining resources",
				Confidence: 0.9,
			}, nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute one cycle
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	if decisionCount != 1 {
		t.Errorf("Expected 1 decision, got %d", decisionCount)
	}

	if len(client.actionsRecorded) != 1 {
		t.Fatalf("Expected 1 action recorded, got %d", len(client.actionsRecorded))
	}

	if client.actionsRecorded[0] != "mine" {
		t.Errorf("Expected 'mine' action, got %s", client.actionsRecorded[0])
	}

	// Check that last action tick was updated
	if runner.GetLastActionTick() != 100 {
		t.Errorf("Expected lastActionTick=100, got %d", runner.GetLastActionTick())
	}
}

func TestRunner_ExecuteCycle_QueryCommand(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			return Decision{
				Action:     "get_status",
				Reasoning:  "Checking status",
				Confidence: 1.0,
			}, nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute cycle
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	if len(client.actionsRecorded) != 1 {
		t.Fatalf("Expected 1 action recorded, got %d", len(client.actionsRecorded))
	}

	if client.actionsRecorded[0] != "get_status" {
		t.Errorf("Expected 'get_status' action, got %s", client.actionsRecorded[0])
	}

	// Check that last action tick was NOT updated (query commands don't consume tick)
	if runner.GetLastActionTick() != 0 {
		t.Errorf("Expected lastActionTick=0 for query command, got %d", runner.GetLastActionTick())
	}
}

func TestRunner_ThrottleActionCommands(t *testing.T) {
	callCount := 0
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			callCount++
			return Decision{
				Action:     "mine",
				Reasoning:  "Mining",
				Confidence: 0.9,
			}, nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)
	runner.lastActionTick = 100        // Already acted this tick
	runner.lastActionTime = time.Now() // Recent action time prevents time-based fallback

	ctx := context.Background()

	// Execute cycle - should skip entirely (no LLM call, no action)
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	// Agent should NOT be called (skip cycle to save LLM calls)
	if callCount != 0 {
		t.Errorf("Expected agent not to be called when throttled, got %d calls", callCount)
	}

	// No game actions executed (get_notifications is internal tick refresh, not a game action)
	gameActions := filterActions(client.actionsRecorded, "get_notifications")
	if len(gameActions) != 0 {
		t.Errorf("Expected 0 game actions (throttled), got %d: %v", len(gameActions), gameActions)
	}

	// Now advance tick and try again
	client.state.CurrentTick = 101
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	// Now agent should be called and action should execute
	if callCount != 1 {
		t.Errorf("Expected 1 call after tick advance, got %d", callCount)
	}
	gameActions = filterActions(client.actionsRecorded, "get_notifications")
	if len(gameActions) != 1 {
		t.Errorf("Expected 1 game action after tick advance, got %d: %v", len(gameActions), gameActions)
	}
}

func TestRunner_DecisionError_Retries(t *testing.T) {
	var errorCount atomic.Int32
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			count := errorCount.Add(1)
			if count < 3 {
				return Decision{}, errors.New("temporary error")
			}
			return Decision{Action: "wait"}, nil
		},
	}

	client := newMockGameClient()
	config := DefaultRunnerConfig()
	config.MaxRetries = 5
	config.DecisionInterval = 50 * time.Millisecond

	runner := NewRunner(agent, client, config)

	ctx := context.Background()
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}
	defer func() { _ = runner.Stop() }()

	// Wait for several cycles
	time.Sleep(300 * time.Millisecond)

	// Should have retried and eventually succeeded
	if errorCount.Load() < 3 {
		t.Errorf("Expected at least 3 decision calls, got %d", errorCount.Load())
	}

	if runner.HasCrashed() {
		t.Error("Runner should not have crashed after recovering")
	}
}

func TestRunner_MaxRetries_Stops(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			return Decision{}, errors.New("persistent error")
		},
	}

	client := newMockGameClient()
	config := DefaultRunnerConfig()
	config.MaxRetries = 3
	config.DecisionInterval = 50 * time.Millisecond

	runner := NewRunner(agent, client, config)

	ctx := context.Background()
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}

	// Wait for runner to crash
	time.Sleep(300 * time.Millisecond)

	if !runner.HasCrashed() {
		t.Error("Runner should have crashed after max retries")
	}

	if runner.IsRunning() {
		t.Error("Runner should have stopped after crashing")
	}
}

func TestRunner_ContextCancellation(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()

	config := DefaultRunnerConfig()
	config.DecisionInterval = 50 * time.Millisecond

	runner := NewRunner(agent, client, config)

	ctx, cancel := context.WithCancel(context.Background())

	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}

	// Cancel context
	cancel()

	// Wait for runner to stop
	time.Sleep(200 * time.Millisecond)

	if runner.IsRunning() {
		t.Error("Runner should have stopped after context cancellation")
	}
}

func TestRunner_WaitAction(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			return Decision{
				Action:     "wait",
				Reasoning:  "Waiting for next tick",
				Confidence: 1.0,
			}, nil
		},
	}

	client := newMockGameClient()
	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute cycle with wait action
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	// No actions should be recorded for "wait"
	if len(client.actionsRecorded) != 0 {
		t.Errorf("Expected 0 actions for 'wait', got %d", len(client.actionsRecorded))
	}
}

func TestRunner_InvalidAction(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			return Decision{
				Action:     "invalid_action",
				Confidence: 1.0,
			}, nil
		},
	}

	client := newMockGameClient()
	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute cycle with invalid action
	err := runner.executeCycle(ctx)
	if err == nil {
		t.Error("Expected error for invalid action")
	}

	if len(client.actionsRecorded) != 0 {
		t.Errorf("Expected 0 actions for invalid action, got %d", len(client.actionsRecorded))
	}
}

func TestRunner_LearningCallback(t *testing.T) {
	learnCount := 0
	var lastResult ActionResult

	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			return Decision{Action: "mine", Confidence: 0.8}, nil
		},
		learnFn: func(result ActionResult) error {
			learnCount++
			lastResult = result
			return nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute cycle
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	if learnCount != 1 {
		t.Errorf("Expected Learn to be called once, got %d", learnCount)
	}

	if !lastResult.Success {
		t.Error("Expected successful result")
	}

	if lastResult.Error != nil {
		t.Errorf("Expected no error in result, got %v", lastResult.Error)
	}
}

func TestRunner_RecycleDispatch(t *testing.T) {
	t.Run("dispatches recycle decision", func(t *testing.T) {
		agent := &mockAgent{
			id: "test-agent",
			decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
				return Decision{
					Action:     "recycle",
					Target:     "recipe_iron_plate",
					Confidence: 0.9,
				}, nil
			},
		}

		client := newMockGameClient()
		client.state.CurrentTick = 100

		config := DefaultRunnerConfig()
		runner := NewRunner(agent, client, config)

		ctx := context.Background()

		if err := runner.executeCycle(ctx); err != nil {
			t.Fatalf("executeCycle failed: %v", err)
		}

		recorded := filterActions(client.actionsRecorded)
		if len(recorded) != 1 {
			t.Fatalf("Expected 1 action recorded, got %d: %v", len(recorded), recorded)
		}
		if recorded[0] != "recycle:recipe_iron_plate" {
			t.Errorf("Expected 'recycle:recipe_iron_plate', got %s", recorded[0])
		}
	})

	t.Run("returns error for empty target", func(t *testing.T) {
		agent := &mockAgent{
			id: "test-agent",
			decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
				return Decision{
					Action:     "recycle",
					Target:     "",
					Confidence: 0.9,
				}, nil
			},
		}

		client := newMockGameClient()
		config := DefaultRunnerConfig()
		runner := NewRunner(agent, client, config)

		ctx := context.Background()

		if err := runner.executeCycle(ctx); err == nil {
			t.Error("Expected error for empty recycle target")
		}

		if len(client.actionsRecorded) != 0 {
			t.Errorf("Expected 0 actions recorded for empty target, got %d", len(client.actionsRecorded))
		}
	})
}

func TestIsActionCommand(t *testing.T) {
	tests := []struct {
		action   string
		expected bool
	}{
		// Action commands (consume tick)
		{"mine", true},
		{"travel", true},
		{"jump", true},
		{"dock", true},
		{"undock", true},
		{"attack", true},
		{"scan", true},
		{"craft", true},

		// Query commands (do not consume tick)
		{"get_status", false},
		{"get_system", false},
		{"get_ship", false},
		{"get_skills", false},
		{"get_map", false},
		{"help", false},
		{"wait", false},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			result := isActionCommand(tt.action)
			if result != tt.expected {
				t.Errorf("isActionCommand(%q) = %v, want %v", tt.action, result, tt.expected)
			}
		})
	}
}

func TestRunner_DoubleStart(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Start once
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("First start failed: %v", err)
	}
	defer func() { _ = runner.Stop() }()

	// Try to start again
	err := runner.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already running runner")
	}
}

func TestRunner_GettersSetters(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	// Test getters
	if runner.GetAgent() != agent {
		t.Error("GetAgent() returned wrong agent")
	}

	if runner.GetGameClient() != client {
		t.Error("GetGameClient() returned wrong client")
	}

	if runner.GetLastActionTick() != 0 {
		t.Errorf("Expected initial lastActionTick=0, got %d", runner.GetLastActionTick())
	}

	if runner.GetCrashCount() != 0 {
		t.Errorf("Expected initial crashCount=0, got %d", runner.GetCrashCount())
	}

	// Update values
	runner.mu.Lock()
	runner.lastActionTick = 123
	runner.crashCount = 5
	runner.mu.Unlock()

	if runner.GetLastActionTick() != 123 {
		t.Errorf("Expected lastActionTick=123, got %d", runner.GetLastActionTick())
	}

	if runner.GetCrashCount() != 5 {
		t.Errorf("Expected crashCount=5, got %d", runner.GetCrashCount())
	}
}

func TestRunner_PassiveSkillCheck_FiresOncePerInterval(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			return Decision{
				Action:     "wait",
				Reasoning:  "idle",
				Confidence: 1.0,
			}, nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)
	ctx := context.Background()

	// First cycle: not yet due — runner was just created, lastPassiveSkillCheck
	// is "now", so no get_skills should fire.
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("first executeCycle: %v", err)
	}
	for _, a := range client.actionsRecorded {
		if a == "get_skills" {
			t.Fatalf("get_skills fired prematurely on first cycle: %v", client.actionsRecorded)
		}
	}

	// Force the timer into the past so the next cycle is "due".
	runner.mu.Lock()
	runner.lastPassiveSkillCheck = time.Now().Add(-2 * game.PassiveSkillCheckInterval)
	runner.mu.Unlock()

	// Advance the game tick so the runner is allowed to act this cycle.
	client.state.CurrentTick = 200

	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("second executeCycle: %v", err)
	}

	skillCalls := 0
	for _, a := range client.actionsRecorded {
		if a == "get_skills" {
			skillCalls++
		}
	}
	if skillCalls != 1 {
		t.Errorf("expected 1 get_skills call after interval elapsed, got %d (recorded: %v)",
			skillCalls, client.actionsRecorded)
	}

	// Third cycle: not due again because the previous cycle reset the timer.
	client.state.CurrentTick = 300
	before := len(client.actionsRecorded)
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("third executeCycle: %v", err)
	}
	for _, a := range client.actionsRecorded[before:] {
		if a == "get_skills" {
			t.Fatalf("get_skills fired again before next interval (recorded since prev: %v)",
				client.actionsRecorded[before:])
		}
	}
}

func TestRunner_PassiveSkillCheck_SkippedWhilePaused(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()
	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	// Force the timer into the past, then pause.
	runner.mu.Lock()
	runner.lastPassiveSkillCheck = time.Now().Add(-2 * game.PassiveSkillCheckInterval)
	runner.paused = true
	runner.mu.Unlock()

	if err := runner.executeCycle(context.Background()); err != nil {
		t.Fatalf("executeCycle: %v", err)
	}
	for _, a := range client.actionsRecorded {
		if a == "get_skills" {
			t.Fatalf("get_skills fired while paused: %v", client.actionsRecorded)
		}
	}
}

func TestRunner_PassiveSkillCheck_TimestampUpdatedOnError(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, es EnrichedState) (Decision, error) {
			return Decision{Action: "wait", Reasoning: "idle", Confidence: 1.0}, nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100
	client.getSkillsErr = errors.New("simulated transient server error")

	runner := NewRunner(agent, client, DefaultRunnerConfig())

	// Force the timer into the past so the first cycle is "due".
	runner.mu.Lock()
	runner.lastPassiveSkillCheck = time.Now().Add(-2 * game.PassiveSkillCheckInterval)
	runner.mu.Unlock()

	if err := runner.executeCycle(context.Background()); err != nil {
		t.Fatalf("first executeCycle: %v", err)
	}

	// GetSkills should have been called exactly once despite returning an error.
	skillCalls := 0
	for _, a := range client.actionsRecorded {
		if a == "get_skills" {
			skillCalls++
		}
	}
	if skillCalls != 1 {
		t.Fatalf("expected 1 get_skills call on first cycle, got %d (recorded: %v)",
			skillCalls, client.actionsRecorded)
	}

	// The timestamp must have been updated even though GetSkills errored, so
	// the immediate next cycle must NOT re-issue the call (no retry storm).
	client.state.CurrentTick = 200
	before := len(client.actionsRecorded)
	if err := runner.executeCycle(context.Background()); err != nil {
		t.Fatalf("second executeCycle: %v", err)
	}
	for _, a := range client.actionsRecorded[before:] {
		if a == "get_skills" {
			t.Fatalf("get_skills re-issued immediately after errored call (recorded since prev: %v)",
				client.actionsRecorded[before:])
		}
	}
}
