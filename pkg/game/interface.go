package game

import (
	"context"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// GameClient defines the interface for game client operations
// This allows for mocking in tests
type GameClient interface {
	// Connection
	Connect(ctx context.Context) error
	Close() error
	IsConnected() bool
	Ready() <-chan struct{}

	// Authentication
	Login(ctx context.Context) error
	Register(ctx context.Context, empire, registrationCode string) error

	// Navigation
	Undock(ctx context.Context) error
	Dock(ctx context.Context) error
	Travel(ctx context.Context, targetPOI string) (*TravelResult, error)
	Jump(ctx context.Context, targetSystem string) (*JumpResult, error)

	// Mining & Scanning
	Mine(ctx context.Context) error
	Scan(ctx context.Context) error
	ScanTarget(ctx context.Context, targetID string) error

	// Combat
	Attack(ctx context.Context, targetID string) error
	Cloak(ctx context.Context, enable bool) error
	Battle(ctx context.Context, action string, payload map[string]any) error
	Reload(ctx context.Context, weaponInstanceID, ammoItemID string) error

	// Commerce
	Sell(ctx context.Context, itemID string, quantity float64) error
	SellAllBulk(ctx context.Context, reservedItems []string) error
	Buy(ctx context.Context, itemID string, quantity float64) error
	GetListings(ctx context.Context) error
	GetTrades(ctx context.Context) error
	EstimatePurchase(ctx context.Context, itemID string, quantity int) error
	CancelOrder(ctx context.Context, payload map[string]any) error
	ModifyOrder(ctx context.Context, payload map[string]any) error
	TradeOffer(ctx context.Context, targetID string, payload map[string]any) error
	TradeAccept(ctx context.Context, tradeID string) error
	TradeCancel(ctx context.Context, tradeID string) error
	TradeDecline(ctx context.Context, tradeID string) error

	// Crafting
	CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error
	GetRecipes(ctx context.Context) error

	// Ship Maintenance
	Refuel(ctx context.Context) error
	Repair(ctx context.Context) error
	RepairWith(ctx context.Context, payload map[string]any) error
	RefitShip(ctx context.Context) error
	InstallMod(ctx context.Context, moduleID string) error
	UninstallMod(ctx context.Context, moduleID string) error
	BuyShip(ctx context.Context, shipClass string) error
	BuyListedShip(ctx context.Context, listingID string) error
	BrowseShips(ctx context.Context, payload map[string]any) error
	BuyInsurance(ctx context.Context, ticks int) error
	ClaimInsurance(ctx context.Context) error
	ListShipForSale(ctx context.Context, shipID string, price float64) error
	CommissionQuote(ctx context.Context, shipClass string) error
	CommissionStatus(ctx context.Context, baseID string) error
	CancelCommission(ctx context.Context, commissionID string) error
	ClaimCommission(ctx context.Context, commissionID string) error
	CommissionShip(ctx context.Context, shipClass string, provideMaterials bool) error

	// Cargo & Storage
	DepositItems(ctx context.Context, itemID string, quantity float64) error
	DepositItemsPayload(ctx context.Context, payload map[string]any) error
	DepositAllItems(ctx context.Context) error
	WithdrawItems(ctx context.Context, itemID string, quantity float64) error
	WithdrawItemsPayload(ctx context.Context, payload map[string]any) error
	ViewStorage(ctx context.Context) error
	ViewStorageAt(ctx context.Context, stationID string) error

	// Cargo Operations
	Jettison(ctx context.Context, itemID string, quantity float64) error

	// Wrecks
	GetWrecks(ctx context.Context) error
	LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error
	SalvageWreck(ctx context.Context, wreckID string) error
	TowWreck(ctx context.Context, wreckID string) error
	UseItem(ctx context.Context, itemID string, quantity int) error

	// Queries
	GetSystem(ctx context.Context) error
	GetStatus(ctx context.Context) error
	GetNotifications(ctx context.Context) error
	GetShip(ctx context.Context) error
	GetCargo(ctx context.Context) error
	GetSkills(ctx context.Context) error
	GetPOI(ctx context.Context) error
	GetBase(ctx context.Context) error
	GetMap(ctx context.Context, force ...bool) error
	GetNearby(ctx context.Context) error
	GetSystemAgents(ctx context.Context) error
	GetVersion(ctx context.Context) error
	GetCommands(ctx context.Context) error
	GetActiveMissions(ctx context.Context) error
	GetInsuranceQuote(ctx context.Context) error
	Help(ctx context.Context, payload map[string]any) error

	// Data collection
	FactionInfo(ctx context.Context) error
	CaptainsLogList(ctx context.Context) error
	CaptainsLogGet(ctx context.Context, index int) error
	Catalog(ctx context.Context, catalogType string, page, pageSize int) error
	SearchSystems(ctx context.Context, query string) error
	GetGuide(ctx context.Context, guide string) error

	// Route Planning
	FindRoute(ctx context.Context, targetSystem string) ([]RouteStep, error)

	// Faction
	CreateFaction(ctx context.Context, payload map[string]any) error
	JoinFaction(ctx context.Context, factionID string) error
	LeaveFaction(ctx context.Context) error
	FactionInvite(ctx context.Context, playerID string) error
	FactionKick(ctx context.Context, playerID string) error
	FactionPromote(ctx context.Context, playerID, roleID string) error
	FactionList(ctx context.Context, limit, offset int) error
	FactionEdit(ctx context.Context, payload map[string]any) error
	FactionGetInvites(ctx context.Context) error
	FactionDeclineInvite(ctx context.Context, factionID string) error
	FactionDeclareWar(ctx context.Context, targetFactionID, reason string) error
	FactionProposePeace(ctx context.Context, targetFactionID, terms string) error
	FactionAcceptPeace(ctx context.Context, targetFactionID string) error
	FactionProposeAlly(ctx context.Context, targetFactionID string) error
	FactionAcceptAlly(ctx context.Context, targetFactionID string) error
	FactionRemoveAlly(ctx context.Context, targetFactionID string) error
	FactionSetEnemy(ctx context.Context, targetFactionID string) error
	FactionDepositCredits(ctx context.Context, amount float64) error
	FactionWithdrawCredits(ctx context.Context, amount float64) error
	FactionDepositItems(ctx context.Context, itemID string, quantity int) error
	FactionDepositItemsPayload(ctx context.Context, payload map[string]any) error
	FactionWithdrawItems(ctx context.Context, itemID string, quantity int) error
	FactionWithdrawItemsPayload(ctx context.Context, payload map[string]any) error
	ViewFactionStorage(ctx context.Context) error
	ViewFactionStorageAt(ctx context.Context, stationID string) error
	FactionCreateBuyOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error
	FactionCreateSellOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error
	FactionCreateRole(ctx context.Context, name string, priority int, permissions map[string]any) error
	FactionEditRole(ctx context.Context, roleID string, payload map[string]any) error
	FactionDeleteRole(ctx context.Context, roleID string) error
	FactionQueryIntel(ctx context.Context, payload map[string]any) error
	FactionQueryTradeIntel(ctx context.Context, payload map[string]any) error
	FactionIntelStatus(ctx context.Context) error
	FactionTradeIntelStatus(ctx context.Context) error
	FactionRooms(ctx context.Context) error
	FactionVisitRoom(ctx context.Context, roomID string) error
	FactionWriteRoom(ctx context.Context, payload map[string]any) error
	FactionDeleteRoom(ctx context.Context, roomID string) error
	FactionListMissions(ctx context.Context) error
	FactionCancelMission(ctx context.Context, templateID string) error

	// Fleet (v0.240)
	Fleet(ctx context.Context, action string, playerID string) error

	// Distress (v0.240)
	DistressSignal(ctx context.Context, distressType string) error

	// Communication
	Chat(ctx context.Context, channel, content string, targetID string) error
	GetChatHistory(ctx context.Context, channel string, payload map[string]any) error
	SetPlayerStatus(ctx context.Context, payload map[string]any) error
	SetHomeBase(ctx context.Context, baseID string) error
	SendGift(ctx context.Context, payload map[string]any) error
	SetAnonymous(ctx context.Context, anonymous bool) error
	SetColors(ctx context.Context, primaryColor, secondaryColor string) error

	// Forum
	ForumList(ctx context.Context, page int) error
	ForumCreateThread(ctx context.Context, title, content string, category string) error
	ForumGetThread(ctx context.Context, threadID string) error
	ForumReply(ctx context.Context, threadID, content string) error
	ForumUpvote(ctx context.Context, threadID string, replyID string) error
	ForumDeleteThread(ctx context.Context, threadID string) error
	ForumDeleteReply(ctx context.Context, replyID string) error

	// Notes
	CreateNote(ctx context.Context, title, content string) error
	WriteNote(ctx context.Context, noteID, content string) error
	ReadNote(ctx context.Context, noteID string) error
	GetNotes(ctx context.Context) error

	// State
	GetState() *State
	GetMarketListings() []MarketListing
	GetRawJSON(key string) []byte

	// Ship Management
	ListShips(ctx context.Context) error
	SwitchShip(ctx context.Context, shipID string) error
	SellShip(ctx context.Context, shipID string) error

	// Exchange
	CreateSellOrder(ctx context.Context, payload map[string]any) error
	CreateBuyOrder(ctx context.Context, payload map[string]any) error
	ViewMarket(ctx context.Context, payload map[string]any) error
	ViewOrders(ctx context.Context) error

	// Action Log
	GetActionLog(ctx context.Context, payload map[string]any) error

	// Missions
	GetMissions(ctx context.Context) error
	AcceptMission(ctx context.Context, missionID string) error
	CompleteMission(ctx context.Context, missionID string) error
	AbandonMission(ctx context.Context, missionID string) error
	DeclineMission(ctx context.Context, templateID string) error

	// Survey
	SurveySystem(ctx context.Context) error

	// Captain's Log
	CaptainsLogAdd(ctx context.Context, entry string) error

	// Station Facilities
	Facility(ctx context.Context, payload map[string]any) error

	// Generic passthrough for commands without explicit methods.
	RawCommand(ctx context.Context, command string, args map[string]any) error

	// Callbacks
	SetOnChatMessage(fn func(msg serverapi.ChatMessage))
}

// Ensure Client implements GameClient interface
var _ GameClient = (*Client)(nil)
