package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWildlifeGrowthReport_KeepsEachStageOfAMaturingIndividual is a REGRESSION
// test for stage collapse.
//
// The report deduped per creature_id GLOBALLY, keeping the widest hull and the
// name seen at that hull ("a later, larger reading means it matured into this
// name"). So an animal that grew from hatchling to juvenile collapsed into ONE
// entry in the juvenile bucket and its hatchling reading vanished from the
// histogram entirely.
//
// That drains the lower bands over time: every surviving animal migrates
// upward, leaving the young stages populated only by animals that died or
// stopped being sighted. It looked correct only because, as of 2026-10-16, no
// individual had yet been observed growing at all.
func TestWildlifeGrowthReport_KeepsEachStageOfAMaturingIndividual(t *testing.T) {
	dir := t.TempDir()
	// lev_grow is ONE animal seen first as a hatchling, later as a juvenile.
	// lev_j is a second, independent juvenile.
	lines := strings.Join([]string{
		`{"observed_utc":"2026-10-16T01:00:00Z","game_tick":1,"creature_id":"lev_grow","species":"hoarfrost_leviathan","name":"Hoarfrost Leviathan Hatchling","hull":312,"max_hull":312}`,
		`{"observed_utc":"2026-10-16T02:00:00Z","game_tick":900,"creature_id":"lev_grow","species":"hoarfrost_leviathan","name":"Juvenile Hoarfrost Leviathan","hull":700,"max_hull":700}`,
		`{"observed_utc":"2026-10-16T02:00:00Z","game_tick":900,"creature_id":"lev_j","species":"hoarfrost_leviathan","name":"Juvenile Hoarfrost Leviathan","hull":769,"max_hull":769}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "observations.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	out := wildlifeGrowthReport(dir)

	if !strings.Contains(out, "Hatchling") {
		t.Errorf("hatchling stage erased by maturation:\n%s", out)
	}
	// The hatchling band must report the hull it was a hatchling AT, not the
	// hull it later grew to.
	if !strings.Contains(out, "312") {
		t.Errorf("hatchling hull 312 missing:\n%s", out)
	}
	// Two distinct juveniles: the matured animal and lev_j.
	if !strings.Contains(out, "n=2") {
		t.Errorf("juvenile band should count 2 individuals:\n%s", out)
	}
	// Growth is still called out.
	if !strings.Contains(out, "lev_grow") {
		t.Errorf("observed growth not reported:\n%s", out)
	}
}

// TestWildlifeGrowthReport_CountsIndividualsNotRows keeps the double-look
// dedupe: two sightings in the same tick are one animal.
func TestWildlifeGrowthReport_CountsIndividualsNotRows(t *testing.T) {
	dir := t.TempDir()
	line := `{"observed_utc":"2026-10-16T01:00:00Z","game_tick":1,"creature_id":"lev_a","species":"molt_leviathan","name":"Juvenile Molt Leviathan","hull":569,"max_hull":569}`
	if err := os.WriteFile(filepath.Join(dir, "observations.jsonl"),
		[]byte(line+"\n"+line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := wildlifeGrowthReport(dir)
	if !strings.Contains(out, "n=1") {
		t.Errorf("double-look not collapsed:\n%s", out)
	}
}
