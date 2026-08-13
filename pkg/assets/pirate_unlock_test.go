package assets

import (
	"context"
	"testing"
)

// seedStanding writes one agent + one pirate standing row directly, which is the
// shape the capture path produces.
func seedStanding(t *testing.T, s *Store, agentID, playerID, faction string, baseline, reputation int) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO agents (player_id, agent_id) VALUES (?, ?)`, playerID, agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO agent_standings (player_id, faction, reputation, baseline, captured_at)
		 VALUES (?, ?, ?, ?, '2026-08-12T22:00:00Z')`,
		playerID, faction, reputation, baseline); err != nil {
		t.Fatalf("seed standing: %v", err)
	}
}

// TestHoldsPirateUnlockReadsBaselineNotReputation is the distinction that took a
// wrong turn to find: reputation is 10-11 whether or not an agent holds the
// unlock, so only the baseline separates them (-30 locked, 10 unlocked).
func TestHoldsPirateUnlockReadsBaselineNotReputation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Locked: baseline -30, but the SAME reputation an unlocked agent shows.
	seedStanding(t, s, "hauler-locked", "p-locked", "pirate_voss", -30, 10)
	// Unlocked: baseline 10.
	seedStanding(t, s, "hauler-free", "p-free", "pirate_voss", 10, 10)

	locked, err := s.HoldsPirateUnlock(ctx, "hauler-locked")
	if err != nil {
		t.Fatalf("locked: %v", err)
	}
	if locked {
		t.Error("baseline -30 must read as LOCKED even though reputation is 10")
	}
	free, err := s.HoldsPirateUnlock(ctx, "hauler-free")
	if err != nil {
		t.Fatalf("unlocked: %v", err)
	}
	if !free {
		t.Error("baseline 10 must read as unlocked")
	}
}

// TestHoldsPirateUnlockNeedsOnlyOneStronghold: the mission raises every pirate
// standing, but one is enough to answer the question.
func TestHoldsPirateUnlockNeedsOnlyOneStronghold(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedStanding(t, s, "hauler-0", "p0", "pirate_voss", -30, 10)
	seedStanding(t, s, "hauler-0", "p0", "pirate_thane", 10, 10)
	got, err := s.HoldsPirateUnlock(ctx, "hauler-0")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !got {
		t.Error("one stronghold at the unlock baseline is enough")
	}
}

// TestHoldsPirateUnlockIgnoresNonPirateFactions guards against an empire standing
// (nebula, outerrim) being mistaken for pirate access — those routinely exceed 10.
func TestHoldsPirateUnlockIgnoresNonPirateFactions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedStanding(t, s, "hauler-0", "p0", "nebula", 20, 28)
	seedStanding(t, s, "hauler-0", "p0", "outerrim", 20, 30)
	got, err := s.HoldsPirateUnlock(ctx, "hauler-0")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if got {
		t.Error("empire standings must not be read as pirate stronghold access")
	}
}

// TestHoldsPirateUnlockOnAnUncapturedAgent: absence of evidence answers "not
// yet", which is the safe reading for a caller deciding whether to end a trip.
func TestHoldsPirateUnlockOnAnUncapturedAgent(t *testing.T) {
	s := openTestStore(t)
	got, err := s.HoldsPirateUnlock(context.Background(), "never-seen")
	if err != nil {
		t.Fatalf("an unknown agent is not an error: %v", err)
	}
	if got {
		t.Error("an agent with no captured standings must not read as unlocked")
	}
}
