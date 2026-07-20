package game

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestHandleCompleteMissionEvent verifies the server-initiated complete_mission
// notification (mission_title + nested rewards, distinct from the command
// response) is handled rather than logged as an unhandled response type or
// flagged for unknown fields.
func TestHandleCompleteMissionEvent(t *testing.T) {
	loggedAPIChanges.Delete("unknown_type:" + protocol.TypeCompleteMission)
	loggedAPIChanges.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && len(k) >= 20 && k[:20] == "unknown_event_fields" {
			loggedAPIChanges.Delete(key)
		}
		return true
	})

	// Decode the exact wire payload so number/object types match json.Unmarshal.
	const wire = `{"mission_id":"4efa4ef3d948474765ac2fcfbd7efbd8","mission_title":"Distress: Peon3 in Haven","rewards":{"credits":0,"skill_xp":{"piloting":25}}}`
	var payload map[string]any
	if err := json.Unmarshal([]byte(wire), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	client := NewClient("ws://test", "user", "pass", nil)
	client.SetDebugLogging(false)
	client.handleResponse(protocol.Response{Type: protocol.TypeCompleteMission, Payload: payload})

	if _, found := loggedAPIChanges.Load("unknown_type:" + protocol.TypeCompleteMission); found {
		t.Error("complete_mission was treated as an unhandled response type")
	}
	loggedAPIChanges.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && len(k) >= 20 && k[:20] == "unknown_event_fields" {
			t.Errorf("complete_mission flagged for unknown fields: %s", k)
		}
		return true
	})
}

func TestJsonFieldNames(t *testing.T) {
	fields := jsonFieldNames(reflect.TypeOf(serverapi.GetSystemResponse{}))
	want := map[string]bool{
		"action":          true,
		"poi":             true,
		"security_status": true,
		"system":          true,
	}
	for k := range want {
		if !fields[k] {
			t.Errorf("jsonFieldNames missing expected field %q", k)
		}
	}
	if fields["nonexistent"] {
		t.Error("jsonFieldNames returned true for nonexistent field")
	}
}

func TestExpectedFieldsForAction_Known(t *testing.T) {
	// Clear cache for deterministic test
	actionFieldsCache.Range(func(key, _ any) bool {
		actionFieldsCache.Delete(key)
		return true
	})

	fields, known := expectedFieldsForAction("get_system")
	if !known {
		t.Fatal("get_system should be a known action")
	}
	// Should include struct fields
	if !fields["system"] {
		t.Error("expected 'system' field from GetSystemResponse")
	}
	// Should include common envelope fields
	if !fields["player"] {
		t.Error("expected 'player' from commonOKFields")
	}
}

func TestExpectedFieldsForAction_Unknown(t *testing.T) {
	_, known := expectedFieldsForAction("totally_new_action")
	if known {
		t.Error("totally_new_action should not be known")
	}
}

func TestExpectedFieldsForAction_Cached(t *testing.T) {
	// First call populates cache
	fields1, _ := expectedFieldsForAction("get_poi")
	// Second call should return same result
	fields2, _ := expectedFieldsForAction("get_poi")
	if len(fields1) != len(fields2) {
		t.Error("cached result should match")
	}
}

func TestCheckOKResponseFields_UnknownAction(t *testing.T) {
	// Clear dedup state for this test
	loggedAPIChanges.Delete("unknown_action:brand_new_action")

	client := NewClient("ws://test", "user", "pass", nil)
	client.SetDebugLogging(false)

	payload := map[string]any{
		"action":    "brand_new_action",
		"new_field": "value",
	}
	// Should not panic
	_ = client // ensure client is valid
	CheckOKResponseFields(payload)

	// Verify it was logged (dedup key should exist)
	if _, ok := loggedAPIChanges.Load("unknown_action:brand_new_action"); !ok {
		t.Error("expected unknown action to be logged")
	}
}

func TestCheckEventFields_UnknownFields(t *testing.T) {
	// Clear dedup state
	loggedAPIChanges.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && len(k) > 20 && k[:20] == "unknown_event_fields" {
			loggedAPIChanges.Delete(key)
		}
		return true
	})

	client := NewClient("ws://test", "user", "pass", nil)
	client.SetDebugLogging(false)

	payload := map[string]any{
		"tick":            float64(100),
		"brand_new_field": "surprise",
		"another_new_one": 42,
	}
	// Should not panic
	client.checkEventFields(protocol.TypeTick, payload)
}

func TestAllActionTypesHaveValidStructs(t *testing.T) {
	for action, rt := range actionResponseTypes {
		if rt.Kind() != reflect.Struct {
			t.Errorf("actionResponseTypes[%q] is %v, want struct", action, rt.Kind())
		}
		fields := jsonFieldNames(rt)
		if len(fields) == 0 {
			t.Errorf("actionResponseTypes[%q] (%s) has no JSON fields", action, rt.Name())
		}
	}
}

// v0.531.4 added a `kind` discriminator to ~39 response shapes. The monitor
// diffs top-level payload keys against the registered struct's json tags, so
// every registered action whose response now carries kind must have a Kind
// field or the fleet logs fill with [SERVER API CHANGE] spam.
func TestExpectedFieldsIncludeKindDiscriminator(t *testing.T) {
	actionFieldsCache.Range(func(key, _ any) bool {
		actionFieldsCache.Delete(key)
		return true
	})
	// Every registered action whose v0.531.4 response carries a top-level kind.
	for _, action := range []string{
		"get_system", "get_poi",
		"create_buy_order", "create_sell_order", "cancel_order", "modify_order",
		"faction_create_buy_order", "faction_create_sell_order",
		"mine", "craft", "recycle",
		"facility", "facility_list", "list",
		"unload_passenger", "attack",
	} {
		fields, known := expectedFieldsForAction(action)
		if !known {
			t.Errorf("%q should be a known action", action)
			continue
		}
		if !fields["kind"] {
			t.Errorf("%q expected fields must include the v0.531.4 kind discriminator", action)
		}
	}
}

// The /shipping reads reply with bare-verb actions; they must be registered or
// every carrier pass logs "Unhandled action" spam.
func TestShippingReadActionsRegistered(t *testing.T) {
	cases := map[string]string{
		"get":     "contract",
		"profile": "profile",
		"track":   "events",
	}
	for action, marker := range cases {
		fields, known := expectedFieldsForAction(action)
		if !known {
			t.Errorf("shipping read action %q should be registered", action)
			continue
		}
		if !fields[marker] {
			t.Errorf("%q expected fields must include %q from its shipping struct", action, marker)
		}
	}
}

// "list" is served by BOTH the facility command and the /shipping board read,
// and the payload carries no originating command, so the expected-field set is
// the union of both structs. A field in neither must still count as unknown —
// the union must not become a sieve.
func TestListActionUnionCoversFacilityAndShipping(t *testing.T) {
	actionFieldsCache.Range(func(key, _ any) bool {
		actionFieldsCache.Delete(key)
		return true
	})
	fields, known := expectedFieldsForAction("list")
	if !known {
		t.Fatal("list should be a known action")
	}
	// Facility side.
	if !fields["station_facilities"] {
		t.Error("list union must include station_facilities from FacilityListResponse")
	}
	// Shipping side.
	for _, f := range []string{"shipments", "total", "empty_reason"} {
		if !fields[f] {
			t.Errorf("list union must include %q from ShippingListResponse", f)
		}
	}
	// Still a real check: a genuinely new server field is in neither struct.
	if fields["definitely_not_a_real_field"] {
		t.Error("union must not accept fields absent from every registered struct")
	}
}
