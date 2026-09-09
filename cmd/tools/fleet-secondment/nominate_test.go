package main

import (
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

func ledgerPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "secondments.json")
}

// The whole point of the CLI: an operator can enqueue an agent that cannot
// nominate itself, because only the hauler role has a self-nomination path.
func TestNominateAgentsAddsNewEntries(t *testing.T) {
	p := ledgerPath(t)
	added, skipped, err := nominateAgents(p, []string{"marketbot_008"}, "mb", "unlock", "operator")
	if err != nil {
		t.Fatalf("nominateAgents: %v", err)
	}
	if len(added) != 1 || added[0] != "marketbot_008" {
		t.Fatalf("added = %v, want [marketbot_008]", added)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}

	led, err := supervisor.LoadSecondments(p)
	if err != nil {
		t.Fatalf("LoadSecondments: %v", err)
	}
	var found *supervisor.Secondment
	for i := range led.Entries {
		if led.Entries[i].AgentID == "marketbot_008" {
			found = &led.Entries[i]
		}
	}
	if found == nil {
		t.Fatal("nomination was not persisted")
	}
	if found.HomeFleet != "mb" || found.AwayFleet != "unlock" {
		t.Fatalf("home/away = %q/%q, want mb/unlock", found.HomeFleet, found.AwayFleet)
	}
}

// Re-running must not open a second trip for the same agent — an agent moved
// twice concurrently is an agent running twice, which kills its game session.
func TestNominateAgentsIsIdempotent(t *testing.T) {
	p := ledgerPath(t)
	if _, _, err := nominateAgents(p, []string{"miner-7"}, "mining", "unlock", "operator"); err != nil {
		t.Fatalf("first nominate: %v", err)
	}
	added, skipped, err := nominateAgents(p, []string{"miner-7"}, "mining", "unlock", "operator")
	if err != nil {
		t.Fatalf("second nominate: %v", err)
	}
	if len(added) != 0 {
		t.Fatalf("added = %v on re-nominate, want none", added)
	}
	if len(skipped) != 1 || skipped[0] != "miner-7" {
		t.Fatalf("skipped = %v, want [miner-7]", skipped)
	}
}

// A batch must report per-agent outcomes rather than failing wholesale, so one
// already-nominated agent does not block the rest of a rotation batch.
func TestNominateAgentsMixedBatch(t *testing.T) {
	p := ledgerPath(t)
	if _, _, err := nominateAgents(p, []string{"a"}, "mb", "unlock", "operator"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	added, skipped, err := nominateAgents(p, []string{"a", "b", "c"}, "mb", "unlock", "operator")
	if err != nil {
		t.Fatalf("nominateAgents: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("added = %v, want b and c", added)
	}
	if len(skipped) != 1 || skipped[0] != "a" {
		t.Fatalf("skipped = %v, want [a]", skipped)
	}
}

// Blank entries from a trailing comma must not become a nomination for "".
func TestParseAgentListDropsBlanks(t *testing.T) {
	got := parseAgentList(" miner-7 , ,miner-8,")
	if len(got) != 2 || got[0] != "miner-7" || got[1] != "miner-8" {
		t.Fatalf("parseAgentList = %v, want [miner-7 miner-8]", got)
	}
}
