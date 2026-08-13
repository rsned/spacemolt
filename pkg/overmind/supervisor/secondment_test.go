package supervisor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func nomination(agent string) Secondment {
	return Secondment{
		AgentID:     agent,
		HomeFleet:   "haul",
		AwayFleet:   "unlock",
		NominatedAt: "2026-08-12T22:00:00Z",
		StationID:   "treasure_cache_trading_post",
		SystemID:    "treasure_cache",
	}
}

// TestNominateIsIdempotentPerAgent is the one that keeps a chatty worker from
// emptying the haul fleet: the nomination check runs every pass, so re-asking
// must not queue a second trip.
func TestNominateIsIdempotentPerAgent(t *testing.T) {
	var s Secondments
	if !s.Nominate(nomination("hauler-0")) {
		t.Fatal("first nomination must be accepted")
	}
	for range 5 {
		if s.Nominate(nomination("hauler-0")) {
			t.Fatal("a second nomination while one is in flight must be refused")
		}
	}
	if len(s.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(s.Entries))
	}
}

// TestAnAgentCanBeSecondedAgainAfterComingHome: the refusal above is scoped to
// the trip, not to the agent forever.
func TestAnAgentCanBeSecondedAgainAfterComingHome(t *testing.T) {
	var s Secondments
	s.Nominate(nomination("hauler-0"))
	s.SetPhase("hauler-0", PhaseHome, "unlock held", "2026-08-12T23:00:00Z")
	if !s.Nominate(nomination("hauler-0")) {
		t.Fatal("a completed trip must not block a later one")
	}
}

// TestInFlightCountsOnlyTripsNotYetHome backs the concurrency cap. Miscounting
// here is what would let several haulers leave at once.
func TestInFlightCountsOnlyTripsNotYetHome(t *testing.T) {
	var s Secondments
	s.Nominate(nomination("hauler-0"))
	s.Nominate(nomination("hauler-1"))
	s.Nominate(nomination("hauler-2"))
	s.SetPhase("hauler-1", PhaseSeconded, "", "2026-08-12T22:10:00Z")
	s.SetPhase("hauler-2", PhaseHome, "", "2026-08-12T22:20:00Z")
	if got := s.InFlight(); got != 2 {
		t.Fatalf("InFlight = %d, want 2 (nominated + seconded, not home)", got)
	}
}

// TestSetPhaseLeavesTerminalEntriesAlone: a finished trip is a record. Letting a
// later call reopen it would resurrect an agent into a fleet it already left.
func TestSetPhaseLeavesTerminalEntriesAlone(t *testing.T) {
	var s Secondments
	s.Nominate(nomination("hauler-0"))
	s.SetPhase("hauler-0", PhaseFailed, "stop timed out", "2026-08-12T22:30:00Z")
	if s.SetPhase("hauler-0", PhaseSeconded, "retry", "2026-08-12T22:40:00Z") {
		t.Fatal("a failed trip must not be advanced automatically")
	}
	if s.Entries[0].Phase != PhaseFailed {
		t.Fatalf("phase = %q, want %q", s.Entries[0].Phase, PhaseFailed)
	}
}

// TestFailedTripBlocksRenomination: a failure leaves a membership change
// half-applied, and re-nominating from there is how an agent ends up started in
// two fleets at once. It must stay pinned until an operator clears the entry.
func TestFailedTripBlocksRenomination(t *testing.T) {
	var s Secondments
	s.Nominate(nomination("hauler-0"))
	s.SetPhase("hauler-0", PhaseFailed, "stop timed out", "2026-08-12T22:30:00Z")
	if s.Nominate(nomination("hauler-0")) {
		t.Fatal("a failed trip must block re-nomination until an operator clears it")
	}
	if got := s.InFlight(); got != 0 {
		t.Fatalf("InFlight = %d: a failed trip holds no fleet slot, it holds the AGENT", got)
	}
}

func TestPruneKeepsRecentAndInFlight(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	s := Secondments{Entries: []Secondment{
		{AgentID: "old", Phase: PhaseHome, UpdatedAt: now.Add(-48 * time.Hour).Format(time.RFC3339)},
		{AgentID: "recent", Phase: PhaseHome, UpdatedAt: now.Add(-1 * time.Hour).Format(time.RFC3339)},
		{AgentID: "flying", Phase: PhaseSeconded, UpdatedAt: now.Add(-72 * time.Hour).Format(time.RFC3339)},
		{AgentID: "bad-stamp", Phase: PhaseFailed, UpdatedAt: "not-a-time"},
	}}
	s.Prune(now, 24*time.Hour)
	kept := map[string]bool{}
	for _, e := range s.Entries {
		kept[e.AgentID] = true
	}
	if kept["old"] {
		t.Error("a terminal entry older than the window must be pruned")
	}
	for _, want := range []string{"recent", "flying", "bad-stamp"} {
		if !kept[want] {
			t.Errorf("%s must be kept", want)
		}
	}
}

// TestSecondmentsRoundTripOnDisk also proves the write is atomic-by-rename, so a
// reader never sees a half-written ledger.
func TestSecondmentsRoundTripOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "secondments.json")
	var s Secondments
	s.Nominate(nomination("hauler-0"))
	if err := SaveSecondments(path, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("the temp file must not survive a successful save")
	}
	got, err := LoadSecondments(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Entries) != 1 || got.Entries[0].AgentID != "hauler-0" || got.Entries[0].Phase != PhaseNominated {
		t.Fatalf("round trip lost data: %+v", got.Entries)
	}
}

func TestLoadSecondmentsTreatsAMissingFileAsEmpty(t *testing.T) {
	got, err := LoadSecondments(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("a missing ledger is not an error: %v", err)
	}
	if len(got.Entries) != 0 {
		t.Fatal("a missing ledger must be empty")
	}
}

// TestLoadSecondmentsReportsCorruptionWithoutBlocking mirrors LoadOverrides: a
// bad sidecar must never take a fleet down, but it must be loud.
func TestLoadSecondmentsReportsCorruptionWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := LoadSecondments(path)
	if err == nil {
		t.Fatal("corruption must be reported")
	}
	if len(got.Entries) != 0 {
		t.Fatal("a corrupt ledger must degrade to empty")
	}
}
