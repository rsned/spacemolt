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

// TestCaptureProfileClearsHullsOnLegitimateEmptyFleet pins that a clean
// ListShips call reporting zero owned ships actually clears agent_hulls,
// not merely skips the write. Gating the write on len(hulls) > 0 instead of
// on call/decode success would leave a sold last ship's hull row behind
// forever — including a stale is_active row — letting Task 6's
// activeHull() keep reporting haul/freight/mission_delivery as eligible on
// phantom capacity.
func TestCaptureProfileClearsHullsOnLegitimateEmptyFleet(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newFakeClient()

	// Seed: first capture writes at least one hull.
	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("seed CaptureProfile: %v", err)
	}
	var seeded int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_hulls WHERE player_id='abc123'`).Scan(&seeded); err != nil {
		t.Fatalf("seeded count: %v", err)
	}
	if seeded == 0 {
		t.Fatalf("seed did not write any hulls, cannot test clearing")
	}

	// Second capture: ListShips succeeds, but the agent now legitimately owns
	// zero ships.
	c.raw["owned_ships"] = []byte(`{"action":"list_ships","ships":[]}`)
	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("second CaptureProfile: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_hulls WHERE player_id='abc123'`).Scan(&n); err != nil {
		t.Fatalf("hulls after clearing: %v", err)
	}
	if n != 0 {
		t.Errorf("agent_hulls rows = %d, want 0 (stale hull not cleared on a legitimately empty fleet)", n)
	}
}

// TestCaptureProfileHullsSurviveListShipsFailure is the complementary case:
// a ListShips error must leave previously captured hulls untouched, not
// clear them. Without this test, a "fix" that clears agent_hulls on ANY
// ListShips outcome (including failure) would also pass the sibling test
// above.
func TestCaptureProfileHullsSurviveListShipsFailure(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newFakeClient()

	// Seed: first capture writes at least one hull.
	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("seed CaptureProfile: %v", err)
	}
	var seeded int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_hulls WHERE player_id='abc123'`).Scan(&seeded); err != nil {
		t.Fatalf("seeded count: %v", err)
	}
	if seeded == 0 {
		t.Fatalf("seed did not write any hulls, cannot test survival")
	}

	// Second capture: ListShips fails outright.
	c.shipsErr = errors.New("boom")
	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("second CaptureProfile must not fail on a ListShips error: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_hulls WHERE player_id='abc123'`).Scan(&n); err != nil {
		t.Fatalf("hulls after failure: %v", err)
	}
	if n != seeded {
		t.Errorf("agent_hulls rows = %d, want %d (previously captured hulls must survive a failed ListShips)", n, seeded)
	}
}

// TestCaptureProfileHullsSurviveEmptyRawCache is the third leg, and the one
// that catches the real defect: ListShips returns nil, but no body ever landed
// in the raw cache (reconnect churn, or an ack-only await returning before the
// payload arrives). That is indistinguishable from a zero-ship fleet unless
// HullsFrom reports ok — and since an agent can never own zero ships (a
// destroyed last hull respawns a Tier 0 starter), an empty cache is the ONLY
// way this path is reached in practice. Gating on decode success alone wipes
// agent_hulls here.
func TestCaptureProfileHullsSurviveEmptyRawCache(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newFakeClient()

	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("seed CaptureProfile: %v", err)
	}
	var seeded int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_hulls WHERE player_id='abc123'`).Scan(&seeded); err != nil {
		t.Fatalf("seeded count: %v", err)
	}
	if seeded == 0 {
		t.Fatalf("seed did not write any hulls, cannot test survival")
	}

	// Second capture: ListShips succeeds, but nothing reached the raw cache.
	delete(c.raw, "owned_ships")
	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("second CaptureProfile: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_hulls WHERE player_id='abc123'`).Scan(&n); err != nil {
		t.Fatalf("hulls after empty cache: %v", err)
	}
	if n != seeded {
		t.Errorf("agent_hulls rows = %d, want %d (an empty raw cache must not wipe captured hulls)", n, seeded)
	}
}

// TestCaptureProfileFailedGetStatusWritesNothing pins the counterpart to
// TestCaptureProfileCallsGetStatusExplicitly: a GetStatus error must leave
// EVERY table untouched, not just skip the call. GetStatus is synchronous, so
// on failure GetState() still returns the previously cached Player (ID intact
// from login) — without this guard, CaptureProfile would happily write
// agent_profile, agent_skills and agent_standings under a fresh captured_at
// on data of arbitrary age, which is exactly the "capture looks fine" failure
// the whole ledger exists to make visible.
func TestCaptureProfileFailedGetStatusWritesNothing(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newFakeClient()
	c.statusErr = errors.New("boom")

	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("CaptureProfile must not fail on a GetStatus error: %v", err)
	}

	for _, q := range []struct {
		name string
		sql  string
	}{
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
		if n != 0 {
			t.Errorf("%s: %d rows written on a failed GetStatus, want 0 (stale data must not be recorded under a fresh timestamp)", q.name, n)
		}
	}
}

func capabilityEligible(t *testing.T, st *Store, playerID, capability string) bool {
	t.Helper()
	var eligible bool
	if err := st.DB().QueryRow(
		`SELECT eligible FROM agent_capability WHERE player_id=? AND capability=?`,
		playerID, capability).Scan(&eligible); err != nil {
		t.Fatalf("read capability %s: %v", capability, err)
	}

	return eligible
}

func capabilityReason(t *testing.T, st *Store, playerID, capability string) string {
	t.Helper()
	var reason string
	if err := st.DB().QueryRow(
		`SELECT blocking_reason FROM agent_capability WHERE player_id=? AND capability=?`,
		playerID, capability).Scan(&reason); err != nil {
		t.Fatalf("read reason %s: %v", capability, err)
	}

	return reason
}

// TestCaptureProfileFallsBackToStoredHulls pins the flapping fix: a transient
// ListShips failure on a later pass must not recompute capabilities as if the
// agent had never been captured. Before this, one dropped frame flipped haul,
// freight and mission_delivery to ineligible with "no active hull captured"
// while a perfectly good hull set sat in agent_hulls.
func TestCaptureProfileFallsBackToStoredHulls(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	// Pass 1: everything captures cleanly. newFakeClient() already seeds
	// raw["owned_ships"] with one active hull, so no setup is needed here.
	c := newFakeClient()
	if err := CaptureProfile(ctx, c, st, "agent-x", now); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if got := capabilityEligible(t, st, "abc123", "mission_delivery"); !got {
		t.Fatalf("pass 1 mission_delivery must be eligible")
	}

	// Pass 2: ListShips fails. The stored hull set must still carry the verdict.
	c.shipsErr = errors.New("connection reset")
	if err := CaptureProfile(ctx, c, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	if got := capabilityEligible(t, st, "abc123", "mission_delivery"); !got {
		t.Errorf("pass 2 mission_delivery flipped to ineligible on a transient ListShips failure")
	}
	if reason := capabilityReason(t, st, "abc123", "mission_delivery"); reason != "" {
		t.Errorf("an eligible verdict must carry no blocking reason, got %q", reason)
	}
}
