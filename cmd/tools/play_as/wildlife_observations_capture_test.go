package main

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/knowledge/knowledgetest"
)

// TestCaptureCreatureObservations_WritesTheTable covers the KB half of the
// collector.
//
// It uses a real SQLiteKB: MemoryKB implements NONE of the wildlife
// interfaces, so a type assertion against it returns nil and the capture
// becomes a silent no-op that still reports success -- which is exactly how
// earlier wildlife tests passed while recording nothing.
func TestCaptureCreatureObservations_WritesTheTable(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: knowledgetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	ctx := context.Background()

	creatures := []serverapi.NearbyCreature{
		{CreatureID: "lev_h", Species: "hoarfrost_leviathan", Name: "Hoarfrost Leviathan Hatchling",
			Role: "predator", Hull: 312, MaxHull: 312},
		{CreatureID: "crt_g", Species: "belt_grazer", Name: "Belt-Grazer", Role: "grazer", Hull: 60, MaxHull: 60},
	}

	if err = captureCreatureObservations(ctx, kb, creatures, "oxbow", "oxbow_belt_b", "craftsman-1", 2143625); err != nil {
		t.Fatal(err)
	}

	buckets, err := kb.WildlifeStageHistogram(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 2 {
		t.Fatalf("got %d buckets, want 2: %+v", len(buckets), buckets)
	}

	var sawHatchling bool
	for _, b := range buckets {
		if b.Species == "hoarfrost_leviathan" {
			sawHatchling = true
			if b.Stage != "hatchling" {
				t.Errorf("stage = %q, want hatchling", b.Stage)
			}
			if b.Individuals != 1 || b.HullMax != 312 {
				t.Errorf("got n%d max %d, want n1 312", b.Individuals, b.HullMax)
			}
		}
	}
	if !sawHatchling {
		t.Error("hoarfrost_leviathan bucket missing")
	}
}

// TestCaptureCreatureObservations_NilKBIsNotAnError keeps the collector usable
// in a session started without --db-path, where globalKB is nil.
func TestCaptureCreatureObservations_NilKBIsNotAnError(t *testing.T) {
	if err := captureCreatureObservations(context.Background(), nil,
		[]serverapi.NearbyCreature{{CreatureID: "lev_h", Name: "Hoarfrost Leviathan Hatchling", MaxHull: 312}},
		"oxbow", "oxbow_belt_b", "craftsman-1", 1); err != nil {
		t.Errorf("nil kb should be a no-op, got %v", err)
	}
}
