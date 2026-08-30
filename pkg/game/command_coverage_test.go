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
	// v0.572.0 personnel/prize commands ("Layer B"): typed wrappers are
	// deliberately deferred until the passive absorption (structs, pushes,
	// KB tables — committed 2026-08-30) is rolled out to the fleets. Remove
	// these entries as each command gets its client method + response struct.
	// See memory reference_v0572_boarding_personnel.
	"claim_prize":        "v0.572.0 Layer B; wrapper deferred until A+C are deployed",
	"service_prize":      "v0.572.0 Layer B; wrapper deferred until A+C are deployed",
	"recruit_personnel":  "v0.572.0 Layer B; wrapper deferred until A+C are deployed",
	"treat_personnel":    "v0.572.0 Layer B; wrapper deferred until A+C are deployed",
	"transfer_personnel": "v0.572.0 Layer B; wrapper deferred until A+C are deployed",
	"faction_personnel":  "v0.572.0 Layer B; wrapper deferred until A+C are deployed",
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

	// Battle/inspect read commands — STOPGAP (surfaced 2026-07-19 when the
	// shipping-carrier client became the first pkg/game change to hit this
	// package's pre-commit race hook). These are normal v1 commands with real
	// payloads, so the map's convention says they belong in the passthrough path
	// (responses_passthrough.go struct + actionResponseTypes entry), NOT here.
	// Parked to unblock the shipping work; replace with passthrough coverage.
	"get_battle_summary": "battle read command; deferred to passthrough coverage (stopgap 2026-07-19)",
	"get_battle_log":     "battle read command; deferred to passthrough coverage (stopgap 2026-07-19)",
	"inspect":            "inspect read command; deferred to passthrough coverage (stopgap 2026-07-19)",

	// shipping is action-dispatched (list/get/accept/deliver/return/cancel/
	// profile/track/pay_debt/quote/post), cached under shipping_<action> by
	// storeRawJSON. That's not structurally impossible for this map's shape:
	// battle (serverapi.BattleResponse), facility (serverapi.FacilityResponse),
	// and cloak (serverapi.CloakResponse) are all action-dispatched multi-shape
	// commands covered by one umbrella struct with omitempty fields spanning
	// their sub-actions, registered in actionResponseTypes. A ShippingResponse
	// built the same way is the known path here too; it's just deferred, not
	// built yet, so it doesn't block the action_result decode fix landing
	// (stopgap 2026-07-20).
	"shipping": "action-dispatched; umbrella ShippingResponse (battle/facility-style) deferred, not built (stopgap 2026-07-20)",

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
