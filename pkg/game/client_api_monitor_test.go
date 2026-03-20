package game

import (
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

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
		"tick":             float64(100),
		"brand_new_field":  "surprise",
		"another_new_one":  42,
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
