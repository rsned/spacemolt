package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The deliverable is a per-stage hull histogram ("Youth 1250-1500 HP, Adult
// 2000-2500 HP"), so the report buckets by the creature NAME, which is what
// carries the stage. That needs no stage vocabulary: a new stage simply appears
// as a new bucket the first time one is seen.
func TestWildlifeGrowthReportBucketsByStageName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `
{"creature_id":"lev_a","species":"cloudwhale","name":"Young Cloudwhale","max_hull":1368,"observed_utc":"2026-10-14T15:13:09Z"}
{"creature_id":"lev_b","species":"cloudwhale","name":"Young Cloudwhale","max_hull":1433,"observed_utc":"2026-10-14T15:13:09Z"}
{"creature_id":"lev_c","species":"cloudwhale","name":"Young Cloudwhale","max_hull":1408,"observed_utc":"2026-10-14T15:13:09Z"}
{"creature_id":"lev_d","species":"cloudwhale","name":"Cloudwhale","max_hull":2400,"observed_utc":"2026-10-14T18:00:00Z"}
`)
	got := wildlifeGrowthReport(dir)

	for _, want := range []string{"cloudwhale", "Young Cloudwhale", "1368", "1433", "Cloudwhale", "2400"} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// A creature re-observed on every pass must count ONCE in the distribution:
// the explore loop deliberately looks twice per POI, so raw rows double every
// individual and would report a population of 6 where 3 exist. The widest hull
// seen for that individual is the one kept -- hull only grows.
func TestWildlifeGrowthReportDedupesByCreature(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `
{"creature_id":"lev_a","species":"cloudwhale","name":"Young Cloudwhale","max_hull":1368,"observed_utc":"2026-10-14T15:13:09Z"}
{"creature_id":"lev_a","species":"cloudwhale","name":"Young Cloudwhale","max_hull":1368,"observed_utc":"2026-10-14T15:13:15Z"}
{"creature_id":"lev_a","species":"cloudwhale","name":"Young Cloudwhale","max_hull":1390,"observed_utc":"2026-10-14T18:44:32Z"}
`)
	got := wildlifeGrowthReport(dir)

	if !strings.Contains(got, "n=1") {
		t.Errorf("want n=1 individual after dedupe\n--- got ---\n%s", got)
	}
	// 1390 is the latest and widest reading for this individual; 1368 must not
	// be reported as the top of the range.
	if !strings.Contains(got, "1390") {
		t.Errorf("want the grown hull 1390 retained\n--- got ---\n%s", got)
	}
	if strings.Contains(got, "n=3") {
		t.Errorf("counted raw rows instead of individuals\n--- got ---\n%s", got)
	}
}

// Growth is the whole question, so an individual seen at two hulls must be
// reported as having grown rather than silently collapsed.
func TestWildlifeGrowthReportFlagsObservedGrowth(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `
{"creature_id":"lev_a","species":"cloudwhale","name":"Young Cloudwhale","max_hull":1368,"observed_utc":"2026-10-14T15:13:09Z"}
{"creature_id":"lev_a","species":"cloudwhale","name":"Young Cloudwhale","max_hull":1390,"observed_utc":"2026-10-14T18:44:32Z"}
`)
	got := wildlifeGrowthReport(dir)
	// Assert the CONTENT of the growth call-out, not its wording: the
	// individual and its hull transition are the finding.
	if !strings.Contains(got, "lev_a 1368->1390") {
		t.Errorf("report must surface observed growth\n--- got ---\n%s", got)
	}
}

func TestWildlifeGrowthReportMissingDirIsFriendly(t *testing.T) {
	got := wildlifeGrowthReport(filepath.Join(t.TempDir(), "nope"))
	if got == "" {
		t.Error("want an explanatory message, not empty output")
	}
}

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "observations.jsonl"),
		[]byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
