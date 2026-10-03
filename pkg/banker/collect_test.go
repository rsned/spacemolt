package banker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

func writeStatus(t *testing.T, dir, fleet string, capturedAt time.Time, recs ...balances.LiveRecord) {
	t.Helper()
	sf := balances.StatusFile{CapturedAt: capturedAt.UTC().Format(time.RFC3339), Workers: recs}
	b, err := json.Marshal(sf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fleet+"-status.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Collect reads every fleet's live status file, which is the only place a
// fleet-wide view of credits exists. agent_profile is captured hourly and
// agent_faction daily, far too stale for an agent that stranded ten minutes ago.
func TestCollectReadsEveryFleet(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeStatus(t, dir, "haul", now, balances.LiveRecord{AgentID: "trader-1", Credits: 0, Seen: true})
	writeStatus(t, dir, "mb", now, balances.LiveRecord{AgentID: "marketbot_001", Credits: 500, Seen: true})

	got, stale, err := Collect(dir, time.Hour, now)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("nothing should be stale: %v", stale)
	}
	byID := map[string]Candidate{}
	for _, c := range got {
		byID[c.AgentID] = c
	}
	if byID["trader-1"].Fleet != "haul" || byID["marketbot_001"].Fleet != "mb" {
		t.Fatalf("fleet must come from the filename: %+v", got)
	}
}

// A stale status file is the documented failure mode here -- fleet-status.json
// once sat 17 days dead while reading as live. Funding off it would pay ghosts
// and miss real strandings, so the whole file is refused and NAMED rather than
// silently trusted or silently dropped.
func TestCollectRefusesAStaleFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeStatus(t, dir, "fresh", now, balances.LiveRecord{AgentID: "ok", Credits: 0, Seen: true})
	writeStatus(t, dir, "fossil", now.Add(-48*time.Hour), balances.LiveRecord{AgentID: "ghost", Credits: 0, Seen: true})

	got, stale, err := Collect(dir, time.Hour, now)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, c := range got {
		if c.AgentID == "ghost" {
			t.Fatal("an agent from a stale file must never be funded")
		}
	}
	if len(stale) != 1 || stale[0] != "fossil" {
		t.Fatalf("the stale fleet must be named: %v", stale)
	}
}

// An agent the supervisor has not seen has no trustworthy credit reading: the
// heartbeat is known to report a stale 0, which would make a solvent agent look
// broke and pull money it does not need.
func TestCollectSkipsUnseenAgents(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeStatus(t, dir, "haul", now,
		balances.LiveRecord{AgentID: "live", Credits: 0, Seen: true},
		balances.LiveRecord{AgentID: "never-launched", Credits: 0, Seen: false},
	)

	got, _, err := Collect(dir, time.Hour, now)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 1 || got[0].AgentID != "live" {
		t.Fatalf("only seen agents carry a usable credit reading: %+v", got)
	}
}
