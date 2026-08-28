package main

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

type nearbyCountClient struct {
	game.GameClient
	nearbyCalls int
	raw         map[string][]byte
}

func (c *nearbyCountClient) GetNearby(ctx context.Context) error {
	c.nearbyCalls++
	return nil
}
func (c *nearbyCountClient) GetRawJSON(key string) []byte { return c.raw[key] }
func (c *nearbyCountClient) GetState() *game.State        { return &game.State{} }

// A get_nearby creature list is a snapshot, not a census: ashford_ice_shelf
// read 0 creatures at 12:41:35 and 3 at 12:43:16 on 2026-08-28, and
// prismatic_gas_pocket did the same. One look per POI therefore records a
// false negative as fact.
func TestSecondLook_IssuesAnotherGetNearby(t *testing.T) {
	c := &nearbyCountClient{raw: map[string][]byte{
		"nearby": []byte(`{"poi_id":"ashford_ice_shelf","creature_count":0,"creatures":[]}`),
	}}
	captureWildlifeSecondLook(c, context.Background(), "ashford_ice_shelf", "ice_field", formatStyled)
	if c.nearbyCalls != 1 {
		t.Errorf("GetNearby called %d times, want 1", c.nearbyCalls)
	}
}

// It must survive a KB-less session and an empty reply without panicking; this
// runs inside an exploration loop that must not break for bookkeeping.
func TestSecondLook_ToleratesNoKBAndEmptyReply(t *testing.T) {
	c := &nearbyCountClient{raw: map[string][]byte{}}
	captureWildlifeSecondLook(c, context.Background(), "poi", "ice_field", formatStyled)
	if c.nearbyCalls != 1 {
		t.Errorf("GetNearby called %d times, want 1", c.nearbyCalls)
	}
}
