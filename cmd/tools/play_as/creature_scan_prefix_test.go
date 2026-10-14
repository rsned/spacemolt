package main

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/knowledge/knowledgetest"
)

// MemoryKB implements none of the wildlife interfaces, so CaptureWildlifeScan
// and UnscannedSpecies both silently no-op against it -- which would make
// every assertion here pass whether the bug is fixed or not. A real SQLite KB
// is required for these tests to mean anything.
func scanTestKB(t *testing.T) knowledge.Base {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: knowledgetest.Path(t)})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	return kb
}

// seedUnscanned puts the species in the guide the way get_nearby does -- name,
// role and hull but no danger bracket -- which is the state that makes explore
// announce it as unscanned. UnscannedSpecies deliberately ignores a species
// with NO row at all, so without this the assertions below pass vacuously.
func seedUnscanned(t *testing.T, kb knowledge.Base, species, name string, hull int) {
	t.Helper()
	rec, ok := kb.(interface {
		UpsertWildlifeSpecies(context.Context, []knowledge.WildlifeSpecies) error
	})
	if !ok {
		t.Fatal("KB does not record wildlife species; the test would prove nothing")
	}
	if err := rec.UpsertWildlifeSpecies(context.Background(), []knowledge.WildlifeSpecies{{
		Species: species, Name: name, Role: "grazer", MaxHull: hull,
	}}); err != nil {
		t.Fatalf("seed species: %v", err)
	}
	pre, err := knowledge.UnscannedSpecies(context.Background(), kb, []string{species})
	if err != nil {
		t.Fatalf("UnscannedSpecies(pre): %v", err)
	}
	if len(pre) != 1 {
		t.Fatalf("precondition: %s should read as unscanned before the scan, got %v", species, pre)
	}
}

// stubScanClient serves the two raw cache keys captureCreatureScan reads: the
// scan reply ("_last") and the creature roster it resolves the species from
// ("nearby").
type stubScanClient struct {
	game.GameClient
	last   []byte
	nearby []byte
}

func (s stubScanClient) GetRawJSON(key string) []byte {
	if key == "nearby" {
		return s.nearby
	}
	return s.last
}

// Creature ids are NOT all crt_. The leviathan family introduced with the
// 2026-10-14 wildlife update uses lev_, and the capture was gated on a
// hardcoded "crt_" prefix -- so scanning lev_48383fdf... returned a good
// server reply that was then silently discarded, leaving the species reported
// as unscanned forever with no error to explain it.
//
// The gate must be whether the id resolves to a creature in the get_nearby
// roster, which is prefix-agnostic, not the spelling of the id.
func TestCaptureCreatureScanAcceptsNonCrtPrefix(t *testing.T) {
	saved := globalKB
	defer func() { globalKB = saved }()
	globalKB = scanTestKB(t)
	seedUnscanned(t, globalKB, "cloudwhale", "Young Cloudwhale", 1368)

	client := stubScanClient{
		// Real shape: the threat class and traits ride in the username
		// brackets with an em-dash separator, and revealed_info is a list.
		last: []byte(`{"success":true,
		               "username":"Young Cloudwhale [grazer \u2014 harmless prey]",
		               "revealed_info":["species","role","hull"],"hull":1368,
		               "description":"A drifting gasbag grazer."}`),
		nearby: []byte(`{"creatures":[
		               {"creature_id":"lev_48383fdffaa8222348d32134fdc996b1",
		                "species":"cloudwhale","name":"Young Cloudwhale",
		                "role":"grazer","hull":1368,"max_hull":1368}]}`),
	}

	captureCreatureScan(client, context.Background(),
		"lev_48383fdffaa8222348d32134fdc996b1", formatRaw)

	got, err := knowledge.UnscannedSpecies(context.Background(), globalKB, []string{"cloudwhale"})
	if err != nil {
		t.Fatalf("UnscannedSpecies: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("cloudwhale still reported unscanned %v; the scan was discarded", got)
	}
}

// A crt_ creature must keep working.
func TestCaptureCreatureScanStillAcceptsCrtPrefix(t *testing.T) {
	saved := globalKB
	defer func() { globalKB = saved }()
	globalKB = scanTestKB(t)
	seedUnscanned(t, globalKB, "bulwark", "Bulwark", 95)

	client := stubScanClient{
		last:   []byte(`{"success":true,"username":"Bulwark [grazer \u2014 harmless prey]","revealed_info":["species","role","hull"],"hull":95}`),
		nearby: []byte(`{"creatures":[{"creature_id":"crt_abc","species":"bulwark","name":"Bulwark","role":"grazer","max_hull":95}]}`),
	}
	captureCreatureScan(client, context.Background(), "crt_abc", formatRaw)

	got, _ := knowledge.UnscannedSpecies(context.Background(), globalKB, []string{"bulwark"})
	if len(got) != 0 {
		t.Errorf("bulwark still unscanned %v", got)
	}
}

// Scanning a PLAYER must record nothing: scan serves both, and the player is
// absent from the creature roster.
func TestCaptureCreatureScanIgnoresPlayerTarget(t *testing.T) {
	saved := globalKB
	defer func() { globalKB = saved }()
	globalKB = scanTestKB(t)

	client := stubScanClient{
		last:   []byte(`{"success":true,"username":"VoidMoth_Jolly","revealed_info":["hull"],"hull":80}`),
		nearby: []byte(`{"creatures":[]}`),
	}
	// Must not panic and must not invent a species row.
	captureCreatureScan(client, context.Background(), "VoidMoth_Jolly", formatRaw)

	// Nothing was observed, so nothing may be claimed as scanned; an invented
	// row would show up as a species the guide thinks it knows.
	got, err := knowledge.UnscannedSpecies(context.Background(), globalKB, []string{"VoidMoth_Jolly"})
	if err != nil {
		t.Fatalf("UnscannedSpecies: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("player scan created a species row: %v", got)
	}
}
