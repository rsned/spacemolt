package protocol

// Message types sent to/from the Spacemolt server
const (
	TypeWelcome       = "welcome"
	TypeRegistered    = "registered"
	TypeLoggedIn      = "logged_in"
	TypeError         = "error"
	TypeOK            = "ok"
	TypeDocked        = "docked"
	TypeUndocked      = "undocked"
	TypeStateUpdate   = "state_update"
	TypeChatMessage   = "chat_message"
	TypePOI           = "poi"
	TypeSystem        = "system"
	TypeMining        = "mining"
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
