package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/knowledge/knowledgetest"
)

// TestBackfillCreatureObservations loads the existing JSONL dump into
// wildlife_observations, so the table does not start empty and the two can be
// compared before the file is retired.
func TestBackfillCreatureObservations(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: knowledgetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	ctx := context.Background()

	dir := t.TempDir()
	lines := strings.Join([]string{
		`{"observed_utc":"2026-10-16T01:00:00Z","game_tick":1,"agent_id":"craftsman-1","system_id":"zibal","poi_id":"zibal_ice_shelf","creature_id":"lev_h","species":"hoarfrost_leviathan","name":"Hoarfrost Leviathan Hatchling","role":"predator","hull":312,"max_hull":312}`,
		// Same tick twice: the deliberate double-look, must collapse.
		`{"observed_utc":"2026-10-16T01:00:06Z","game_tick":1,"creature_id":"lev_h","species":"hoarfrost_leviathan","name":"Hoarfrost Leviathan Hatchling","hull":312,"max_hull":312}`,
		`{"observed_utc":"2026-10-16T02:00:00Z","game_tick":900,"creature_id":"lev_h","species":"hoarfrost_leviathan","name":"Juvenile Hoarfrost Leviathan","hull":700,"max_hull":700}`,
		// A malformed line must not abort the import of the rest.
		`{"creature_id":"broken",`,
		`{"observed_utc":"2026-10-16T02:00:00Z","game_tick":900,"creature_id":"crt_g","species":"belt_grazer","name":"Belt-Grazer","hull":60,"max_hull":60}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "observations.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	n, skipped, err := backfillCreatureObservations(ctx, kb, dir)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 malformed line", skipped)
	}
	if n != 4 {
		t.Errorf("imported = %d, want 4 parsed rows", n)
	}

	// Rows: lev_h at two ticks (double-look collapsed) plus crt_g = 3.
	buckets, err := kb.WildlifeStageHistogram(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byStage := map[string]knowledge.WildlifeStageBucket{}
	for _, b := range buckets {
		byStage[b.Species+"/"+b.Stage] = b
	}
	if got := byStage["hoarfrost_leviathan/hatchling"]; got.Individuals != 1 || got.HullMax != 312 || got.Sightings != 1 {
		t.Errorf("hatchling = %+v, want n1 hull 312 sightings 1 (double-look collapsed)", got)
	}
	if got := byStage["hoarfrost_leviathan/juvenile"]; got.Individuals != 1 || got.HullMax != 700 {
		t.Errorf("juvenile = %+v, want n1 hull 700", got)
	}
	if got := byStage["belt_grazer/"]; got.Individuals != 1 {
		t.Errorf("mature belt_grazer bucket = %+v, want n1", got)
	}

	// Re-running must be idempotent: the import is an upsert, not an append.
	if _, _, err := backfillCreatureObservations(ctx, kb, dir); err != nil {
		t.Fatal(err)
	}
	if again, err := kb.WildlifeStageHistogram(ctx); err != nil {
		t.Fatal(err)
	} else if len(again) != len(buckets) {
		t.Errorf("re-import changed bucket count %d -> %d; not idempotent", len(buckets), len(again))
	}
}
