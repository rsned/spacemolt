package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// wildlifeObservationReader is the read half of the observations table.
//
// It is asserted rather than added to knowledge.Base because MemoryKB
// implements none of the wildlife interfaces. A failed assertion therefore
// means "this KB cannot answer", which must be an ERROR here -- the same
// assertion returning nil silently is how earlier wildlife capture reported
// success while recording nothing.
type wildlifeObservationReader interface {
	WildlifeStageHistogram(ctx context.Context) ([]knowledge.WildlifeStageBucket, error)
	WildlifeGrowthObserved(ctx context.Context) ([]knowledge.WildlifeGrowth, error)
}

// wildlifeGrowthReportFromKB renders the per-stage histogram from
// wildlife_observations rather than the JSONL dump.
//
// It exists to be COMPARED against wildlifeGrowthReport: the file is retired
// only once the two agree, so the two renderers deliberately report the same
// columns from independent sources.
func wildlifeGrowthReportFromKB(ctx context.Context, kb knowledge.Base) (string, error) {
	if kb == nil {
		return "", fmt.Errorf("wildlife_growth --db: no knowledge base configured (use --db-path)")
	}
	reader, ok := kb.(wildlifeObservationReader)
	if !ok {
		return "", fmt.Errorf("wildlife_growth --db: this knowledge base does not hold wildlife observations")
	}

	buckets, err := reader.WildlifeStageHistogram(ctx)
	if err != nil {
		return "", err
	}
	if len(buckets) == 0 {
		return "wildlife_observations holds no rows yet.\n" +
			"Run `wildlife_growth --import` to load the JSONL dump, or explore past a POI with creatures.\n", nil
	}

	grew, err := reader.WildlifeGrowthObserved(ctx)
	if err != nil {
		return "", err
	}

	spW, stW := len("SPECIES"), len("STAGE")
	var individuals, sightings int
	for _, b := range buckets {
		spW = max(spW, len(b.Species))
		stW = max(stW, len(displayStage(b)))
		individuals += b.Individuals
		sightings += b.Sightings
	}

	var out strings.Builder
	fmt.Fprintf(&out, "Creature hull by stage (wildlife_observations) — %d stage populations, %d sightings\n",
		len(buckets), sightings)
	fmt.Fprintf(&out, "  n counts individuals WITHIN a stage, so a matured animal counts in each stage it was seen at.\n\n")
	fmt.Fprintf(&out, "  %-*s  %-*s  %5s  %-14s %s\n",
		spW, "SPECIES", stW, "STAGE", "n", "HULL RANGE", "SIGHTINGS")
	fmt.Fprintf(&out, "  %s\n", strings.Repeat("-", spW+stW+36))
	for _, b := range buckets {
		rng := fmt.Sprintf("%d-%d HP", b.HullMin, b.HullMax)
		if b.HullMin == b.HullMax {
			rng = fmt.Sprintf("%d HP", b.HullMin)
		}
		fmt.Fprintf(&out, "  %-*s  %-*s  n=%-3d  %-14s %d\n",
			spW, b.Species, stW, displayStage(b), b.Individuals, rng, b.Sightings)
	}

	if len(grew) == 0 {
		fmt.Fprintf(&out, "\n  No individual has yet been observed at two different hulls.\n")

		return out.String(), nil
	}

	fmt.Fprintf(&out, "\n  Individuals observed GROWING:\n")
	for _, g := range grew {
		from, to := g.FirstStage, g.LastStage
		if from == "" {
			from = "(mature)"
		}
		if to == "" {
			to = "(mature)"
		}
		fmt.Fprintf(&out, "    %-40s %5d -> %-5d HP  over %d tick(s)  %s -> %s\n",
			g.CreatureID, g.FirstHull, g.LastHull, g.TicksElapsed, from, to)
	}

	return out.String(), nil
}

// displayStage renders the empty stage as "(mature)": "" is the adult bucket,
// not a missing value, and a blank column reads as a capture gap.
func displayStage(b knowledge.WildlifeStageBucket) string {
	if b.Stage == "" {
		return "(mature)"
	}

	return b.Stage
}
