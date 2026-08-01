package assets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// fakeClient implements only the GameClient methods CaptureProfile uses.
// Embedding game.GameClient means the unused methods panic if ever called,
// which is the behaviour we want from a test double.
type fakeClient struct {
	game.GameClient
	state       *game.State
	statusErr   error
	shippingErr error
	shipsErr    error
	raw         map[string][]byte
	calls       []string
}

func (f *fakeClient) GetStatus(context.Context) error {
	f.calls = append(f.calls, "get_status")

	return f.statusErr
}

func (f *fakeClient) ShippingProfile(context.Context) error {
	f.calls = append(f.calls, "shipping_profile")

	return f.shippingErr
}

func (f *fakeClient) ListShips(context.Context) error {
	f.calls = append(f.calls, "list_ships")

	return f.shipsErr
}

func (f *fakeClient) GetState() *game.State { return f.state }

func (f *fakeClient) GetRawJSON(key string) []byte { return f.raw[key] }

func newFakeClient() *fakeClient {
	st := &game.State{}
	st.Player.ID = "abc123"
	st.Player.Username = "Arthur 'Artificer' Artis"
	st.Player.Credits = 15135
	st.Player.Empire = "haven"
	st.Player.HomeBase = "grand_exchange_station"
	st.Player.Skills = map[string]game.Skill{"smuggling": {Level: 3, XP: 12}}
	st.Player.Standings = map[string]game.EmpireStanding{
		"pirates": {Reputation: 42, Baseline: 10},
	}

	return &fakeClient{
		state: st,
		raw: map[string][]byte{
			"owned_ships": []byte(`{"action":"list_ships","ships":[
				{"ship_id":"s1","class_id":"reclaim","is_active":true,
				 "hull":"180/180","fuel":"150/200","location_base_id":"grand_exchange_station"}]}`),
			"shipping_profile": []byte(`{"action":"profile",
				"profile":{"tier":"licensed","successful_deliveries":6},
				"progression":{"current_tier":"licensed","next_tier":"trusted"}}`),
		},
	}
}

// TestCaptureProfileWritesEverySource pins the happy path end to end.
func TestCaptureProfileWritesEverySource(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newFakeClient()

	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("CaptureProfile: %v", err)
	}

	for _, q := range []struct {
		name string
		sql  string
	}{
		{"agents", `SELECT COUNT(*) FROM agents WHERE player_id='abc123' AND agent_id='engineer-3'`},
		{"agent_profile", `SELECT COUNT(*) FROM agent_profile WHERE player_id='abc123'`},
		{"agent_skills", `SELECT COUNT(*) FROM agent_skills WHERE player_id='abc123'`},
		{"agent_standings", `SELECT COUNT(*) FROM agent_standings WHERE player_id='abc123'`},
		{"agent_carrier", `SELECT COUNT(*) FROM agent_carrier WHERE player_id='abc123'`},
		{"agent_hulls", `SELECT COUNT(*) FROM agent_hulls WHERE player_id='abc123'`},
	} {
		var n int
		if err := st.DB().QueryRowContext(ctx, q.sql).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q.name, err)
		}
		if n == 0 {
			t.Errorf("%s: no rows written", q.name)
		}
	}

	// Pin that the carrier row actually decoded the progression block, not
	// just a zeroed struct: an all-zero CarrierTierProgress decodes cleanly
	// from a mis-keyed fixture with no error, which is exactly the trap this
	// plan has already been bitten by twice (see task-7-report.md). next_tier
	// only has a non-empty value if "progression" was the key read.
	var nextTier string
	if err := st.DB().QueryRowContext(ctx,
		`SELECT next_tier FROM agent_carrier WHERE player_id='abc123'`).
		Scan(&nextTier); err != nil {
		t.Fatalf("next_tier: %v", err)
	}
	if nextTier != "trusted" {
		t.Errorf("next_tier = %q, want %q (progression block not decoded)", nextTier, "trusted")
	}

	// Capabilities derived from the above: smuggling L3 and pirate baseline 10
	// are both eligible.
	var eligible int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT eligible FROM agent_capability WHERE player_id='abc123' AND capability='smuggling'`).
		Scan(&eligible); err != nil {
		t.Fatalf("capability: %v", err)
	}
	if eligible != 1 {
		t.Error("smuggling L3 must be eligible")
	}
}

// TestCaptureProfileSurvivesShippingFailure pins partial-capture honesty: when
// the shipping profile call fails, the profile is still written and the
// carrier row is simply absent — not written with zeroes, which would read as
// a debt-free probationary carrier.
func TestCaptureProfileSurvivesShippingFailure(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newFakeClient()
	c.shippingErr = errors.New("boom")
	delete(c.raw, "shipping_profile")

	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("CaptureProfile must not fail on a shipping error: %v", err)
	}

	var profiles, carriers int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_profile WHERE player_id='abc123'`).Scan(&profiles); err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_carrier WHERE player_id='abc123'`).Scan(&carriers); err != nil {
		t.Fatalf("carriers: %v", err)
	}
	if profiles != 1 {
		t.Errorf("agent_profile rows = %d, want 1", profiles)
	}
	if carriers != 0 {
		t.Errorf("agent_carrier rows = %d, want 0 (uncaptured, not zeroed)", carriers)
	}
}

// TestCaptureProfileCallsGetStatusExplicitly pins that we re-read status
// rather than trusting ambient state. Standings ride ONLY on a full player
// payload (pkg/game/client.go preserves the old map on a partial one), so
// skipping the call can silently persist arbitrarily stale standings.
func TestCaptureProfileCallsGetStatusExplicitly(t *testing.T) {
	st := openTestStore(t)
	c := newFakeClient()
	if err := CaptureProfile(context.Background(), c, st, "engineer-3",
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CaptureProfile: %v", err)
	}
	found := false
	for _, call := range c.calls {
		if call == "get_status" {
			found = true
		}
	}
	if !found {
		t.Error("CaptureProfile must call GetStatus explicitly")
	}
}

// TestCaptureProfileNilStoreIsANoOp pins that an unconfigured store disables
// capture rather than erroring — assets must never be a new way for a worker
// pass to fail.
func TestCaptureProfileNilStoreIsANoOp(t *testing.T) {
	if err := CaptureProfile(context.Background(), newFakeClient(), nil, "engineer-3",
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Errorf("nil store must be a no-op, got %v", err)
	}
}
