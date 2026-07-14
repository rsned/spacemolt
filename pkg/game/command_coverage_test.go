package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ignoredCommands are server commands the client intentionally does not support.
// Every entry MUST carry a justification; adding one is a deliberate, reviewed act.
var ignoredCommands = map[string]string{
	// v2 API — client is on v1 (project_v2_api_migration).
	"v2_get_player":   "v2 API; client on v1 (project_v2_api_migration)",
	"v2_get_ship":     "v2 API; client on v1 (project_v2_api_migration)",
	"v2_get_cargo":    "v2 API; client on v1 (project_v2_api_migration)",
	"v2_get_skills":   "v2 API; client on v1 (project_v2_api_migration)",
	"v2_get_missions": "v2 API; client on v1 (project_v2_api_migration)",
	"v2_get_queue":    "v2 API; client on v1 (project_v2_api_migration)",
	"get_state":       "v2 API; client on v1 (project_v2_api_migration)",
	"get_location":    "v2 format; get_poi→get_location migration on hold (project_get_poi_retirement); consumed generically",
	"storage":         "v2 unified storage command; client uses v1 view_storage/deposit_items/withdraw_items",

	// Streaming subscriptions — no client consumer yet:
	"subscribe_market":        "streaming subscription; no client consumer",
	"unsubscribe_market":      "streaming subscription; no client consumer",
	"subscribe_observation":   "streaming subscription; no client consumer",
	"unsubscribe_observation": "streaming subscription; no client consumer",

	// NOTE: the block that used to sit here listed 16 commands as "not
	// implemented". That was misleading — every one of them IS implemented
	// server-side and returns a fully-specified payload, and all are reachable
	// via play_as's raw passthrough. They now have response structs
	// (serverapi/responses_passthrough.go) and entries in actionResponseTypes,
	// so drift in them is detected instead of silently ignored. They still have
	// no dedicated client method; that is a separate question from coverage.
	//
	// Only add an entry here for a command the client genuinely cannot receive a
	// response for (a different API version, or a subscription we never open).
}

func TestServerCommandsCoveredByClient(t *testing.T) {
	path := filepath.Join("..", "..", "data", "game-api", "latest", "get_commands.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("get_commands.json not found: %v", err)
	}
	var doc struct {
		Commands []struct {
			Name string `json:"name"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, c := range doc.Commands {
		if actionResponseTypes[c.Name] != nil {
			continue
		}
		if _, ok := ignoredCommands[c.Name]; ok {
			continue
		}
		t.Errorf("server command %q is not covered by actionResponseTypes and not in ignoredCommands "+
			"(add a client method + response struct, or add to ignoredCommands with justification)", c.Name)
	}
}
