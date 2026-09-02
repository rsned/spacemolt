package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func newHaltKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

// detectUnscanned reports which species present in a get_nearby creature list
// have never been scanned: seen (upserted) but no danger_scanned_utc stamp.
func TestDetectUnscanned_FiltersScannedSpecies(t *testing.T) {
	kb := newHaltKB(t)
	ctx := context.Background()
	creatures := []serverapi.NearbyCreature{
		{CreatureID: "crt_1", Species: "aeonbear", Name: "Aeonbear", Role: "grazer"},
		{CreatureID: "crt_2", Species: "frost_lurker", Name: "Frost-Lurker", Role: "predator"},
		{CreatureID: "crt_3", Species: "frost_lurker", Name: "Frost-Lurker", Role: "predator"}, // dup species
	}
	if _, err := knowledge.CaptureWildlifeNearby(ctx, kb, creatures, "sys", "poi", "ice_field", "tester", 100); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// aeonbear has been scanned; frost_lurker has not.
	if err := knowledge.CaptureWildlifeScan(ctx, kb, "aeonbear",
		knowledge.CreatureScan{Name: "Aeonbear", Traits: "harmless prey", ThreatClass: "grazer"}, time.Now()); err != nil {
		t.Fatalf("scan stamp: %v", err)
	}

	got := detectUnscanned(ctx, kb, creatures)
	if len(got) != 1 || got[0] != "frost_lurker" {
		t.Errorf("detectUnscanned = %v, want [frost_lurker]", got)
	}
}

func TestDetectUnscanned_NilKBReportsNothing(t *testing.T) {
	got := detectUnscanned(context.Background(), nil, []serverapi.NearbyCreature{{Species: "aeonbear"}})
	if len(got) != 0 {
		t.Errorf("detectUnscanned with nil KB = %v, want none", got)
	}
}

// The halt is a typed error so exploreSystem can surface it through its error
// return and autoExplore can recognise a clean stop rather than a failure.
func TestUnscannedHalt_ErrorAndUnwrap(t *testing.T) {
	err := fmt.Errorf("explore: %w", &unscannedHalt{
		Species: []string{"frost_lurker", "tempest_eel"}, POIID: "p1", POIName: "Ashford Ice Shelf"})
	var halt *unscannedHalt
	if !errors.As(err, &halt) {
		t.Fatalf("errors.As failed to find unscannedHalt in %v", err)
	}
	msg := halt.Error()
	for _, want := range []string{"Ashford Ice Shelf", "frost_lurker", "tempest_eel", "unscanned"} {
		if !strings.Contains(msg, want) {
			t.Errorf("halt error %q missing %q", msg, want)
		}
	}
}
