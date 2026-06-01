package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// stubSightingClient satisfies the slice of game.GameClient that
// captureSightings touches: GetRawJSON only.
type stubSightingClient struct {
	game.GameClient
	raw []byte
}

func (s stubSightingClient) GetRawJSON(string) []byte { return s.raw }

func TestCaptureSightings_AppendsRawOnSuccess(t *testing.T) {
	client := stubSightingClient{raw: []byte(`{"agents":[]}`)}
	called := false
	fn := func(context.Context) error { called = true; return nil }

	var got []json.RawMessage
	captureSightings(client, context.Background(), fn, "get_nearby", formatRaw, &got)

	if !called {
		t.Fatal("query fn was not called")
	}
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1 appended in raw mode", len(got))
	}
}

func TestCaptureSightings_ErrorIsNonFatalNoAppend(t *testing.T) {
	client := stubSightingClient{raw: []byte(`{"agents":[]}`)}
	fn := func(context.Context) error { return errors.New("boom") }

	var got []json.RawMessage
	// Must not panic or append on error.
	captureSightings(client, context.Background(), fn, "get_nearby", formatRaw, &got)

	if len(got) != 0 {
		t.Fatalf("got %d responses, want 0 on error", len(got))
	}
}

func TestCaptureSightings_StyledDoesNotAppend(t *testing.T) {
	client := stubSightingClient{raw: []byte(`{"agents":[]}`)}
	fn := func(context.Context) error { return nil }

	var got []json.RawMessage
	captureSightings(client, context.Background(), fn, "get_system_agents", formatStyled, &got)

	if len(got) != 0 {
		t.Fatalf("got %d responses, want 0 in styled mode (no raw collection)", len(got))
	}
}
