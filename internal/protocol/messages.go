package protocol

// Message types sent to/from the Spacemolt server
const (
	// Connection and Authentication
	TypeWelcome    = "welcome"
	TypeRegistered = "registered"
	TypeLoggedIn   = "logged_in"

	// Action responses
	TypeOK          = "ok"
	TypeError       = "error"
	TypeDocked      = "docked"
	TypeUndocked    = "undocked"
	TypeStateUpdate = "state_update"
	TypeTick        = "tick"
	TypeListings    = "listings"

	// Game events
	TypeChatMessage        = "chat_message"
	TypeCombatUpdate       = "combat_update"
	TypePlayerDied         = "player_died"
	TypeMining             = "mining"
	TypeMiningYield        = "mining_yield"
	TypeScanResult         = "scan_result"
	TypeTradeOfferReceived = "trade_offer_received"
)

// Message represents a message sent to the server
type Message struct {
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
}

// Response represents a message received from the server
type Response struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}
