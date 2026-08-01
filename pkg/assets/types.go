package assets

// Identity is the three-way agent identity map. None of the three is derivable
// from the others:
//   - PlayerID is the server's stable hex id (Player.ID). Never changes.
//   - Username is the in-game display name. CAN change.
//   - AgentID is our local label (engineer-3) from data/agents and the fleet
//     YAMLs. Not a game concept at all.
//
// PlayerID is the primary key everywhere in this package.
type Identity struct {
	PlayerID string
	AgentID  string
	Username string
}
