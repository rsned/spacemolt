package game

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStoreRawJSONOwnedShips pins the same class of raw-key drift that killed
// ship-listing capture 2026-02-18..2026-07-04, in its second instance: the
// "owned_ships" key was only ever reachable through the action-based switch
// (case "list_ships"), but a live list_ships reply carries NO "action" field —
// verified against databot 2026-08-01:
//
//	{"active_ship_class":"theoria","active_ship_id":"eff9ab...","count":2,
//	 "ships":[...]}
//
// So the payload fell through to the generic hasShips branch and landed under
// "ships", leaving "owned_ships" permanently empty. cmd/tools/daily-summary
// papered over it with a read-side fallback; pkg/assets read only
// "owned_ships" and captured zero hulls for every agent.
//
// The discriminator is active_ship_id: an owned-fleet listing always names the
// active hull, and no other "ships"-bearing payload does.
func TestStoreRawJSONOwnedShips(t *testing.T) {
	c := &Client{
		latestRawJSON: make(map[string][]byte),
		latestShips:   make(map[string]any),
		debugLogger:   log.New(io.Discard, "", 0),
	}

	// list_ships response, live shape 2026-08-01: no "action" field.
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"active_ship_class": "theoria",
			"active_ship_id":    "eff9ab6ddf08251a0623ce6471233d33",
			"count":             float64(2),
			"ships": []any{
				map[string]any{
					"class_id": "theoria", "class_name": "Theoria",
					"fuel": "55/100", "hull": "100/100", "is_active": true,
					"ship_id": "eff9ab6ddf08251a0623ce6471233d33",
				},
				map[string]any{
					"class_id": "catalogue", "class_name": "Catalogue",
					"fuel": "110/110", "hull": "90/90", "is_active": false,
					"location_base_id": "confederacy_central_command",
					"ship_id":          "c63763d53539dd8cdde94211d64916d9",
				},
			},
		},
	})

	got := string(c.GetRawJSON("owned_ships"))
	if !strings.Contains(got, "catalogue") {
		t.Errorf("GetRawJSON(%q) missing list_ships payload: %q", "owned_ships", got)
	}

	// The generic "ships" key must keep working: cmd/auto-trader and
	// cmd/tools/daily-summary read it, and this fix must not break them.
	if s := string(c.GetRawJSON("ships")); !strings.Contains(s, "catalogue") {
		t.Errorf("GetRawJSON(%q) must still carry the payload for existing readers: %q", "ships", s)
	}
}

// TestStoreRawJSONShipsWithoutActiveShipIDIsNotOwned pins the negative case:
// a "ships"-bearing payload that names no active hull is not an owned-fleet
// listing and must not claim the owned_ships key, or an unrelated response
// could overwrite a good capture with something that is not the agent's fleet.
func TestStoreRawJSONShipsWithoutActiveShipIDIsNotOwned(t *testing.T) {
	c := &Client{
		latestRawJSON: make(map[string][]byte),
		latestShips:   make(map[string]any),
		debugLogger:   log.New(io.Discard, "", 0),
	}

	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"count": float64(1),
			"ships": []any{map[string]any{"class_id": "hauler"}},
		},
	})

	if s := c.GetRawJSON("owned_ships"); s != nil {
		t.Errorf("payload without active_ship_id must not land under owned_ships: %q", string(s))
	}
	if s := string(c.GetRawJSON("ships")); !strings.Contains(s, "hauler") {
		t.Errorf("payload must still land under the generic ships key: %q", s)
	}
}
