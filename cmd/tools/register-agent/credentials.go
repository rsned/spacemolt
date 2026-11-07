package main

import (
	"encoding/json"

	"github.com/rsned/spacemolt/pkg/game"
)

// credentialsJSON renders a registration in the data/agents/<id>/credentials.json
// format, so the output can be saved straight into an agent directory.
func credentialsJSON(username, password, empire, playerID string) ([]byte, error) {
	return json.MarshalIndent(game.Credentials{
		Username: username,
		Password: password,
		Empire:   empire,
		PlayerID: playerID,
	}, "", "  ")
}
