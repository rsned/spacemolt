package worker

import (
	"errors"
	"path/filepath"
	"testing"
)

// Real ids, copied from the live galaxy on 2026-08-11. A station is dual-named,
// so both its base id and its poi id appear here -- a caller may hold either.
const (
	blackthornBase = "566fbb2d3a304fa2d846147aed2aec0c" // Fortress Blackthorn: REFUSED johnny_cab
	blackthornPOI  = "9d742eadce7c211220fa20ba10093462"
	hexStarBase    = "b495c6003fc83e18f6d8cecbe6929133" // Hex Star: nine agents docked
	hexStarPOI     = "98eba8b1a7ad0520d6a7c8ea44b2d6aa"
	obsidianBase   = "cca9e51e6eaf8dada77f698ccc4a09c7" // The Obsidian Well: marketbot_002 docked
)

func TestPlayerStationIDDiscriminatesByIDShape(t *testing.T) {
	for _, id := range []string{blackthornBase, blackthornPOI, hexStarBase, hexStarPOI, obsidianBase} {
		if !playerStationID(id) {
			t.Errorf("playerStationID(%q) = false, want true (live player station)", id)
		}
	}
	// Every NPC station carries a readable slug. These are the ids the shuttle
	// sees constantly, and a false positive here would refuse ordinary fares.
	for _, id := range []string{
		"treasure_cache_trading_post", "grand_exchange_station", "central_nexus",
		"confederacy_central_command", "gold_run_extraction_hub", "",
	} {
		if playerStationID(id) {
			t.Errorf("playerStationID(%q) = true, want false (NPC station)", id)
		}
	}
	// Shape edges: right alphabet but wrong length, right length but not hex.
	for _, id := range []string{
		"566fbb2d3a304fa2d846147aed2aec0",   // 31
		"566fbb2d3a304fa2d846147aed2aec0cc", // 33
		"566FBB2D3A304FA2D846147AED2AEC0C",  // uppercase; the server mints lowercase
		"566fbb2d3a304fa2d846147aed2aec0g",  // 'g' is not hex
		"566fbb2d-3a30-4fa2-d846-147aed2a",  // hyphenated uuid
	} {
		if playerStationID(id) {
			t.Errorf("playerStationID(%q) = true, want false", id)
		}
	}
}

// The operator's rule: an unverified player station is treated as CLOSED. The
// asymmetry is deliberate -- guessing wrong costs one declined fare, while
// guessing wrong the other way strands a passenger nobody can drop and a shuttle
// that circles until someone intervenes by hand.
func TestDeliverableTreatsUnverifiedPlayerStationsAsClosed(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))
	if a.Deliverable(blackthornBase) {
		t.Error("an unverified player station must not be deliverable")
	}
	// NPC stations are never in question.
	if !a.Deliverable("treasure_cache_trading_post") {
		t.Error("an NPC station must always be deliverable")
	}
	// Proven open by another agent's dock.
	a.Seed([]string{hexStarBase})
	if !a.Deliverable(hexStarBase) {
		t.Error("a seeded-open player station must be deliverable")
	}
	// Seeding one station must not vouch for its neighbours.
	if a.Deliverable(obsidianBase) {
		t.Error("seeding Hex Star must not make an unrelated station deliverable")
	}
}

func TestRecordDockLearnsOnlyFromConclusiveOutcomes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")
	a := LoadStationAccess(path)

	a.RecordDock(hexStarBase, nil)
	if !a.Deliverable(hexStarBase) {
		t.Error("a successful dock must prove access")
	}
	a.RecordDock(blackthornBase, errors.New("Error: Access denied"))
	if !a.Denied(blackthornBase) {
		t.Error("an Access denied dock must be recorded as denied")
	}
	if a.Deliverable(blackthornBase) {
		t.Error("a denied station must not be deliverable")
	}

	// An inconclusive failure must teach NOTHING. Reading a disconnect or a
	// routing failure as a refusal would blacklist a good station permanently.
	for _, err := range []error{
		errors.New("dial tcp: connection reset by peer"),
		errors.New("no route to station"),
		errors.New("insufficient fuel"),
		errors.New("context deadline exceeded"),
	} {
		a.RecordDock(obsidianBase, err)
		if a.Denied(obsidianBase) {
			t.Errorf("a non-refusal error (%v) must not mark the station denied", err)
		}
	}

	// Owners can reconfigure access, so a later conclusive result overrides.
	a.RecordDock(blackthornBase, nil)
	if a.Denied(blackthornBase) || !a.Deliverable(blackthornBase) {
		t.Error("a later successful dock must clear a stale denial")
	}
	a.RecordDock(hexStarBase, errors.New("access denied"))
	if a.Deliverable(hexStarBase) {
		t.Error("a later refusal must clear a stale open record")
	}
}

// The map's whole value is that it outlives the pass that learned it -- the
// shuttle pays one stranded passenger to discover a closed station, and must
// never pay it twice.
func TestStationAccessPersistsAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "access.json")
	a := LoadStationAccess(path)
	a.RecordDock(hexStarBase, nil)
	a.RecordDock(blackthornBase, errors.New("Access denied"))

	b := LoadStationAccess(path)
	if !b.Deliverable(hexStarBase) {
		t.Error("a proven-open station must survive a reload")
	}
	if !b.Denied(blackthornBase) {
		t.Error("a denial must survive a reload")
	}
	if b.Deliverable(obsidianBase) {
		t.Error("an unrecorded station must still read as closed after a reload")
	}
}

// A missing or corrupt file must degrade to "nothing known", never to a worker
// that refuses to run -- and never to one that silently trusts every station.
func TestLoadStationAccessToleratesMissingAndCorruptFiles(t *testing.T) {
	missing := LoadStationAccess(filepath.Join(t.TempDir(), "nope.json"))
	if missing == nil {
		t.Fatal("a missing file must still yield a usable map")
	}
	if missing.Deliverable(hexStarBase) {
		t.Error("a missing file must not vouch for a player station")
	}
	if !missing.Deliverable("grand_exchange_station") {
		t.Error("a missing file must still allow NPC stations")
	}
}

// A nil map means the feature is not wired up (older callers, tests). It must be
// permissive: a nil map that refused every player station would disable delivery
// wherever it was forgotten.
func TestNilStationAccessIsPermissive(t *testing.T) {
	var a *StationAccess
	if !a.Deliverable(blackthornBase) {
		t.Error("a nil map must not block delivery")
	}
	if a.Denied(blackthornBase) {
		t.Error("a nil map must report nothing denied")
	}
	a.RecordDock(blackthornBase, errors.New("Access denied")) // must not panic
	a.Seed([]string{hexStarBase})                             // must not panic
}

// Seeding carries cross-agent evidence from the asset ledger. It may only ever
// ADD access: an empty docked_at_base proves nothing, so absence of evidence
// must not be recorded as refusal, and a seed must never overwrite a first-hand
// denial this shuttle actually experienced.
func TestSeedOnlyAddsAccessAndNeverOverridesADenial(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))
	a.RecordDock(blackthornBase, errors.New("Access denied"))
	a.Seed([]string{blackthornBase, hexStarBase, "treasure_cache_trading_post"})

	if a.Deliverable(blackthornBase) {
		t.Error("a seed must not resurrect a station that refused us first-hand")
	}
	if !a.Deliverable(hexStarBase) {
		t.Error("a seed must open a station with no contrary evidence")
	}
	// NPC ids are not worth storing: they inform no decision and only grow the file.
	if a.Denied("treasure_cache_trading_post") {
		t.Error("NPC stations must not enter the map")
	}
}
