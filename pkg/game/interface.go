package game

import "context"

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

	// Combat
	Attack(ctx context.Context, targetID string) error
	Cloak(ctx context.Context, enable bool) error

	// Commerce
	Sell(ctx context.Context, itemID string, quantity float64) error
	SellAllBulk(ctx context.Context, reservedItems []string) error
	Buy(ctx context.Context, itemID string, quantity float64) error
	GetListings(ctx context.Context) error
	GetTrades(ctx context.Context) error

	// Crafting
	CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error
	GetRecipes(ctx context.Context) error

	// Ship Maintenance
	Refuel(ctx context.Context) error
	Repair(ctx context.Context) error
	RepairWith(ctx context.Context, payload map[string]any) error
	Install(ctx context.Context, itemID string) error
	UninstallMod(ctx context.Context, moduleID string) error
	BuyShip(ctx context.Context, shipClass string) error
	BrowseShips(ctx context.Context, payload map[string]any) error
	BuyInsurance(ctx context.Context, ticks int) error
	ClaimInsurance(ctx context.Context) error

	// Cargo & Storage
	DepositItems(ctx context.Context, itemID string, quantity float64) error
	DepositAllItems(ctx context.Context) error
	WithdrawItems(ctx context.Context, itemID string, quantity float64) error
	ViewStorage(ctx context.Context) error
	ViewStorageAt(ctx context.Context, stationID string) error

	// Cargo Operations
	Jettison(ctx context.Context, itemID string, quantity float64) error

	// Wrecks
	GetWrecks(ctx context.Context) error
	LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error
	SalvageWreck(ctx context.Context, wreckID string) error

	// Queries
	GetSystem(ctx context.Context) error
	GetStatus(ctx context.Context) error
	GetShip(ctx context.Context) error
	GetCargo(ctx context.Context) error
	GetSkills(ctx context.Context) error
	GetPOI(ctx context.Context) error
	GetBase(ctx context.Context) error
	GetMap(ctx context.Context, force ...bool) error
	GetNearby(ctx context.Context) error
	GetVersion(ctx context.Context) error
	GetDrones(ctx context.Context) error
	GetCommands(ctx context.Context) error
	GetActiveMissions(ctx context.Context) error
	GetInsuranceQuote(ctx context.Context) error
	Help(ctx context.Context, payload map[string]any) error

	// Data collection
	FactionInfo(ctx context.Context) error
	CaptainsLogList(ctx context.Context) error
	ShipyardShowroom(ctx context.Context) error
	Catalog(ctx context.Context, catalogType string, page, pageSize int) error

	// Route Planning
	FindRoute(ctx context.Context, targetSystem string) ([]RouteStep, error)

	// Faction
	CreateFaction(ctx context.Context, payload map[string]any) error
	JoinFaction(ctx context.Context, factionID string) error
	LeaveFaction(ctx context.Context) error
	FactionInvite(ctx context.Context, playerID string) error
	FactionKick(ctx context.Context, playerID string) error
	FactionPromote(ctx context.Context, playerID, roleID string) error

	// Fleet (v0.240)
	Fleet(ctx context.Context, action string, playerID string) error

	// Distress (v0.240)
	DistressSignal(ctx context.Context, distressType string) error

	// Communication
	Chat(ctx context.Context, channel, content string, targetID string) error
	GetChatHistory(ctx context.Context, channel string, payload map[string]any) error
	SetPlayerStatus(ctx context.Context, payload map[string]any) error
	SetHomeBase(ctx context.Context, baseID string) error

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
	ViewMarket(ctx context.Context, itemID string) error
	ViewOrders(ctx context.Context) error

	// Missions
	GetMissions(ctx context.Context) error
	AcceptMission(ctx context.Context, missionID string) error

	// Survey
	SurveySystem(ctx context.Context) error

	// Captain's Log
	CaptainsLogAdd(ctx context.Context, entry string) error

	// Generic passthrough for commands without explicit methods.
	RawCommand(ctx context.Context, command string, args map[string]any) error
}

// Ensure Client implements GameClient interface
var _ GameClient = (*Client)(nil)
