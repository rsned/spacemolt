package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestMatchType(t *testing.T) {
	m := matchType(protocol.TypeOK)
	if !m(protocol.Response{Type: protocol.TypeOK}) {
		t.Error("expected OK to match")
	}
	if m(protocol.Response{Type: protocol.TypeError}) {
		t.Error("expected Error not to match")
	}
}

func TestMatchAction(t *testing.T) {
	m := matchAction("get_system")
	ok := protocol.Response{Payload: map[string]any{"action": "get_system"}}
	if !m(ok) {
		t.Error("expected get_system action to match")
	}
	if m(protocol.Response{Payload: map[string]any{"action": "get_poi"}}) {
		t.Error("expected get_poi not to match")
	}
	if m(protocol.Response{Payload: map[string]any{}}) {
		t.Error("expected missing action not to match")
	}
	if m(protocol.Response{}) {
		t.Error("expected nil payload not to match")
	}
}

func TestMatchCommand(t *testing.T) {
	m := matchCommand("deposit_items")
	if !m(protocol.Response{Payload: map[string]any{"command": "deposit_items"}}) {
		t.Error("expected deposit_items to match")
	}
	if m(protocol.Response{Payload: map[string]any{"command": "withdraw_items"}}) {
		t.Error("expected withdraw_items not to match")
	}
}

func TestMatchChannel(t *testing.T) {
	m := matchChannel("system")
	if !m(protocol.Response{Payload: map[string]any{"channel": "system"}}) {
		t.Error("expected system channel to match")
	}
	if m(protocol.Response{Payload: map[string]any{"channel": "local"}}) {
		t.Error("expected local channel not to match")
	}
}

func TestMatchPayloadKey(t *testing.T) {
	m := matchPayloadKey("cargo")
	if !m(protocol.Response{Payload: map[string]any{"cargo": []any{}}}) {
		t.Error("expected cargo key presence to match")
	}
	if m(protocol.Response{Payload: map[string]any{"ship": nil}}) {
		t.Error("expected missing cargo key not to match")
	}
}

func TestMatchAllEmpty(t *testing.T) {
	// Empty matchAll() is vacuously true — matches every response.
	m := matchAll()
	if !m(protocol.Response{}) {
		t.Error("expected empty matchAll to match zero response")
	}
	if !m(protocol.Response{Type: protocol.TypeError}) {
		t.Error("expected empty matchAll to match arbitrary response")
	}
}

func TestMatchAll(t *testing.T) {
	m := matchAll(matchType(protocol.TypeOK), matchPayloadKey("cargo"))
	ok := protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{"cargo": []any{}}}
	if !m(ok) {
		t.Error("expected composite match")
	}
	missingType := protocol.Response{Payload: map[string]any{"cargo": []any{}}}
	if m(missingType) {
		t.Error("expected missing type to fail composite")
	}
	missingKey := protocol.Response{Type: protocol.TypeOK}
	if m(missingKey) {
		t.Error("expected missing key to fail composite")
	}
}
